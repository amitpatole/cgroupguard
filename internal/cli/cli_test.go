package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRejectMalformedSnapshots(t *testing.T) {
	for _, s := range []string{`{}`, `{"schemaVersion":1,"typo":true}`, `{"schemaVersion":1} {}`, `{"schemaVersion":2}`, `not-json`} {
		if _, e := ReadSnapshot(strings.NewReader(s)); e == nil {
			t.Fatalf("accepted %s", s)
		}
	}
}

func TestExitCodesAndSafeGate(t *testing.T) {
	p := filepath.Join(t.TempDir(), "nodes.json")
	if e := os.WriteFile(p, []byte(`{"schemaVersion":1,"nodes":[{"name":"n","os":"linux","cgroup":"v2","evidence":"test statfs"}]}`), 0600); e != nil {
		t.Fatal(e)
	}
	var out, err bytes.Buffer
	if code := Run([]string{"check", "--input", p, "--format", "json"}, &out, &err); code != 1 {
		t.Fatalf("code %d stderr %s", code, err.String())
	}
	if !strings.Contains(out.String(), `"clusterReadiness": "unknown"`) {
		t.Fatal(out.String())
	}
	for _, args := range [][]string{{"check", "--input", p, "--format", "bogus"}, {"check", "--input", p, "--probe"}, {"check", "--input", p, "--local"}, {"check", "--input", p, "--path", "/tmp"}, {"check", "--timeout", "0s"}} {
		if Run(args, &out, &err) != 2 {
			t.Fatalf("accepted args %v", args)
		}
	}
	if Run([]string{"--help"}, &out, &err) != 0 {
		t.Fatal("help failed")
	}
}

func writeSnapshot(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "s.json")
	if e := os.WriteFile(p, []byte(body), 0600); e != nil {
		t.Fatal(e)
	}
	return p
}

const v1Node = `{"schemaVersion":1,"nodes":[{"name":"n","os":"linux","cgroup":"v1","evidence":"test statfs"}]}`
const v2Node = `{"schemaVersion":1,"nodes":[{"name":"n","os":"linux","cgroup":"v2","evidence":"test statfs"}]}`

func TestExitErrorFollowsKubentSemantics(t *testing.T) {
	var out, err bytes.Buffer
	// Findings at or above --fail-on: 200 in exit-error mode, 1 in default mode.
	if c := Run([]string{"check", "--input", writeSnapshot(t, v1Node), "--exit-error"}, &out, &err); c != 200 {
		t.Fatalf("v1 node with --exit-error: %d", c)
	}
	if c := Run([]string{"check", "--input", writeSnapshot(t, v1Node)}, &out, &err); c != 1 {
		t.Fatalf("default mode: %d", c)
	}
	// No findings: exit-error mode returns 0 but must say coverage is incomplete.
	err.Reset()
	if c := Run([]string{"check", "--input", writeSnapshot(t, v2Node), "-e"}, &out, &err); c != 0 || !strings.Contains(err.String(), "coverage is incomplete") {
		t.Fatalf("clean v2 node with -e: %d %q", c, err.String())
	}
	// --fail-on high ignores warnings; --fail-on none never fails in exit-error mode.
	unknown := `{"schemaVersion":1,"nodes":[{"name":"n","os":"linux","cgroup":"unknown","evidence":"inventory only"}]}`
	if c := Run([]string{"check", "--input", writeSnapshot(t, unknown), "-e", "--fail-on", "high"}, &out, &err); c != 0 {
		t.Fatalf("warning with fail-on high: %d", c)
	}
	if c := Run([]string{"check", "--input", writeSnapshot(t, v1Node), "-e", "--fail-on", "none"}, &out, &err); c != 0 {
		t.Fatalf("fail-on none: %d", c)
	}
	// Errors stay 2 in every mode.
	for _, args := range [][]string{{"check", "--input", writeSnapshot(t, v1Node), "-e", "--fail-on", "bogus"}, {"check", "--input", "/nonexistent", "-e"}} {
		if c := Run(args, &out, &err); c != 2 {
			t.Fatalf("%v: %d", args, c)
		}
	}
}

func TestTargetVersionAndOutputAlias(t *testing.T) {
	var out, err bytes.Buffer
	p := writeSnapshot(t, v1Node)
	if c := Run([]string{"check", "--input", p, "-t", "1.34", "-o", "json"}, &out, &err); c != 1 || !strings.Contains(out.String(), `"target": "1.34"`) || !strings.Contains(out.String(), `"nodeReadiness": "needs-migration"`) {
		t.Fatalf("%d %s %s", c, out.String(), err.String())
	}
	out.Reset()
	if c := Run([]string{"check", "--input", p, "--format", "kubent-compatible"}, &out, &err); c != 1 || !strings.Contains(out.String(), `"RuleSet": "cgroupguard"`) || !strings.Contains(out.String(), `"Kind": "Node"`) {
		t.Fatalf("%d %s", c, out.String())
	}
	for _, args := range [][]string{{"check", "--input", p, "-t", "9.9"}, {"check", "--input", p, "-o", "json", "--format", "table"}, {"check", "--input", p, "--kubelet-metrics"}, {"check", "--input", p, "--prometheus-url", "http://x"}} {
		if c := Run(args, &out, &err); c != 2 {
			t.Fatalf("accepted %v", args)
		}
	}
}

func FuzzReadSnapshot(f *testing.F) {
	f.Add(v1Node)
	f.Add(`{"schemaVersion":1,"nodes":[],"workloads":[{"namespace":"n","kind":"Pod","name":"p","container":"c","containerType":"regular","image":"x","placements":{"a":1}}]}`)
	f.Add(`{`)
	f.Fuzz(func(t *testing.T, in string) {
		s, e := ReadSnapshot(strings.NewReader(in))
		if e == nil && s.SchemaVersion != 1 {
			t.Fatalf("accepted schema %d", s.SchemaVersion)
		}
	})
}

func TestSnapshotRejectsSpecialFiles(t *testing.T) {
	dir := t.TempDir()
	for _, p := range []string{dir, "/dev/null", "/dev/zero", "/nonexistent/x.json"} {
		var out, err bytes.Buffer
		if c := Run([]string{"check", "--input", p}, &out, &err); c != 2 {
			t.Fatalf("%s accepted (exit %d)", p, c)
		}
	}
}
