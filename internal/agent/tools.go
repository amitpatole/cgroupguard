package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/amitpatole/cgroupguard/internal/model"
	"github.com/amitpatole/cgroupguard/internal/plan"
	"github.com/amitpatole/cgroupguard/internal/redact"
	"github.com/amitpatole/cgroupguard/internal/rules"
)

// Suggestion is a bounded parameter proposal recorded from the model. It is shown as
// advice only; no code reads it to build commands or files.
type Suggestion struct {
	Group               string `json:"group"`
	MaxSurge            int    `json:"maxSurge"`
	DrainTimeoutMinutes int    `json:"drainTimeoutMinutes"`
}

// Env is everything tools can see: an already-collected report and plan. Tools never
// talk to a cluster, a network or the file system.
type Env struct {
	Report      model.Report
	Plan        plan.Plan
	R           *redact.Redactor
	SendImages  bool
	Docs        []DocSection
	Suggestions []Suggestion
	groups      map[string]bool
}

type Tool struct {
	Spec     ToolSpec
	Mutating bool
	Run      func(ctx context.Context, e *Env, args json.RawMessage) (string, error)
}

// Policy decides what the model may call. Mutating tools are denied unless the caller
// explicitly grants it, and no command line option in this version does.
type Policy struct{ AllowMutating bool }

var ErrDenied = fmt.Errorf("denied")

// Prepare assigns pseudonyms deterministically before any data is shown.
func (e *Env) Prepare() {
	var nodes, pools, nss, wls []string
	for _, n := range e.Report.Nodes {
		nodes = append(nodes, n.Name)
		if n.Pool != "" {
			pools = append(pools, n.Pool)
		}
	}
	for _, w := range e.Report.Workloads {
		nss = append(nss, w.Namespace)
		wls = append(wls, w.Name)
	}
	e.R.Prime("node", nodes)
	e.R.Prime("pool", dedupe(pools))
	e.R.Prime("ns", dedupe(nss))
	e.R.Prime("wl", dedupe(wls))
	e.groups = map[string]bool{}
	for _, g := range e.Plan.Groups {
		if g.Pool != "" {
			e.groups[e.R.Name("pool", g.Pool)] = true
		}
	}
}

