package report

import (
	"encoding/json"
	"fmt"
	"github.com/amitpatole/cgroupguard/internal/model"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
)

func Write(w io.Writer, r model.Report, format string) error {
	if format == "json" {
		e := json.NewEncoder(w)
		e.SetIndent("", "  ")
		return e.Encode(r)
	}
	if format == "markdown" {
		return markdown(w, r)
	}
	if format == "sarif" {
		return sarif(w, r)
	}
	if format == "kubent-compatible" {
		return kubent(w, r)
	}
	if format != "table" {
		return fmt.Errorf("unsupported format %q (use table or json)", format)
	}
	t := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if _, e := fmt.Fprintf(t, "Node cgroup readiness: %s\nWorkload assessment: %s\nCluster upgrade readiness: %s\n\nNODE\tPROVIDER\tCGROUP\tKUBELET\n", r.NodeReadiness, r.WorkloadReadiness, r.ClusterReadiness); e != nil {
		return e
	}
	for _, n := range r.Nodes {
		if _, e := fmt.Fprintf(t, "%s\t%s\t%s\t%s\n", safe(n.Name), safe(n.Provider), safe(n.Cgroup), safe(n.Kubelet)); e != nil {
			return e
		}
	}
	for _, f := range r.Findings {
		if _, e := fmt.Fprintf(t, "\n%s [%s] %s\n  Evidence: %s\n  Why: %s\n  Blast radius: %s\n  Source: %s\n", f.RuleID, f.Severity, safe(resource(f)), safe(f.Evidence), safe(f.Why), safe(f.BlastRadius.Summary()), f.Source); e != nil {
			return e
		}
	}
	for _, limit := range r.Limitations {
		if _, e := fmt.Fprintf(t, "\nLimitation: %s\n", safe(limit)); e != nil {
			return e
		}
	}
	return t.Flush()
}

func resource(f model.Finding) string {
	if f.Resource != "" {
		return f.Resource
	}
	return f.Node
}

// safe escapes control and non-printable characters in untrusted values so they
// cannot alter terminal output; ordinary text is shown without added quotes.
func safe(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == '\\' {
			b.WriteString(`\\`)
		} else if r < 0x20 || r == 0x7f || !strconv.IsPrint(r) {
			b.WriteString(strings.Trim(strconv.QuoteToASCII(string(r)), `"`))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
