// Package controller runs cgroupguard inside a cluster. It reconciles CgroupReadiness
// custom resources: it scans on a schedule, writes a compact status, emits Events when
// the picture changes, and exposes Prometheus metrics. It is read-only toward the
// cluster except for the status of its own resource and Events.
package controller

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/amitpatole/cgroupguard/internal/model"
	"github.com/amitpatole/cgroupguard/internal/rules"
)

const (
	Group   = "cgroupguard.io"
	Version = "v1alpha1"
	Kind    = "CgroupReadiness"

	MinInterval     = 5 * time.Minute
	MaxInterval     = 24 * time.Hour
	DefaultInterval = time.Hour
	maxPreview      = 20
	maxLimitations  = 10
)

var nsRe = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`)

// Spec is the validated desired state. Evidence sources that reach outside the cluster
// (Prometheus URL) or need extra permissions are chosen by the operator in the controller's
// own configuration, never by whoever can create this resource.
type Spec struct {
	Target            string
	Interval          time.Duration
	IncludeWorkloads  bool
	WorkloadNamespace string
	StatusDetail      string // summary | preview
}

// ParseSpec validates the untyped spec of a CgroupReadiness object.
func ParseSpec(raw map[string]any) (Spec, error) {
	s := Spec{Target: rules.DefaultTarget, Interval: DefaultInterval, IncludeWorkloads: true, StatusDetail: "summary"}
	known := map[string]bool{"target": true, "intervalSeconds": true, "includeWorkloads": true, "workloadNamespace": true, "statusDetail": true}
	for k := range raw {
		if !known[k] {
			return s, fmt.Errorf("unknown spec field %q", k)
		}
	}
	if v, ok := raw["target"]; ok {
		str, ok := v.(string)
		if !ok {
			return s, fmt.Errorf("spec.target must be a string")
		}
		t, err := rules.ParseTarget(str)
		if err != nil {
			return s, err
		}
		s.Target = t
	}
	if v, ok := raw["intervalSeconds"]; ok {
		var n int64
		switch x := v.(type) {
		case int64:
			n = x
		case float64:
			n = int64(x)
		default:
			return s, fmt.Errorf("spec.intervalSeconds must be an integer")
		}
		d := time.Duration(n) * time.Second
		if d < MinInterval || d > MaxInterval {
			return s, fmt.Errorf("spec.intervalSeconds must be between %d and %d", int(MinInterval.Seconds()), int(MaxInterval.Seconds()))
		}
		s.Interval = d
	}
	if v, ok := raw["includeWorkloads"]; ok {
		b, ok := v.(bool)
		if !ok {
			return s, fmt.Errorf("spec.includeWorkloads must be a boolean")
		}
		s.IncludeWorkloads = b
	}
	if v, ok := raw["workloadNamespace"]; ok {
		str, ok := v.(string)
		if !ok || (str != "" && !nsRe.MatchString(str)) {
			return s, fmt.Errorf("spec.workloadNamespace must be a namespace name")
		}
		s.WorkloadNamespace = str
	}
	if v, ok := raw["statusDetail"]; ok {
		str, _ := v.(string)
		if str != "summary" && str != "preview" {
			return s, fmt.Errorf("spec.statusDetail must be summary or preview")
		}
		s.StatusDetail = str
	}
	if s.WorkloadNamespace != "" && !s.IncludeWorkloads {
		return s, fmt.Errorf("spec.workloadNamespace needs spec.includeWorkloads")
	}
	return s, nil
}

// Counts is the numeric summary used for status, Events and metrics.
type Counts struct {
	Total, V1, V2, Hybrid, Unknown int
	High, Warning, Note            int
}

func Count(r model.Report) Counts {
	var c Counts
	for _, n := range r.Nodes {
		if n.OS != "linux" {
			continue
		}
		c.Total++
		switch n.Cgroup {
		case "v1":
			c.V1++
		case "v2":
			c.V2++
		case "hybrid":
			c.Hybrid++
		default:
			c.Unknown++
		}
	}
	for _, f := range r.Findings {
		switch f.Severity {
		case "high":
			c.High++
		case "warning":
			c.Warning++
		case "note":
			c.Note++
		}
	}
	return c
}

func clip(s string, n int) string {
	var b strings.Builder
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			b.WriteByte(' ')
		} else {
			b.WriteRune(r)
		}
	}
	out := b.String()
	if len(out) > n {
		out = out[:n] + "..."
	}
	return out
}

// BuildStatus renders the status object. Names appear only with statusDetail=preview.
func BuildStatus(r model.Report, sp Spec, generation int64, now time.Time, scanErr error, previous map[string]any) map[string]any {
	st := map[string]any{"observedGeneration": generation}
	prevTime := ""
	if cs, ok := previous["conditions"].([]any); ok {
		for _, c := range cs {
			if m, ok := c.(map[string]any); ok && m["type"] == "ScanSucceeded" {
				prevTime, _ = m["lastTransitionTime"].(string)
				if (m["status"] == "True") != (scanErr == nil) {
					prevTime = ""
				}
			}
		}
	}
	if prevTime == "" {
		prevTime = now.UTC().Format(time.RFC3339)
	}
	cond := map[string]any{"type": "ScanSucceeded", "lastTransitionTime": prevTime}
	if scanErr != nil {
		cond["status"], cond["reason"], cond["message"] = "False", "ScanFailed", clip(scanErr.Error(), 300)
		// A failed scan keeps the last good result visible instead of blanking it.
		for _, k := range []string{"lastScanTime", "target", "nodeReadiness", "workloadReadiness", "clusterReadiness", "nodes", "findings", "findingsPreview", "limitations"} {
			if v, ok := previous[k]; ok {
				st[k] = v
			}
		}
		st["lastAttemptTime"] = now.UTC().Format(time.RFC3339)
		st["conditions"] = []any{cond}
		return st
	}
	cond["status"], cond["reason"], cond["message"] = "True", "Scanned", "Scan completed; cluster readiness is never certified by this resource"
	c := Count(r)
	st["lastScanTime"] = now.UTC().Format(time.RFC3339)
	st["lastAttemptTime"] = st["lastScanTime"]
	st["target"] = r.Target
	st["nodeReadiness"] = r.NodeReadiness
	st["workloadReadiness"] = r.WorkloadReadiness
	st["clusterReadiness"] = r.ClusterReadiness
	st["nodes"] = map[string]any{"total": int64(c.Total), "cgroupV1": int64(c.V1), "cgroupV2": int64(c.V2), "hybrid": int64(c.Hybrid), "unknown": int64(c.Unknown)}
	st["findings"] = map[string]any{"high": int64(c.High), "warning": int64(c.Warning), "note": int64(c.Note)}
	if sp.StatusDetail == "preview" {
		var prev []any
		for _, f := range r.Findings {
			if f.Severity == "note" || len(prev) >= maxPreview {
				continue
			}
			where := f.Resource
			if where == "" {
				where = f.Node
			}
			prev = append(prev, map[string]any{"rule": f.RuleID, "severity": f.Severity, "resource": clip(where, 200)})
		}
		st["findingsPreview"] = prev
	}
	var lim []any
	for i, l := range r.Limitations {
		if i >= maxLimitations {
			break
		}
		lim = append(lim, clip(l, 300))
	}
	st["limitations"] = lim
	st["conditions"] = []any{cond}
	return st
}