func dedupe(l []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range l {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func (e *Env) resource(key string) string {
	p := strings.SplitN(key, "/", 5)
	if len(p) < 5 {
		return e.R.Name("res", key)
	}
	return e.R.Name("ns", p[0]) + "/" + p[1] + "/" + e.R.Name("wl", p[2]) + "/" + p[3]
}

func (e *Env) imageView(image string) map[string]any {
	family, class, status, state, why, matched := rules.Classify(image)
	v := map[string]any{"recognized": matched}
	if matched {
		v["family"], v["class"], v["ruleStatus"], v["state"], v["note"] = family, class, status, state, why
	} else {
		v["image"] = e.R.Name("img", image)
	}
	if e.SendImages {
		v["reference"] = e.R.Text(image)
	}
	return v
}

func marshal(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}

func str(args json.RawMessage, key string) (string, error) {
	var m map[string]any
	if len(args) > 0 {
		if err := json.Unmarshal(args, &m); err != nil {
			return "", fmt.Errorf("arguments must be a JSON object")
		}
	}
	s, _ := m[key].(string)
	if len(s) > 512 {
		return "", fmt.Errorf("argument %q is too long", key)
	}
	return s, nil
}

func obj(props map[string]any, required ...string) map[string]any {
	m := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		m["required"] = required
	}
	return m
}

func strProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

// Tools returns the fixed tool set. Names follow the project specification.
func Tools() []Tool {
	return []Tool{
		{Spec: ToolSpec{Name: "list_nodes", Description: "List nodes with pseudonymous names, provider, pool, cgroup mode, kubelet version and the kind of evidence behind the mode.", Schema: obj(map[string]any{})},
			Run: func(_ context.Context, e *Env, _ json.RawMessage) (string, error) {
				var out []map[string]any
				for _, n := range e.Report.Nodes {
					out = append(out, map[string]any{"node": e.R.Name("node", n.Name), "provider": n.Provider, "pool": e.R.Name("pool", n.Pool), "cgroup": n.Cgroup, "kubelet": n.Kubelet, "os": n.OS, "evidence": evidenceKind(n.Evidence)})
				}
				return marshal(out)
			}},
		{Spec: ToolSpec{Name: "probe_node", Description: "Return the recorded cgroup evidence for one node (no live probe is performed).", Schema: obj(map[string]any{"node": strProp("pseudonymous node name from list_nodes")}, "node")},
			Run: func(_ context.Context, e *Env, args json.RawMessage) (string, error) {
				want, err := str(args, "node")
				if err != nil {
					return "", err
				}
				for _, n := range e.Report.Nodes {
					if e.R.Name("node", n.Name) == want {
						return marshal(map[string]any{"node": want, "cgroup": n.Cgroup, "evidence": evidenceKind(n.Evidence), "note": "evidence text withheld; only its kind is shared"})
					}
				}
				return "", fmt.Errorf("unknown node")
			}},
		{Spec: ToolSpec{Name: "scan_workloads", Description: "List findings for workloads. Optional filters: ruleId, severity (high, warning, note). At most 40 results.", Schema: obj(map[string]any{"ruleId": strProp("for example CG101"), "severity": strProp("high, warning or note")})},
			Run: func(_ context.Context, e *Env, args json.RawMessage) (string, error) {
				rid, err := str(args, "ruleId")
				if err != nil {
					return "", err
				}
				sev, _ := str(args, "severity")
				byRes := map[string]model.Workload{}
				for _, w := range e.Report.Workloads {
					byRes[w.Namespace+"/"+w.Kind+"/"+w.Name+"/"+w.ContainerType+"/"+w.Container] = w
				}
				var out []map[string]any
				for _, f := range e.Report.Findings {
					if f.Resource == "" || (rid != "" && f.RuleID != rid) || (sev != "" && f.Severity != sev) {
						continue
					}
					if len(out) >= 40 {
						break
					}
					b := f.BlastRadius
					out = append(out, map[string]any{"rule": f.RuleID, "severity": f.Severity, "resource": e.resource(f.Resource), "image": e.imageView(byRes[f.Resource].Image),
						"why": e.R.Text(f.Why), "blast": map[string]any{"pods": b.PodsAffected, "nodes": b.NodesAffected, "scope": b.Scope, "failureMode": b.FailureMode, "recoverability": b.Recoverability.Rating, "criticality": b.Criticality}})
				}
				return marshal(out)
			}},
		{Spec: ToolSpec{Name: "inspect_image", Description: "Classify an image reference or an image pseudonym with the local rules (runtime family, rule status, note). Offline; nothing is pulled.", Schema: obj(map[string]any{"image": strProp("image reference or pseudonym such as img-2")}, "image")},
			Run: func(_ context.Context, e *Env, args json.RawMessage) (string, error) {
				img, err := str(args, "image")
				if err != nil {
					return "", err
				}
				for _, w := range e.Report.Workloads {
					if e.R.Name("img", w.Image) == img {
						img = w.Image
						break
					}
				}
				return marshal(e.imageView(img))
			}},
		{Spec: ToolSpec{Name: "lookup_docs", Description: "Search the project's verified runtime coverage and source notes. Returns up to 3 short excerpts.", Schema: obj(map[string]any{"query": strProp("keywords")}, "query")},
			Run: func(_ context.Context, e *Env, args json.RawMessage) (string, error) {
				q, err := str(args, "query")
				if err != nil {
					return "", err
				}
				return marshal(SearchDocs(e.Docs, q, 3))
			}},
		{Spec: ToolSpec{Name: "generate_patch", Description: "Describe the advisory change for one finding rule from the deterministic plan (text only; nothing is written or applied).", Schema: obj(map[string]any{"ruleId": strProp("for example CG101")}, "ruleId")},
			Run: func(_ context.Context, e *Env, args json.RawMessage) (string, error) {
				rid, err := str(args, "ruleId")
				if err != nil {
					return "", err
				}
				for _, ph := range e.Plan.Phases {
					for _, s := range ph.Steps {
						if s.ID == "W."+rid {
							return marshal(map[string]any{"advisoryOnly": true, "title": s.Title, "guidance": e.R.Text(s.Details), "verify": s.Verification, "rollback": s.Rollback})
						}
					}
				}
				return "", fmt.Errorf("no workload step exists for that rule")
			}},
		{Spec: ToolSpec{Name: "suggest_parameters", Description: "Record an advisory parameter suggestion for a node group. maxSurge 1-5, drainTimeoutMinutes 5-120. Advice only.", Schema: obj(map[string]any{"group": strProp("pseudonymous pool name"), "maxSurge": map[string]any{"type": "integer"}, "drainTimeoutMinutes": map[string]any{"type": "integer"}}, "group", "maxSurge", "drainTimeoutMinutes")},
			Run: func(_ context.Context, e *Env, args json.RawMessage) (string, error) {
				var s Suggestion
				dec := json.NewDecoder(strings.NewReader(string(args)))
				dec.DisallowUnknownFields()
				if err := dec.Decode(&s); err != nil {
					return "", fmt.Errorf("invalid arguments")
				}
				if !e.groups[s.Group] {
					return "", fmt.Errorf("unknown group")
				}
				if s.MaxSurge < 1 || s.MaxSurge > 5 || s.DrainTimeoutMinutes < 5 || s.DrainTimeoutMinutes > 120 {
					return "", fmt.Errorf("value out of range")
				}
				e.Suggestions = append(e.Suggestions, s)
				return `{"recorded":true,"note":"shown to the user as advice only"}`, nil
			}},
		{Spec: ToolSpec{Name: "open_pr", Description: "Open a pull request with generated changes. Requires explicit user approval and is NOT available to the model in this version.", Schema: obj(map[string]any{"title": strProp("title")}, "title")},
			Mutating: true,
			Run: func(context.Context, *Env, json.RawMessage) (string, error) {
				return "", fmt.Errorf("open_pr is not implemented for model use")
			}},
	}
}

func evidenceKind(ev string) string {
	l := strings.ToLower(ev)
	switch {
	case strings.Contains(l, "prometheus"):
		return "prometheus"
	case strings.Contains(l, "node proxy"), strings.Contains(l, "kubelet_cgroup_version"):
		return "kubelet-metric"
	case strings.Contains(l, "event"):
		return "kubelet-event"
	case strings.Contains(l, "statfs"), strings.Contains(l, "direct cgroup"):
		return "filesystem"
	case strings.Contains(l, "inventory only"):
		return "inventory-only"
	}
	return "other"
}

// Registry resolves and authorizes calls.
type Registry struct {
	tools  map[string]Tool
	Policy Policy
}

func NewRegistry(p Policy) *Registry {
	r := &Registry{tools: map[string]Tool{}, Policy: p}
	for _, t := range Tools() {
		r.tools[t.Spec.Name] = t
	}
	return r
}

func (r *Registry) Specs() []ToolSpec {
	var specs []ToolSpec
	for _, t := range r.tools {
		specs = append(specs, t.Spec)
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].Name < specs[j].Name })
	return specs
}

// Call runs a tool after the policy check. A denied or unknown call returns an error
// and never executes anything.
func (r *Registry) Call(ctx context.Context, e *Env, name string, args json.RawMessage) (string, error) {
	t, ok := r.tools[name]
	if !ok {
		return "", fmt.Errorf("%w: unknown tool", ErrDenied)
	}
	if t.Mutating && !r.Policy.AllowMutating {
		return "", fmt.Errorf("%w: %s changes state and the agent is read-only", ErrDenied, name)
	}
	return t.Run(ctx, e, args)
}
