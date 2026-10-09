package detect

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/amitpatole/cgroupguard/internal/model"
	"k8s.io/client-go/kubernetes"
)

// kubelet_cgroup_version is an ALPHA kubelet gauge ("cgroup version on the hosts").
// Value 2 was observed on a cgroup v2 node; kubelet_linux.go cgroupVersionCheck
// sets the gauge from the node config version and switches on 1 (v1) and 2 (v2).
// It cannot express a hybrid layout, so hybrid nodes are never reported by it.
const metricName = "kubelet_cgroup_version"

const maxMetricsBytes = 32 << 20

// MaxSampleAge is how old a Prometheus sample may be before it is rejected.
const MaxSampleAge = 5 * time.Minute

// ParseCgroupVersionMetric reads a Prometheus text exposition and returns the
// cgroup version (1 or 2). Missing, malformed, out-of-range or conflicting
// samples are errors, never a guessed value.
func ParseCgroupVersionMetric(r io.Reader) (int, error) {
	sc := bufio.NewScanner(io.LimitReader(r, maxMetricsBytes))
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	version := 0
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, metricName) {
			continue
		}
		rest := line[len(metricName):]
		if rest == "" || rest[0] != ' ' && rest[0] != '{' {
			continue // a different metric that merely shares the prefix
		}
		if i := strings.LastIndex(rest, "}"); i >= 0 {
			rest = rest[i+1:]
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			return 0, fmt.Errorf("malformed %s sample", metricName)
		}
		v, e := strconv.ParseFloat(fields[0], 64)
		if e != nil || (v != 1 && v != 2) {
			return 0, fmt.Errorf("unexpected %s value %q", metricName, fields[0])
		}
		if version != 0 && version != int(v) {
			return 0, fmt.Errorf("conflicting %s samples", metricName)
		}
		version = int(v)
	}
	if e := sc.Err(); e != nil {
		return 0, e
	}
	if version == 0 {
		return 0, fmt.Errorf("%s not present (metric is ALPHA and kubelet-version dependent)", metricName)
	}
	return version, nil
}

func modeFor(version int) string {
	if version == 1 {
		return "v1"
	}
	return "v2"
}

// Fetcher returns the kubelet /metrics body for a node.
type Fetcher func(ctx context.Context, node string) (io.ReadCloser, error)

// NodeProxyFetcher scrapes through the API server node proxy. This needs the
// nodes/proxy permission, which is broader than read-only inventory; callers must
// opt in explicitly.
func NodeProxyFetcher(c kubernetes.Interface) Fetcher {
	return func(ctx context.Context, node string) (io.ReadCloser, error) {
		return c.CoreV1().RESTClient().Get().Resource("nodes").Name(node).SubResource("proxy").Suffix("metrics").Stream(ctx)
	}
}

// ApplyKubeletMetrics fills in Cgroup for Linux nodes whose mode is still unknown.
// A failure on one node leaves it unknown and returns a limitation, not an error.
func ApplyKubeletMetrics(ctx context.Context, nodes []model.Node, fetch Fetcher) []string {
	var limits []string
	for i := range nodes {
		n := &nodes[i]
		if n.OS != "linux" || n.Cgroup != "unknown" {
			continue
		}
		body, e := fetch(ctx, n.Name)
		if e == nil {
			var v int
			v, e = ParseCgroupVersionMetric(body)
			_ = body.Close()
			if e == nil {
				n.Cgroup = modeFor(v)
				n.Evidence = fmt.Sprintf("%s=%d via API-server node proxy (ALPHA metric; hybrid layouts are not visible)", metricName, v)
				continue
			}
		}
		limits = append(limits, fmt.Sprintf("Kubelet metrics unavailable for node %s: %v", n.Name, e))
	}
	return limits
}

// PrometheusCgroup queries an existing Prometheus for kubelet_cgroup_version and
// applies it to nodes still unknown. Samples older than MaxSampleAge, samples that
// do not name exactly one known node, and conflicting duplicates are rejected.
func PrometheusCgroup(ctx context.Context, hc *http.Client, base string, nodes []model.Node, now time.Time) ([]string, error) {
	u, e := url.Parse(base)
	if e != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return nil, fmt.Errorf("--prometheus-url must be an http(s) URL without credentials")
	}
	u.Path = strings.TrimSuffix(u.Path, "/") + "/api/v1/query"
	u.RawQuery = url.Values{"query": {metricName}}.Encode()
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if e != nil {
		return nil, e
	}
	resp, e := hc.Do(req)
	if e != nil {
		return nil, e
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("prometheus returned HTTP %d", resp.StatusCode)
	}
	return applyPrometheus(io.LimitReader(resp.Body, maxMetricsBytes), nodes, now)
}

// HTTPClient returns a client with a total timeout that never follows redirects,
// so a configured Prometheus URL cannot bounce the request to another host.
func HTTPClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func applyPrometheus(body io.Reader, nodes []model.Node, now time.Time) ([]string, error) {
	var out struct {
		Status string `json:"status"`
		Data   struct {
			ResultType string `json:"resultType"`
			Result     []struct {
				Metric map[string]string `json:"metric"`
				Value  [2]any            `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}
	if e := json.NewDecoder(body).Decode(&out); e != nil {
		return nil, fmt.Errorf("invalid prometheus response: %w", e)
	}
	if out.Status != "success" || out.Data.ResultType != "vector" {
		return nil, fmt.Errorf("unexpected prometheus result")
	}
	known := map[string]*model.Node{}
	for i := range nodes {
		known[nodes[i].Name] = &nodes[i]
	}
	seen := map[string]int{}
	conflict := map[string]bool{}
	for _, s := range out.Data.Result {
		name := ""
		for _, label := range []string{"node", "nodename", "instance"} {
			v := s.Metric[label]
			if label == "instance" {
				if i := strings.LastIndex(v, ":"); i > 0 {
					v = v[:i]
				}
			}
			if _, ok := known[v]; ok {
				name = v
				break
			}
		}
		if name == "" {
			continue // sample cannot be tied to exactly one inventory node
		}
		ts, ok := s.Value[0].(float64)
		str, ok2 := s.Value[1].(string)
		if !ok || !ok2 {
			conflict[name] = true
			continue
		}
		age := now.Sub(time.Unix(int64(ts), 0))
		if age > MaxSampleAge || age < -time.Minute {
			conflict[name] = true
			continue
		}
		v, e := strconv.ParseFloat(str, 64)
		if e != nil || (v != 1 && v != 2) {
			conflict[name] = true
			continue
		}
		if prev, dup := seen[name]; dup && prev != int(v) {
			conflict[name] = true
		}
		seen[name] = int(v)
	}
	var limits []string
	for i := range nodes {
		n := &nodes[i]
		if n.OS != "linux" || n.Cgroup != "unknown" {
			continue
		}
		v, ok := seen[n.Name]
		switch {
		case conflict[n.Name]:
			limits = append(limits, fmt.Sprintf("Prometheus %s for node %s was stale, malformed or conflicting; left unknown", metricName, n.Name))
		case ok:
			n.Cgroup = modeFor(v)
			n.Evidence = fmt.Sprintf("%s=%d from Prometheus (sample under %s old; ALPHA metric; hybrid layouts are not visible)", metricName, v, MaxSampleAge)
		default:
			limits = append(limits, fmt.Sprintf("Prometheus has no %s sample for node %s", metricName, n.Name))
		}
	}
	return limits, nil
}
