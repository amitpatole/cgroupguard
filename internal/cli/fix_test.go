package cli

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amitpatole/cgroupguard/internal/plan"
)

const planFixture = "../../examples/plan-mixed.json"

func TestFixRequiresPlanAndNeverApplies(t *testing.T) {
	var out, err bytes.Buffer
	for _, args := range [][]string{{"fix", "--input", planFixture}, {"fix", "--plan", "--input", planFixture, "--emit", "terraform"}, {"fix", "--plan", "--input", planFixture, "--emit", "terraform", "--output-dir", "x"}, {"fix", "--plan", "--input", planFixture, "--emit", "terraform", "--output-dir", "x", "--validate", "--no-validate"}, {"fix", "--plan", "--input", planFixture, "--validate"}, {"fix", "--plan", "--input", planFixture, "--format", "yaml"}, {"fix", "--plan", "--input", planFixture, "--stopgap-until", "someday"}} {
		out.Reset()
		if c := Run(args, &out, &err); c != 2 {
			t.Fatalf("%v: exit %d", args, c)
		}
	}
}

func TestFixPrintsRunbookAndJSON(t *testing.T) {
	var out, err bytes.Buffer
	if c := Run([]string{"fix", "--plan", "--input", planFixture}, &out, &err); c != 0 || !strings.Contains(out.String(), "Nothing in this document has been executed") {
		t.Fatalf("exit %d %s", c, err.String())
	}
	out.Reset()
	if c := Run([]string{"fix", "--plan", "--input", planFixture, "--format", "json"}, &out, &err); c != 0 || !strings.Contains(out.String(), `"schemaVersion": 1`) {
		t.Fatalf("exit %d %s", c, err.String())
	}
}

func TestFixWritesFilesWithoutOverwriting(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "out")
	var out, err bytes.Buffer
	args := []string{"fix", "--plan", "--input", planFixture, "--output-dir", dir, "--emit", "terraform,ansible", "--no-validate"}
	if c := Run(args, &out, &err); c != 0 {
		t.Fatalf("exit %d %s", c, err.String())
	}
	for _, p := range []string{"RUNBOOK.md", "plan.json", "terraform/gke-pool-legacy/main.tf", "ansible/cgroupv2-migrate.yml", "gke/pool-legacy-system-config.yaml"} {
		if _, e := os.Stat(filepath.Join(dir, p)); e != nil {
			t.Fatalf("missing %s: %v", p, e)
		}
	}
	rb, _ := os.ReadFile(filepath.Join(dir, "RUNBOOK.md"))
	if !strings.Contains(string(rb), "NOT validated") {
		t.Fatal("unvalidated output is not recorded in the runbook")
	}
	if c := Run(args, &out, &err); c != 2 || !strings.Contains(err.String(), "already exists") {
		t.Fatalf("overwrote an existing directory: exit %d", c)
	}
	if c := Run(append(args, "--overwrite"), &out, &err); c != 0 {
		t.Fatalf("--overwrite failed: %d %s", c, err.String())
	}
}

func TestFixRefusesWhenValidatorsAreMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // no terraform, no ansible
	dir := filepath.Join(t.TempDir(), "out")
	var out, err bytes.Buffer
	c := Run([]string{"fix", "--plan", "--input", planFixture, "--output-dir", dir, "--emit", "terraform", "--validate"}, &out, &err)
	if c != 2 || !strings.Contains(err.String(), "nothing was written") {
		t.Fatalf("exit %d %s", c, err.String())
	}
	if _, e := os.Stat(dir); e == nil {
		t.Fatal("output directory was created although validation could not run")
	}
}

func TestWriteOutputsConfinesPaths(t *testing.T) {
	dir := t.TempDir()
	for _, bad := range []string{"../escape.txt", "/etc/escape.txt", "a/../../escape.txt", ""} {
		if e := writeOutputs(filepath.Join(dir, "o"), []plan.File{{Path: bad, Content: "x"}}, false); e == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	if e := writeOutputs(filepath.Join(dir, "o2"), []plan.File{{Path: "a.txt", Content: "1"}, {Path: "a.txt", Content: "2"}}, false); e == nil {
		t.Fatal("accepted duplicate paths")
	}
	// A symlink inside the output directory must not let a write escape it.
	outside := filepath.Join(dir, "outside")
	if e := os.MkdirAll(outside, 0o750); e != nil {
		t.Fatal(e)
	}
	out := filepath.Join(dir, "o3")
	if e := os.MkdirAll(out, 0o750); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(outside, filepath.Join(out, "link")); e != nil {
		t.Fatal(e)
	}
	if e := writeOutputs(out, []plan.File{{Path: "link/pwn.txt", Content: "x"}}, true); e == nil {
		t.Fatal("write escaped through a symlink")
	}
	if _, e := os.Stat(filepath.Join(outside, "pwn.txt")); e == nil {
		t.Fatal("file landed outside the output directory")
	}
}

func FuzzWriteOutputsPaths(f *testing.F) {
	for _, s := range []string{"a.txt", "../x", "/abs", "a/../../x", "a//b", ".", "..", "a/./b", "link/x"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, p string) {
		base := t.TempDir()
		dir := filepath.Join(base, "out")
		_ = writeOutputs(dir, []plan.File{{Path: p, Content: "x"}}, true)
		// Nothing may appear outside dir, whatever the path says.
		entries, _ := os.ReadDir(base)
		for _, e := range entries {
			if e.Name() != "out" {
				t.Fatalf("path %q created %s outside the output directory", p, e.Name())
			}
		}
	})
}

