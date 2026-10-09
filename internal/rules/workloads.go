package rules

import (
	"bytes"
	_ "embed"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/amitpatole/cgroupguard/internal/detect"
	"github.com/amitpatole/cgroupguard/internal/model"
	"go.yaml.in/yaml/v3"
)

//go:embed workloads.yaml
var workloadData []byte

// ImageRule is one runtime/agent/platform family (see workloads.yaml).
type ImageRule struct {
	Family       string            `yaml:"family"`
	Class        string            `yaml:"class"`
	Status       string            `yaml:"status"`
	Mode         string            `yaml:"mode"`
	RiskID       string            `yaml:"riskId"`
	FailureMode  string            `yaml:"failureMode"`
	NativeFrom   string            `yaml:"nativeFrom"`
	Scheme       string            `yaml:"scheme"` // "java" parses 8uNNN tags
	Repositories []string          `yaml:"repositories"`
	Source       string            `yaml:"source"`
	Note         string            `yaml:"note"`
	Minimums     map[string]string `yaml:"minimums"`
}
type ImageRules struct {
	Version  int         `yaml:"version"`
	Families []ImageRule `yaml:"families"`
}

func LoadImages() (ImageRules, error) {
	var s ImageRules
	d := yaml.NewDecoder(bytes.NewReader(workloadData))
	d.KnownFields(true)
	if e := d.Decode(&s); e != nil {
		return s, e
	}
	return s, validateImages(s)
}

func validateImages(s ImageRules) error {
	if s.Version != 2 {
		return fmt.Errorf("invalid image rule schema")
	}
	failureModes := map[string]bool{"crash": true, "silent-degradation": true, "observability-loss": true, "unknown": true}
	seen := map[string]bool{}
	for _, r := range s.Families {
		if r.Family == "" || !strings.HasPrefix(r.Source, "https://") || len(r.Repositories) == 0 {
			return fmt.Errorf("invalid image rule %q", r.Family)
		}
		if r.Status != "verified" && r.Status != "unverified" || !failureModes[r.FailureMode] {
			return fmt.Errorf("invalid status or failureMode for %s", r.Family)
		}
		switch r.Mode {
		case "threshold":
			if len(r.Minimums) == 0 || r.RiskID == "" {
				return fmt.Errorf("threshold rule %s needs minimums and riskId", r.Family)
			}
		case "native-from":
			if _, ok := majorMinor(r.NativeFrom); !ok {
				return fmt.Errorf("native-from rule %s needs nativeFrom", r.Family)
			}
		case "review", "info":
		default:
			return fmt.Errorf("unknown mode %q for %s", r.Mode, r.Family)
		}
		// Unverified claims may never become a pass or a version threshold.
		if r.Status == "unverified" && r.Mode != "review" && r.Mode != "info" {
			return fmt.Errorf("unverified family %s may only use review or info", r.Family)
		}
		for _, repo := range r.Repositories {
			if seen[repo] {
				return fmt.Errorf("duplicate image repository %s", repo)
			}
			seen[repo] = true
		}
		for _, v := range r.Minimums {
			if _, ok := triple(v); !ok {
				return fmt.Errorf("invalid minimum version %s", v)
			}
		}
	}
	return nil
}

// imageParts normalizes only registry naming. Private lookalike names never
// inherit vendor rules. Digest-only images have no trustworthy runtime version.
func imageParts(image string) (string, string) {
	base := strings.SplitN(image, "@", 2)[0]
	tag := ""
	if i := strings.LastIndex(base, ":"); i > strings.LastIndex(base, "/") {
		tag = base[i+1:]
		base = base[:i]
	}
	if !strings.Contains(base, "/") {
		base = "docker.io/library/" + base
	} else {
		first := strings.SplitN(base, "/", 2)[0]
		if !strings.ContainsAny(first, ".:") && first != "localhost" {
			base = "docker.io/" + base
		}
	}
	base = strings.Replace(base, "index.docker.io/", "docker.io/", 1)
	if strings.HasPrefix(base, "docker.io/") && strings.Count(base, "/") == 1 {
		base = "docker.io/library/" + strings.TrimPrefix(base, "docker.io/")
	}
	return base, tag
}

