package report

import (
	"bytes"
	"encoding/json"
	"github.com/amitpatole/cgroupguard/internal/model"
	"strings"
	"testing"
)

func TestTerminalEscapeNeutralized(t *testing.T) {
	var b bytes.Buffer
	e := Write(&b, model.Report{Nodes: []model.Node{{Name: "n\x1b[2J\nforged"}}}, "table")
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(b.String(), "\x1b") {
		t.Fatal("terminal escape leaked")
	}
}

func TestMarkdownEscapesClusterData(t *testing.T) {
	var b bytes.Buffer
	e := Write(&b, model.Report{Nodes: []model.Node{{Name: "<script>alert(1)</script>|[link](javascript:bad)"}}}, "markdown")
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(b.String(), "<script>") || strings.Contains(b.String(), "|[link]") {
		t.Fatal("unescaped markdown/HTML")
	}
}

func TestSafeEscapesControlsWithoutQuotes(t *testing.T) {
	if got := safe("kubelet v1.37.0"); got != "kubelet v1.37.0" {
		t.Fatalf("added quotes: %q", got)
	}
	got := safe("evil\x1b[31m\nname\\")
	if strings.ContainsAny(got, "\x1b\n") || !strings.Contains(got, `\x1b`) || !strings.Contains(got, `\n`) || !strings.HasSuffix(got, `\\`) {
		t.Fatalf("unescaped control characters: %q", got)
	}
}

// Field names of kubent 0.7.3's judge.Result; cgroupguard's kubent-compatible items must
// use exactly these keys (plus nothing else) so existing parsers keep working.
func TestKubentCompatibleKeys(t *testing.T) {
	r := model.Report{Findings: []model.Finding{{RuleID: "CG101", Resource: "ns/Deployment/app/regular/c", Severity: "warning", Why: "w"}, {RuleID: "CG001", Node: "n1", Severity: "high", Why: "w"}}}
	var b bytes.Buffer
	if e := Write(&b, r, "kubent-compatible"); e != nil {
		t.Fatal(e)
	}
	var items []map[string]string
	if e := json.Unmarshal(b.Bytes(), &items); e != nil || len(items) != 2 {
		t.Fatal(e, b.String())
	}
	want := map[string]bool{"Name": true, "Namespace": true, "Kind": true, "ApiVersion": true, "RuleSet": true, "ReplaceWith": true, "Since": true}
	for _, it := range items {
		if len(it) != len(want) {
			t.Fatalf("keys %v", it)
		}
		for k := range it {
			if !want[k] {
				t.Fatalf("unexpected key %s", k)
			}
		}
	}
	if items[0]["Kind"] != "Deployment" || items[0]["Namespace"] != "ns" || items[1]["Kind"] != "Node" || items[1]["Since"] != "1.35.0" {
		t.Fatalf("%v", items)
	}
}

func FuzzSafeNeverEmitsControls(f *testing.F) {
	for _, s := range []string{"plain", "a\x1b[31mb", "line\nbreak", "tab\there", "\x00", "ünï", "\u202e"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		for _, r := range safe(in) {
			if r < 0x20 || r == 0x7f {
				t.Fatalf("control character %U survived from %q", r, in)
			}
		}
	})
}