func TestFixModesAreExclusiveAndAuditedActionsNeedALog(t *testing.T) {
	var out, err bytes.Buffer
	dir := t.TempDir()
	for _, args := range [][]string{
		{"fix", "--plan", "--apply", "--input", planFixture},
		{"fix", "--apply", "--pr", "--input", planFixture},
		{"fix", "--apply"}, // no audit log
		{"fix", "--pr", "--pr-repo", "acme/infra", "--input", planFixture},                  // no audit log
		{"fix", "--apply", "--audit-log", filepath.Join(dir, "a1"), "--input", planFixture}, // needs a live cluster
		{"fix", "--plan", "--dry-run", "--input", planFixture},
		{"fix", "--pr", "--audit-log", filepath.Join(dir, "a2"), "--input", planFixture}, // no repo
		{"fix", "--pr", "--pr-repo", "not a repo", "--audit-log", filepath.Join(dir, "a3"), "--input", planFixture},
	} {
		if c := Run(args, &out, &err); c != 2 {
			t.Fatalf("%v accepted (exit %d)", args, c)
		}
	}
}

func withTerminal(t *testing.T, input string) {
	t.Helper()
	oldIn, oldTTY := stdin, stdinIsTerminal
	stdin, stdinIsTerminal = strings.NewReader(input), func() bool { return true }
	t.Cleanup(func() { stdin, stdinIsTerminal = oldIn, oldTTY })
}

func TestPRDryRunNeedsNoNetworkOrToken(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "audit.jsonl")
	var out, err bytes.Buffer
	c := Run([]string{"fix", "--pr", "--dry-run", "--pr-repo", "acme/infra", "--audit-log", logPath, "--input", planFixture, "--emit", "terraform", "--no-validate", "--output-dir", filepath.Join(t.TempDir(), "o")}, &out, &err)
	if c != 0 || !strings.Contains(out.String(), "DRY RUN, no network access") || !strings.Contains(out.String(), "DRAFT") {
		t.Fatalf("exit %d out=%s err=%s", c, out.String(), err.String())
	}
}

func TestPRNeedsATerminalAndTheTypedRepository(t *testing.T) {
	const token = "ghp" + "_CLITESTTOKENCLITESTTOKEN1234567890"
	t.Setenv("PR_TEST_TOKEN", token)
	var hits []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, r.Method+" "+r.URL.Path)
		switch {
		case strings.Contains(r.URL.Path, "/git/ref/heads/"):
			io.WriteString(w, `{"object":{"sha":"s"}}`)
		case strings.Contains(r.URL.Path, "/git/commits/"):
			io.WriteString(w, `{"tree":{"sha":"t"}}`)
		case strings.HasSuffix(r.URL.Path, "/git/trees"):
			io.WriteString(w, `{"sha":"nt"}`)
		case strings.HasSuffix(r.URL.Path, "/git/commits"):
			io.WriteString(w, `{"sha":"nc"}`)
		case strings.HasSuffix(r.URL.Path, "/git/refs"):
			w.WriteHeader(201)
			io.WriteString(w, `{}`)
		case strings.HasSuffix(r.URL.Path, "/pulls"):
			w.WriteHeader(201)
			io.WriteString(w, `{"html_url":"https://example.test/pr/1"}`)
		}
	}))
	defer srv.Close()
	args := func(log string) []string {
		return []string{"fix", "--pr", "--pr-repo", "acme/infra", "--pr-base-url", srv.URL, "--pr-token-env", "PR_TEST_TOKEN", "--audit-log", log, "--input", planFixture}
	}
	var out, err bytes.Buffer
	// Not a terminal: refused, nothing sent.
	oldTTY := stdinIsTerminal
	stdinIsTerminal = func() bool { return false }
	if c := Run(args(filepath.Join(t.TempDir(), "a0")), &out, &err); c != 2 || len(hits) != 0 {
		t.Fatalf("piped confirmation accepted: exit %d hits %v", c, hits)
	}
	stdinIsTerminal = oldTTY
	// Terminal, wrong answer: stops without any request.
	withTerminal(t, "yes\n")
	out.Reset()
	if c := Run(args(filepath.Join(t.TempDir(), "a1")), &out, &err); c != 0 || len(hits) != 0 || !strings.Contains(out.String(), "nothing was sent") {
		t.Fatalf("exit %d hits %v %s", c, hits, out.String())
	}
	// Terminal, typed repository: opens a draft.
	withTerminal(t, "acme/infra\n")
	logPath := filepath.Join(t.TempDir(), "a2")
	out.Reset()
	if c := Run(args(logPath), &out, &err); c != 0 || !strings.Contains(out.String(), "Opened draft: https://example.test/pr/1") || len(hits) != 6 {
		t.Fatalf("exit %d hits %v out=%s err=%s", c, hits, out.String(), err.String())
	}
	data, _ := os.ReadFile(logPath)
	if strings.Contains(string(data), token) || !strings.Contains(string(data), "pr_opened") {
		t.Fatalf("audit log: %s", data)
	}
}
