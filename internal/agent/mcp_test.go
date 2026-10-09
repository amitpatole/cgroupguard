package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func rpc(t *testing.T, env *Env, lines ...string) []map[string]any {
	t.Helper()
	var out, log bytes.Buffer
	if err := ServeMCP(context.Background(), strings.NewReader(strings.Join(lines, "\n")+"\n"), &out, env, Policy{}, NewAudit(&log, nil), 0); err != nil {
		t.Fatal(err)
	}
	if n, err := VerifyLog(&log); err != nil || n == 0 {
		t.Fatalf("mcp audit: %d %v", n, err)
	}
	var res []map[string]any
	dec := json.NewDecoder(&out)
	for dec.More() {
		var m map[string]any
		if err := dec.Decode(&m); err != nil {
			t.Fatal(err)
		}
		res = append(res, m)
	}
	return res
}

func TestMCPProtocol(t *testing.T) {
	env := testEnv(t, false)
	res := rpc(t, env,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_nodes","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"open_pr","arguments":{"title":"x"}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"nope"}`,
		`not json`,
		`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{}}`)
	if len(res) != 7 { // the notification gets no reply
		t.Fatalf("got %d responses: %v", len(res), res)
	}
	if res[0]["result"].(map[string]any)["serverInfo"].(map[string]any)["name"] != "cgroupguard" {
		t.Fatalf("%v", res[0])
	}
	tools := res[1]["result"].(map[string]any)["tools"].([]any)
	names := map[string]bool{}
	for _, tl := range tools {
		names[tl.(map[string]any)["name"].(string)] = true
	}
	for _, want := range []string{"list_nodes", "probe_node", "scan_workloads", "inspect_image", "lookup_docs", "generate_patch", "open_pr"} {
		if !names[want] {
			t.Errorf("tool %s not listed", want)
		}
	}
	text := res[2]["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	if strings.Contains(text, realNode) || !strings.Contains(text, "node-1") {
		t.Fatalf("MCP result is not pseudonymized: %s", text)
	}
	if res[3]["result"].(map[string]any)["isError"] != true {
		t.Fatalf("open_pr was not refused: %v", res[3])
	}
	if res[4]["error"].(map[string]any)["code"].(float64) != -32601 || res[5]["error"].(map[string]any)["code"].(float64) != -32700 || res[6]["error"].(map[string]any)["code"].(float64) != -32602 {
		t.Fatalf("error codes: %v %v %v", res[4], res[5], res[6])
	}
}

func FuzzMCPInput(f *testing.F) {
	for _, s := range []string{`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, `{`, `[]`, `{"jsonrpc":"2.0","id":"x","method":"tools/call","params":{"name":"scan_workloads","arguments":{"severity":"high"}}}`, strings.Repeat("a", 5000)} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		var out, log bytes.Buffer
		env := testEnvQuiet()
		_ = ServeMCP(context.Background(), strings.NewReader(in+"\n"), &out, env, Policy{}, NewAudit(&log, nil), 0)
		if strings.Contains(out.String(), realNode) || strings.Contains(out.String(), secretKey) {
			t.Fatalf("leak through MCP for input %q", in)
		}
	})
}
