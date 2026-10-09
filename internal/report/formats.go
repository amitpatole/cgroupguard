package report

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"sort"
	"strings"

	"github.com/amitpatole/cgroupguard/internal/model"
	"github.com/amitpatole/cgroupguard/internal/version"
)

func md(s string) string {
	s = html.EscapeString(s)
	return strings.NewReplacer("\\", "\\\\", "|", "\\|", "`", "\\`", "[", "\\[", "]", "\\]", "*", "\\*", "_", "\\_", "\r", " ", "\n", " ").Replace(s)
}

func markdown(w io.Writer, r model.Report) error {
	var b bytes.Buffer
	fmt.Fprintf(&b, "# CgroupGuard readiness report\n\nNode cgroup readiness: **%s**  \nWorkload assessment: **%s**  \nCluster upgrade readiness: **%s**\n\n", md(r.NodeReadiness), md(r.WorkloadReadiness), md(r.ClusterReadiness))
	fmt.Fprintln(&b, "| Node | Provider | Cgroup | Kubelet |\n|---|---|---|---|")
	for _, n := range r.Nodes {
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", md(n.Name), md(n.Provider), md(n.Cgroup), md(n.Kubelet))
	}
	fmt.Fprintln(&b, "\n## Findings")
	for _, f := range r.Findings {
		fmt.Fprintf(&b, "\n### %s — %s\n\n- Resource: %s\n- Severity: %s; confidence: %s\n- Evidence: %s\n- Why: %s\n- Blast radius: %s\n- Source: <%s>\n", f.RuleID, md(resource(f)), md(resource(f)), md(f.Severity), md(f.Confidence), md(f.Evidence), md(f.Why), md(f.BlastRadius.Summary()), f.Source)
	}
	if len(r.Findings) == 0 {
		fmt.Fprintln(&b, "\nNo known findings within the assessed scope.")
	}
	fmt.Fprint(&b, "\n## Limitations\n\n")
	for _, l := range r.Limitations {
		fmt.Fprintf(&b, "- %s\n", md(l))
	}
	_, e := w.Write(b.Bytes())
	return e
}

func sarif(w io.Writer, r model.Report) error {
	byID := map[string]model.Finding{}
	for _, f := range r.Findings {
		byID[f.RuleID] = f
	}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	indices := map[string]int{}
	descriptors := []any{}
	for i, id := range ids {
		indices[id] = i
		f := byID[id]
		descriptors = append(descriptors, map[string]any{"id": id, "shortDescription": map[string]string{"text": "Cgroup readiness: " + id}, "helpUri": f.Source})
	}
	results := []any{}
	for _, f := range r.Findings {
		level := "warning"
		if f.Severity == "high" {
			level = "error"
		}
		if f.Severity == "note" {
			level = "note"
		}
		key := resource(f)
		fingerprint := fmt.Sprintf("%x", sha256.Sum256([]byte(f.RuleID+"\x00"+key+"\x00"+f.Evidence)))
		results = append(results, map[string]any{"ruleId": f.RuleID, "ruleIndex": indices[f.RuleID], "level": level, "message": map[string]string{"text": f.Why + " Evidence: " + f.Evidence}, "locations": []any{map[string]any{"logicalLocations": []any{map[string]string{"fullyQualifiedName": key, "kind": "resource"}}}}, "partialFingerprints": map[string]string{"cgroupguard/v1": fingerprint}, "properties": map[string]any{"confidence": f.Confidence, "blastRadius": f.BlastRadius, "source": f.Source}})
	}
	doc := map[string]any{"$schema": "https://json.schemastore.org/sarif-2.1.0.json", "version": "2.1.0", "runs": []any{map[string]any{"tool": map[string]any{"driver": map[string]any{"name": "cgroupguard", "version": version.Version, "rules": descriptors}}, "results": results, "invocations": []any{map[string]any{"executionSuccessful": true, "toolExecutionNotifications": []any{map[string]any{"level": "warning", "message": map[string]string{"text": "Cluster readiness remains unknown: incomplete or heuristic coverage. A report with zero findings is not a passing upgrade certification."}}}}}, "properties": map[string]any{"scope": r.Scope, "target": r.Target, "nodeReadiness": r.NodeReadiness, "workloadReadiness": r.WorkloadReadiness, "clusterReadiness": r.ClusterReadiness, "limitations": r.Limitations}}}}
	e := json.NewEncoder(w)
	e.SetIndent("", "  ")
	return e.Encode(doc)
}

// kubent emits a JSON array shaped like kubent's -o json items (Name, Namespace,
// Kind, ApiVersion, RuleSet, ReplaceWith, Since), matching the Result struct in
// kubent 0.7.3 (pkg/judge/judge.go; its optional Labels field is omitted and Since
// is the text form of a go-version, for example "1.35.0"). It is shape-compatible only:
// ApiVersion carries the cgroupguard rule ID and ReplaceWith the explanation,
// because cgroup findings are not API deprecations. It has not been tested
// against upstream kubent fixtures and is not a drop-in replacement.
func kubent(w io.Writer, r model.Report) error {
	items := []map[string]string{}
	for _, f := range r.Findings {
		ns, kind, name := "", "Node", f.Node
		if f.Resource != "" {
			parts := strings.SplitN(f.Resource, "/", 5)
			ns, kind, name = parts[0], parts[1], parts[2]
		}
		items = append(items, map[string]string{"Name": name, "Namespace": ns, "Kind": kind, "ApiVersion": f.RuleID, "RuleSet": "cgroupguard", "ReplaceWith": f.Why, "Since": "1.35.0"})
	}
	e := json.NewEncoder(w)
	e.SetIndent("", "  ")
	return e.Encode(items)
}
