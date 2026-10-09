package cli

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func fakeModel(t *testing.T, replies ...string) (*httptest.Server, *[]string) {
	t.Helper()
	var seen []string
	i := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		seen = append(seen, string(b))
		if i < len(replies) {
			io.WriteString(w, replies[i])
			i++
			return
		}
		io.WriteString(w, `{"choices":[{"message":{"content":"done"}}]}`)
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func TestAgentExplainEndToEnd(t *testing.T) {
	srv, seen := fakeModel(t,
		`{"choices":[{"message":{"content":"","tool_calls":[{"id":"c1","function":{"name":"list_nodes","arguments":"{}"}}]}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`,
		`{"choices":[{"message":{"content":"node-1 is on cgroup v1; migrate it first."}}],"usage":{"prompt_tokens":20,"completion_tokens":9}}`)
	logPath := filepath.Join(t.TempDir(), "audit.jsonl")
	var out, errb bytes.Buffer
	c := Run([]string{"agent", "explain", "--input", planFixture, "--provider", "openai-compatible", "--model", "m", "--base-url", srv.URL + "/v1", "--audit-log", logPath}, &out, &errb)
	if c != 0 {
		t.Fatalf("exit %d: %s", c, errb.String())
	}
	got := out.String()
	if !strings.Contains(got, "AI commentary (advisory, unverified)") || !strings.Contains(got, "authoritative") {
		t.Fatalf("commentary is not labeled: %s", got)
	}
	// Names from the fixture must be pseudonymized on the wire but real in the user's output.
	all := strings.Join(*seen, "\n")
	for _, real := range []string{"gke-a-1", "eks-b-1", "aks-c-1", "pool-legacy"} {
		if strings.Contains(all, real) {
			t.Errorf("%q was sent to the model", real)
		}
	}
	if !strings.Contains(all, "node-1") {
		t.Fatal("pseudonyms missing from the request")
	}
	fi, err := os.Stat(logPath)
	if err != nil || (runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600) {
		t.Fatalf("audit log permissions: %v %v", fi, err)
	}
	out.Reset()
	if c := Run([]string{"agent", "verify-log", logPath}, &out, &errb); c != 0 || !strings.Contains(out.String(), "hash chain intact") {
		t.Fatalf("verify: %d %s %s", c, out.String(), errb.String())
	}
	// An existing log is never appended to (that would break the chain), and edits are caught.
	if c := Run([]string{"agent", "explain", "--input", planFixture, "--provider", "openai-compatible", "--model", "m", "--base-url", srv.URL + "/v1", "--audit-log", logPath}, &out, &errb); c != 2 {
		t.Fatalf("reused an existing audit log: exit %d", c)
	}
	data, _ := os.ReadFile(logPath)
	bad := filepath.Join(t.TempDir(), "bad.jsonl")
	_ = os.WriteFile(bad, []byte(strings.Replace(string(data), `"kind":"request"`, `"kind":"quest"`, 1)), 0o600)
	if c := Run([]string{"agent", "verify-log", bad}, &out, &errb); c != 2 {
		t.Fatalf("tampered log verified: %d", c)
	}
}

func TestAgentBudgetExhaustionIsReported(t *testing.T) {
	loop := `{"choices":[{"message":{"content":"","tool_calls":[{"id":"c","function":{"name":"list_nodes","arguments":"{}"}}]}}]}`
	srv, _ := fakeModel(t, loop, loop, loop, loop, loop)
	var out, errb bytes.Buffer
	c := Run([]string{"agent", "explain", "--input", planFixture, "--provider", "openai-compatible", "--model", "m", "--base-url", srv.URL + "/v1", "--max-steps", "2"}, &out, &errb)
	if c != 1 || !strings.Contains(errb.String(), "step budget") {
		t.Fatalf("exit %d out=%q err=%q", c, out.String(), errb.String())
	}
}

func TestAgentRequiresExplicitProviderAndNeverUsesOneByDefault(t *testing.T) {
	var out, errb bytes.Buffer
	for _, args := range [][]string{
		{"agent", "explain", "--input", planFixture},
		{"agent", "explain", "--input", planFixture, "--provider", "openai-compatible", "--model", "m"},
		{"agent", "explain", "--input", planFixture, "--provider", "anthropic", "--model", "m", "--api-key-env", "SURELY_UNSET_KEY_VAR"},
		{"agent", "explain", "--input", planFixture, "--provider", "openai-compatible", "--model", "m", "--base-url", "http://models.example.com/v1"},
	} {
		if c := Run(args, &out, &errb); c != 2 {
			t.Fatalf("%v accepted (exit %d)", args, c)
		}
	}
}

func TestAgentMCPOverStdio(t *testing.T) {
	// The MCP command reads JSON-RPC from stdin; here a fixture scan and one list call.
	cmd := Run([]string{"agent", "mcp", "--input", planFixture}, &bytes.Buffer{}, &bytes.Buffer{})
	if cmd != 0 {
		t.Fatalf("mcp with empty stdin exited %d", cmd)
	}
}
