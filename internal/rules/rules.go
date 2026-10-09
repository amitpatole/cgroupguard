package rules

import (
	"bytes"
	_ "embed"
	"fmt"
	"github.com/amitpatole/cgroupguard/internal/model"
	"go.yaml.in/yaml/v3"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

//go:embed nodes.yaml
var data []byte

type Rule struct {
	ID       string `yaml:"id"`
	Match    string `yaml:"match"`
	Severity string `yaml:"severity"`
	// Since is the first Kubernetes version at which Severity applies; for an
	// earlier target the finding is reported at EarlierSeverity instead.
	Since           string `yaml:"since"`
	EarlierSeverity string `yaml:"earlierSeverity"`
	Why             string `yaml:"why"`
	Source          string `yaml:"source"`
}
type Set struct {
	Version int    `yaml:"version"`
	Rules   []Rule `yaml:"rules"`
}

func Load() (Set, error) {
	var s Set
	d := yaml.NewDecoder(bytes.NewReader(data))
	d.KnownFields(true)
	if err := d.Decode(&s); err != nil {
		return s, err
	}
	if s.Version != 1 {
		return s, fmt.Errorf("unsupported rule schema")
	}
	seen := map[string]bool{}
	for _, r := range s.Rules {
		if seen[r.Match] || r.ID == "" || r.Why == "" || !strings.HasPrefix(r.Source, "https://") {
			return s, fmt.Errorf("invalid rule %q", r.ID)
		}
		seen[r.Match] = true
		if r.Since != "" {
			if _, ok := majorMinor(r.Since); !ok || r.EarlierSeverity == "" {
				return s, fmt.Errorf("invalid since/earlierSeverity for rule %q", r.ID)
			}
		}
	}
	for _, mode := range []string{"v1", "hybrid", "unknown"} {
		if !seen[mode] {
			return s, fmt.Errorf("missing rule for %s", mode)
		}
	}
	return s, nil
}

// DefaultTarget is the Kubernetes minor version findings are scaled to by default.
const DefaultTarget = "1.35"

// ParseTarget accepts "1.35", "v1.35" or "1.35.2" and returns "major.minor".
var targetPattern = regexp.MustCompile(`^v?1\.(\d{2})(?:\.\d+)?$`)

func ParseTarget(v string) (string, error) {
	m := targetPattern.FindStringSubmatch(strings.TrimSpace(v))
	if m == nil {
		return "", fmt.Errorf("invalid --target-version %q (use a Kubernetes version such as 1.35)", v)
	}
	minor, _ := strconv.Atoi(m[1])
	if minor < 20 {
		return "", fmt.Errorf("invalid --target-version %q (minimum supported is 1.20)", v)
	}
	return fmt.Sprintf("1.%d", minor), nil
}

func Evaluate(s Set, nodes []model.Node) (model.Report, error) {
	return EvaluateFor(s, nodes, DefaultTarget)
}

// EvaluateFor scales node severities to the planned upgrade target.
func EvaluateFor(s Set, nodes []model.Node, target string) (model.Report, error) {
	target, err := ParseTarget(target)
	if err != nil {
		return model.Report{}, err
	}
	targetMM, _ := majorMinor(target)
	if len(nodes) == 0 {
		return model.Report{}, fmt.Errorf("no nodes found; empty scans cannot pass")
	}
	r := model.Report{SchemaVersion: 2, Target: target, Scope: "node-cgroup-only", NodeReadiness: "pass", ClusterReadiness: "unknown", Nodes: append([]model.Node(nil), nodes...), Findings: []model.Finding{}, Limitations: []string{"Workload inventory was not supplied or requested. Kubelet configuration, cgroup controllers, runtime settings and other upgrade prerequisites remain unassessed.", "Offline evidence is user-supplied and is not authenticated or checked for freshness."}}
	sort.Slice(r.Nodes, func(i, j int) bool { return r.Nodes[i].Name < r.Nodes[j].Name })
	seen := map[string]bool{}
	linuxCount := 0
	for _, n := range r.Nodes {
		if n.Name == "" || seen[n.Name] {
			return r, fmt.Errorf("empty or duplicate node name %q", n.Name)
		}
		seen[n.Name] = true
		if n.OS == "windows" {
			continue
		}
		if n.OS != "linux" {
			return r, fmt.Errorf("node %s: os must be linux or windows", n.Name)
		}
		linuxCount++
		if n.Cgroup != "v1" && n.Cgroup != "v2" && n.Cgroup != "hybrid" && n.Cgroup != "unknown" {
			return r, fmt.Errorf("node %s: invalid cgroup value %q", n.Name, n.Cgroup)
		}
		if strings.TrimSpace(n.Evidence) == "" {
			return r, fmt.Errorf("node %s: evidence is required", n.Name)
		}
		for _, rule := range s.Rules {
			if rule.Match != n.Cgroup {
				continue
			}
			confidence := "high"
			if n.Cgroup == "unknown" {
				confidence = "unknown"
			}
			severity, why := rule.Severity, rule.Why
			if rule.Since != "" {
				since, _ := majorMinor(rule.Since)
				if targetMM[0] < since[0] || targetMM[0] == since[0] && targetMM[1] < since[1] {
					severity = rule.EarlierSeverity
					why += " Target " + target + " is below " + rule.Since + ", where this becomes blocking; the node must still be migrated before upgrading to " + rule.Since + " or later."
				}
			}
			r.Findings = append(r.Findings, model.Finding{RuleID: rule.ID, Node: n.Name, Severity: severity, Evidence: n.Evidence, Why: why, Confidence: confidence, Source: rule.Source})
			switch {
			case severity == "high":
				r.NodeReadiness = "blocked"
			case rule.Match != "unknown" && r.NodeReadiness != "blocked":
				// Known v1/hybrid node below the blocking target: migration is needed, not blocking.
				r.NodeReadiness = "needs-migration"
			case r.NodeReadiness == "pass":
				r.NodeReadiness = "unknown"
			}
		}
	}
	if linuxCount == 0 {
		r.NodeReadiness = "not-applicable"
	}
	FinalizeBlastRadius(&r)
	return r, nil
}
