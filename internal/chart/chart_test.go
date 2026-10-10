package chart

import (
	"os"

	"bytes"
	"github.com/amitpatole/cgroupguard/internal/version"
	"os/exec"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

type doc = map[string]any

func render(t *testing.T, sets ...string) ([]doc, string, error) {
	t.Helper()
	helm, err := exec.LookPath("helm")
	if err != nil {
		t.Skip("helm is not installed")
	}
	args := []string{"template", "cg", "../../charts/cgroupguard", "-n", "cgroupguard-system"}
	for _, s := range sets {
		args = append(args, "--set", s)
	}
	cmd := exec.Command(helm, args...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return nil, errb.String(), err
	}
	var docs []doc
	dec := yaml.NewDecoder(&out)
	for {
		var d doc
		if err := dec.Decode(&d); err != nil {
			break
		}
		if d != nil {
			docs = append(docs, d)
		}
	}
	return docs, "", nil
}

func kinds(docs []doc, kind string) []doc {
	var r []doc
	for _, d := range docs {
		if d["kind"] == kind {
			r = append(r, d)
		}
	}
	return r
}

func list(v any) []any { l, _ := v.([]any); return l }
func m(v any) doc      { d, _ := v.(doc); return d }

// ruleSet flattens a ClusterRole into "group/resource:verb" strings.
func ruleSet(d doc) map[string]bool {
	set := map[string]bool{}
	for _, r := range list(d["rules"]) {
		rule := m(r)
		for _, g := range list(rule["apiGroups"]) {
			for _, res := range list(rule["resources"]) {
				for _, v := range list(rule["verbs"]) {
					set[g.(string)+"/"+res.(string)+":"+v.(string)] = true
				}
			}
		}
	}
	return set
}

const img = "image.repository=example.invalid/cgroupguard"

func TestImageIsRequired(t *testing.T) {
	_, stderr, err := render(t)
	if err == nil || !(strings.Contains(stderr, "image.repository") || strings.Contains(stderr, "/image/repository")) {
		t.Fatalf("rendered without an image: %v %s", err, stderr)
	}
}

func TestDefaultRBACIsLeastPrivilege(t *testing.T) {
	docs, _, err := render(t, img)
	if err != nil {
		t.Fatal(err)
	}
	cr := ruleSet(kinds(docs, "ClusterRole")[0])
	for _, want := range []string{"/nodes:list", "/pods:list", "apps/deployments:list", "batch/cronjobs:list", "policy/poddisruptionbudgets:list", "cgroupguard.io/cgroupreadinesses:get", "cgroupguard.io/cgroupreadinesses/status:update"} {
		if !cr[want] {
			t.Errorf("missing permission %s", want)
		}
	}
	for perm := range cr {
		for _, bad := range []string{"secrets", "configmaps", "pods/exec", "pods/log", "nodes/proxy", "events", "*", "serviceaccounts", "roles", "clusterroles"} {
			if strings.Contains(perm, "/"+bad+":") {
				t.Errorf("default role grants %s", perm)
			}
		}
		for _, verb := range []string{":create", ":delete", ":patch", ":deletecollection", ":impersonate", ":escalate", ":bind", ":*"} {
			if strings.HasSuffix(perm, verb) && !strings.Contains(perm, "cgroupreadinesses") {
				t.Errorf("default role grants write permission %s", perm)
			}
		}
	}
	if cr["cgroupguard.io/cgroupreadinesses:update"] || cr["cgroupguard.io/cgroupreadinesses:delete"] {
		t.Error("the controller can modify the spec of its resources")
	}
}

func TestOptionalPermissionsAppearOnlyWhenEnabled(t *testing.T) {
	docs, _, _ := render(t, img, "controller.workloads=false")
	cr := ruleSet(kinds(docs, "ClusterRole")[0])
	for perm := range cr {
		if strings.Contains(perm, "/pods:") || strings.HasPrefix(perm, "apps/") || strings.HasPrefix(perm, "batch/") || strings.HasPrefix(perm, "policy/") {
			t.Errorf("workload permission %s with workloads disabled", perm)
		}
	}
	docs, _, _ = render(t, img, "controller.nodeEvents=true")
	if !ruleSet(kinds(docs, "ClusterRole")[0])["/events:list"] {
		t.Error("nodeEvents did not add events list")
	}
	// The chart offers no switch that grants nodes/proxy.
	if _, _, err := render(t, img, "controller.kubeletMetrics=true"); err == nil {
		t.Error("the chart accepts a kubeletMetrics value although it must not grant nodes/proxy")
	}
	for _, perm := range []string{"/nodes/proxy:get"} {
		for _, set := range [][]string{{img}, {img, "controller.nodeEvents=true"}, {img, "controller.workloads=false"}} {
			docs, _, _ := render(t, set...)
			if ruleSet(kinds(docs, "ClusterRole")[0])[perm] {
				t.Errorf("%s granted with %v", perm, set)
			}
		}
	}
	docs, _, _ = render(t, img, "leaderElection=false")
	for _, r := range kinds(docs, "Role") {
		if ruleSet(r)["coordination.k8s.io/leases:create"] {
			t.Error("lease permission without leader election")
		}
	}
}

func TestPodIsHardened(t *testing.T) {
	docs, _, _ := render(t, img)
	d := kinds(docs, "Deployment")[0]
	spec := m(m(m(d["spec"])["template"])["spec"])
	psc := m(spec["securityContext"])
	if psc["runAsNonRoot"] != true || m(psc["seccompProfile"])["type"] != "RuntimeDefault" {
		t.Errorf("pod security context: %v", psc)
	}
	for _, k := range []string{"hostNetwork", "hostPID", "hostIPC"} {
		if spec[k] != nil && spec[k] != false {
			t.Errorf("%s is set", k)
		}
	}
	c := m(list(spec["containers"])[0])
	sc := m(c["securityContext"])
	caps := list(m(sc["capabilities"])["drop"])
	if sc["allowPrivilegeEscalation"] != false || sc["readOnlyRootFilesystem"] != true || len(caps) != 1 || caps[0] != "ALL" || sc["privileged"] == true {
		t.Errorf("container security context: %v", sc)
	}
	if m(c["resources"])["limits"] == nil || m(c["resources"])["requests"] == nil {
		t.Error("no resource requests/limits")
	}
	if len(list(spec["volumes"])) != 0 {
		t.Error("the pod mounts volumes")
	}
	if !strings.HasSuffix(c["image"].(string), ":"+version.Version) {
		t.Errorf("image tag does not default to the chart version: %v", c["image"])
	}
}

func TestDigestOverridesTagAndSchemaRejectsBadValues(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	docs, _, err := render(t, img, "image.digest="+digest)
	if err != nil {
		t.Fatal(err)
	}
	c := m(list(m(m(m(kinds(docs, "Deployment")[0]["spec"])["template"])["spec"])["containers"])[0])
	if c["image"] != "example.invalid/cgroupguard@"+digest {
		t.Errorf("image: %v", c["image"])
	}
	for _, bad := range []string{"replicaCount=0", "replicaCount=50", "image.digest=sha256:short", "image.pullPolicy=Sometimes", "controller.prometheusURL=http://user:pw@prom:9090", "controller.prometheusURL=ftp://x", "defaultReadiness.intervalSeconds=5", "defaultReadiness.target=2.0"} {
		if _, _, err := render(t, img, bad); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}

func TestNetworkPolicyDeniesByDefaultAndAllowsListedSources(t *testing.T) {
	docs, _, _ := render(t, img)
	np := kinds(docs, "NetworkPolicy")[0]
	if len(list(m(np["spec"])["ingress"])) != 0 {
		t.Errorf("default policy allows ingress: %v", m(np["spec"])["ingress"])
	}
	docs, _, _ = render(t, img, "networkPolicy.metricsAllowFrom[0].namespaceSelector.matchLabels.team=monitoring")
	if len(list(m(kinds(docs, "NetworkPolicy")[0]["spec"])["ingress"])) != 1 {
		t.Error("listed source was not allowed")
	}
	docs, _, _ = render(t, img, "networkPolicy.enabled=false")
	if len(kinds(docs, "NetworkPolicy")) != 0 {
		t.Error("policy rendered while disabled")
	}
}

func TestEventsRoleIsNamespacedAndMinimal(t *testing.T) {
	docs, _, _ := render(t, img)
	found := false
	for _, r := range kinds(docs, "Role") {
		md := m(r["metadata"])
		if md["namespace"] == "default" {
			found = true
			rs := ruleSet(r)
			if len(rs) != 2 || !rs["events.k8s.io/events:create"] || !rs["events.k8s.io/events:patch"] {
				t.Errorf("events role: %v", rs)
			}
		}
	}
	if !found {
		t.Error("no events role")
	}
}

func TestLeaderRoleIsNamespacedAndOnlyWhenNeeded(t *testing.T) {
	docs, _, _ := render(t, img)
	var leader doc
	for _, r := range kinds(docs, "Role") {
		if m(r["metadata"])["namespace"] == "cgroupguard-system" {
			leader = r
		}
	}
	if leader == nil {
		t.Fatal("no leader election role")
	}
	rs := ruleSet(leader)
	want := map[string]bool{"coordination.k8s.io/leases:get": true, "coordination.k8s.io/leases:list": true, "coordination.k8s.io/leases:watch": true, "coordination.k8s.io/leases:create": true, "coordination.k8s.io/leases:update": true, "coordination.k8s.io/leases:patch": true, "/events:create": true, "/events:patch": true}
	for p := range rs {
		if !want[p] {
			t.Errorf("unexpected permission in the leader role: %s", p)
		}
	}
	docs, _, _ = render(t, img, "leaderElection=false")
	for _, r := range kinds(docs, "Role") {
		if m(r["metadata"])["namespace"] == "cgroupguard-system" {
			t.Error("a namespaced role remains without leader election")
		}
	}
}

func TestChartVersionMatchesTheBinary(t *testing.T) {
	data, err := os.ReadFile("../../charts/cgroupguard/Chart.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var c struct {
		Version    string `yaml:"version"`
		AppVersion string `yaml:"appVersion"`
	}
	if err := yaml.Unmarshal(data, &c); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "cgroupguard:"+version.Version) {
		t.Fatalf("the Artifact Hub images annotation does not mention image version %s", version.Version)
	}
	if c.Version != version.Version || c.AppVersion != version.Version {
		t.Fatalf("chart %s/%s does not match binary %s", c.Version, c.AppVersion, version.Version)
	}
}