var fullVersion = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)(?:[+_-].*)?$`)
var java8 = regexp.MustCompile(`^8u(\d+)(?:[-_+].*)?$`)

func triple(s string) ([3]int, bool) {
	var v [3]int
	m := fullVersion.FindStringSubmatch(s)
	if m == nil {
		return v, false
	}
	for i := range v {
		n, e := strconv.Atoi(m[i+1])
		if e != nil {
			return v, false
		}
		v[i] = n
	}
	return v, true
}
func less(a, b [3]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

var leadMajorMinor = regexp.MustCompile(`^v?(\d+)\.(\d+)(?:[.+_-].*)?$`)

func majorMinor(s string) ([2]int, bool) {
	m := leadMajorMinor.FindStringSubmatch(s)
	if m == nil {
		return [2]int{}, false
	}
	a, e1 := strconv.Atoi(m[1])
	b, e2 := strconv.Atoi(m[2])
	return [2]int{a, b}, e1 == nil && e2 == nil
}

// assess returns the state (risk, hint, review, info) and the explanation.
func assess(rule ImageRule, tag string) (string, string) {
	if rule.Mode == "info" {
		return "info", rule.Note
	}
	for _, marker := range []string{"-ea", "-rc", "-beta", "-alpha"} {
		if strings.Contains(strings.ToLower(tag), marker) {
			return "review", "Prerelease tags require exact vendor and runtime validation."
		}
	}
	if strings.Contains(strings.ToLower(tag), "openj9") {
		return "review", "Runtime variant cannot be assessed by HotSpot rules."
	}
	if rule.Mode == "review" {
		return "review", rule.Note
	}
	if rule.Mode == "native-from" {
		have, ok := majorMinor(tag)
		want, _ := majorMinor(rule.NativeFrom)
		if !ok {
			return "review", "Image tag does not establish an exact toolchain version. " + rule.Note
		}
		if have[0] < want[0] || have[0] == want[0] && have[1] < want[1] {
			return "review", "Toolchain below " + rule.NativeFrom + ". " + rule.Note
		}
		return "hint", "Toolchain meets " + rule.NativeFrom + ". " + rule.Note
	}
	v, ok := triple(tag)
	if rule.Scheme == "java" {
		if m := java8.FindStringSubmatch(tag); m != nil {
			n, e := strconv.Atoi(m[1])
			v = [3]int{8, n, 0}
			ok = e == nil
		}
	}
	partial := false
	if !ok {
		// A major.minor tag (for example "6.0") fixes the patch only up to that line.
		if mm, ok2 := majorMinor(tag); ok2 && !java8.MatchString(tag) {
			v, ok, partial = [3]int{mm[0], mm[1], 0}, true, true
		}
	}
	if !ok {
		return "review", "Image tag does not establish an exact runtime version."
	}
	min := rule.Minimums["all"]
	if min == "" {
		min = rule.Minimums[strconv.Itoa(v[0])]
		if v[0] >= 15 {
			min = rule.Minimums["15"]
		}
	}
	if min == "" {
		return "review", "No verified compatibility threshold exists for this version line."
	}
	threshold, _ := triple(min)
	if partial && v[0] == threshold[0] && v[1] == threshold[1] && threshold[2] > 0 {
		return "review", "Image tag names only the " + tag + " line; the threshold " + min + " falls inside it."
	}
	if partial && v[0] == threshold[0] && v[1] == threshold[1] {
		return "hint", "Tag line meets the documented baseline, but an image tag alone cannot prove workload compatibility."
	}
	if less(v, threshold) {
		return "risk", strings.TrimSpace("Image tag suggests a version below the documented cgroup v2 support threshold " + min + "; verify the actual binary and vendor backports. " + rule.Note)
	}
	return "hint", "Tag meets the documented baseline, but an image tag alone cannot prove workload compatibility."
}

func appendUnique(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

func AddWorkloads(r *model.Report, workloads []model.Workload) error {
	r.WorkloadReadiness = "unassessed"
	if workloads == nil {
		return nil
	}
	set, e := LoadImages()
	if e != nil {
		return e
	}
	r.Scope = "nodes-and-workload-metadata"
	r.Workloads = append([]model.Workload{}, workloads...)
	r.WorkloadReadiness = "no-known-findings"
	kept := []string{}
	for _, l := range r.Limitations {
		if !strings.HasPrefix(l, "Workload inventory was not supplied") {
			kept = append(kept, l)
		}
	}
	r.Limitations = kept
	r.Limitations = appendUnique(r.Limitations, "Workload analysis uses image tags and allowlisted manifest path references; no image layers, runtime binaries, mounted configs or application behavior are inspected.")
	r.Limitations = appendUnique(r.Limitations, "Kubelet configuration, enabled cgroup controllers and other upgrade prerequisites remain unassessed. A clean heuristic scan is not proof of cluster readiness.")
	r.Limitations = appendUnique(r.Limitations, "Offline snapshots are user-supplied, unauthenticated evidence with no freshness guarantee.")
	sort.Slice(r.Workloads, func(i, j int) bool { return detect.WorkloadKey(r.Workloads[i]) < detect.WorkloadKey(r.Workloads[j]) })
	seen := map[string]bool{}
	for _, w := range r.Workloads {
		key := detect.WorkloadKey(w)
		if w.Namespace == "" || w.Name == "" || w.Container == "" || w.Image == "" || seen[key] {
			return fmt.Errorf("invalid or duplicate workload %q", key)
		}
		seen[key] = true
		switch w.Kind {
		case "Pod", "Deployment", "StatefulSet", "DaemonSet", "Job", "CronJob":
		default:
			return fmt.Errorf("unsupported workload kind %q", w.Kind)
		}
		switch w.ContainerType {
		case "regular", "init", "ephemeral":
		default:
			return fmt.Errorf("invalid container type %q", w.ContainerType)
		}
		switch w.PDB {
		case "", "present", "none", "unknown":
		default:
			return fmt.Errorf("invalid pdb value for %s", key)
		}
		for node, n := range w.Placements {
			if node == "" || n < 0 {
				return fmt.Errorf("invalid placements for %s", key)
			}
		}
		add := func(id, severity, confidence, evidence, why, source, failureMode string) {
			r.Findings = append(r.Findings, model.Finding{RuleID: id, Resource: key, Severity: severity, Confidence: confidence, Evidence: evidence, Why: why, Source: source, BlastRadius: workloadBlast(w, r.Nodes, failureMode)})
			if severity == "warning" {
				r.WorkloadReadiness = "review-required"
			}
		}
		paths := map[string]bool{}
		for _, p := range w.PathSignals {
			valid := false
			for _, allowed := range detect.CgroupPaths {
				valid = valid || p == allowed
			}
			if !valid {
				return fmt.Errorf("unrecognized path signal for %s", key)
			}
			if paths[p] {
				continue
			}
			paths[p] = true
			add("CG103", "warning", "high", "Manifest contains legacy path prefix "+p, "Direct reads of cgroup v1 controller paths can fail on v2. Inspect the referenced command or mount; the tool does not execute it.", "https://kubernetes.io/docs/concepts/architecture/cgroups/", "unknown")
		}
		repo, tag := imageParts(w.Image)
		matched := false
		for _, rule := range set.Families {
			for _, candidate := range rule.Repositories {
				if repo != candidate {
					continue
				}
				matched = true
				state, why := assess(rule, tag)
				id, sev := "CG105", "warning"
				switch state {
				case "risk":
					id = rule.RiskID
				case "hint":
					id, sev = "CG106", "note"
				case "info":
					id, sev = "CG107", "note"
				}
				confidence := "low"
				add(id, sev, confidence, "Image-tag heuristic for "+rule.Family+" ("+rule.Class+", "+rule.Status+" rule): "+w.Image, why, rule.Source, rule.FailureMode)
			}
		}
		if !matched {
			add("CG104", "warning", "unknown", "Unrecognized image: "+w.Image, "No verified rule covers this image. Inspect its runtime and custom cgroup access before migration.", "https://kubernetes.io/docs/concepts/architecture/cgroups/", "unknown")
		}
	}
	// No workload metadata can prove complete cluster compatibility.
	r.ClusterReadiness = "unknown"
	FinalizeBlastRadius(r)
	return nil
}

// Classify reports how the family rules see one image reference, without needing a
// report. matched is false for images no family covers.
func Classify(image string) (family, class, status, state, why string, matched bool) {
	set, err := LoadImages()
	if err != nil {
		return "", "", "", "", "", false
	}
	repo, tag := imageParts(image)
	for _, rule := range set.Families {
		for _, candidate := range rule.Repositories {
			if repo == candidate {
				st, w := assess(rule, tag)
				return rule.Family, rule.Class, rule.Status, st, w, true
			}
		}
	}
	return "", "", "", "", "", false
}
