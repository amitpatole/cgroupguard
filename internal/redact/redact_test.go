package redact

import (
	"strings"
	"testing"
)

// Secret-shaped test values are assembled at run time so no secret scanner flags this source.
const (
	awsKey   = "AKIA" + "IOSFODNN7EXAMPLE"
	ghTok    = "ghp" + "_abcdefghijklmnopqrstuvwxyz0123456789"
	slackTok = "xox" + "b-123456789012-abcdefghij"
	gKey     = "AIza" + "SyA-abcdefghijklmnopqrstuvwxyz012345"
	pemOpen  = "-----BEGIN RSA " + "PRIVATE KEY-----"
	pemClose = "-----END RSA " + "PRIVATE KEY-----"
)

func TestSecretsAreRemoved(t *testing.T) {
	cases := map[string]string{
		"registry login https://user:hunter2@registry.example/v2":                  "hunter2",
		"key " + awsKey + " here":                                                  awsKey,
		"token " + ghTok:                                                           ghTok[:10],
		"Authorization: Bearer abcdefghijklmnop.qrstuvwxyz0123":                    "abcdefghijklmnop",
		"password=SuperSecret99!":                                                  "SuperSecret99",
		`api_key: "abc123def456"`:                                                  "abc123def456",
		"jwt eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dBjftJeZ4CVPmB92K27": "eyJhbGciOiJIUzI1NiJ9",
		"sk-abcdefghijklmnopqrstuvwxyz":                                            "sk-abcdefghij",
		pemOpen + "\nMIIEow\n" + pemClose:                                          "MIIEow",
		"blob QUJDREVGR0hJSktMTU5PUFFSU1RVVldYWVowMTIzNDU2Nzg5YWJjZGVm":            "QUJDREVGR0hJ",
		slackTok: slackTok[:9],
		gKey:     gKey[:11],
	}
	for in, secret := range cases {
		r := New(false)
		out := r.Text(in)
		if strings.Contains(out, secret) {
			t.Errorf("secret %q survived: %q", secret, out)
		}
		if !strings.Contains(out, "REDACTED") {
			t.Errorf("no redaction marker for %q: %q", in, out)
		}
		if len(r.Counts) == 0 {
			t.Errorf("no counts for %q", in)
		}
	}
}

func TestPrefixedSecretsSurviveNoAdjacentCharacter(t *testing.T) {
	for _, in := range []string{"x_" + awsKey, awsKey + "_", "_sk" + "-abcdefghijklmnopqrstuvwxyz", "sk" + "-abcdefghijklmnopqrstuvwxyz_x", "y_" + slackTok} {
		r := New(false)
		out := r.Text(in)
		for _, secret := range []string{awsKey, "sk" + "-abcdefghij", slackTok[:9]} {
			if strings.Contains(out, secret) {
				t.Errorf("%q leaked %q: %q", in, secret, out)
			}
		}
	}
	// Ordinary words that merely contain a prefix are not redacted.
	if got := New(false).Text("risk-assessment-for-clusters and task-runner-service-name"); strings.Contains(got, "REDACTED") {
		t.Errorf("over-redacted ordinary words: %q", got)
	}
}

func TestOrdinaryTextIsKept(t *testing.T) {
	r := New(false)
	for _, s := range []string{"openjdk:11.0.15 on a node with cgroup v1", "eclipse-temurin:17.0.10-jre", "CG101 warning", "Kubernetes 1.35 kubelet"} {
		if got := r.Text(s); got != s {
			t.Errorf("altered %q -> %q", s, got)
		}
	}
}

func TestControlCharactersAreStripped(t *testing.T) {
	r := New(false)
	if out := r.Text("a\x1b[31mb\x00c\nd"); strings.ContainsAny(out, "\x1b\x00") || !strings.Contains(out, "\n") {
		t.Fatalf("%q", out)
	}
}

func TestPseudonymsAreStableAndReversible(t *testing.T) {
	r := New(false)
	r.Prime("node", []string{"worker-10", "worker-2", "worker-1"})
	a, b := r.Name("node", "worker-1"), r.Name("node", "worker-2")
	if a == b || a != r.Name("node", "worker-1") {
		t.Fatalf("unstable pseudonyms %s %s", a, b)
	}
	if strings.Contains(a, "worker") {
		t.Fatalf("real name leaked in pseudonym %q", a)
	}
	// Unmap must prefer longer pseudonyms (node-10 before node-1).
	msg := "check " + r.Name("node", "worker-10") + " and " + a
	if got := r.Unmap(msg); got != "check worker-10 and worker-1" {
		t.Fatalf("%q", got)
	}
	if rv := New(true); rv.Name("node", "worker-1") != "worker-1" {
		t.Fatal("reveal mode should keep names")
	}
	if r.Name("node", "") != "" {
		t.Fatal("empty name must stay empty")
	}
}

func FuzzTextNeverLeaksPlantedSecrets(f *testing.F) {
	for _, s := range []string{"x", "password=abc", awsKey, "a b c"} {
		f.Add(s)
	}
	const planted = "ghp" + "_PLANTEDSECRETPLANTEDSECRET123456"
	f.Fuzz(func(t *testing.T, in string) {
		r := New(false)
		for _, wrap := range []string{in + " " + planted, planted + in, in + "=" + planted, "token: " + planted + in, "x_" + planted, planted + "_", "a-" + planted + "9", "(" + planted + ")"} {
			if strings.Contains(r.Text(wrap), planted) {
				t.Fatalf("planted secret survived in %q", wrap)
			}
		}
	})
}

func TestMaskReplacesWholeTokensOnly(t *testing.T) {
	r := New(false)
	r.Prime("node", []string{"worker-1", "worker-10", "app"})
	out := r.Mask("worker-1 and worker-10 and worker-100 and app and apple, worker-1.example")
	if strings.Contains(out, "worker-1 ") || !strings.Contains(out, "worker-100") || !strings.Contains(out, "apple") {
		t.Fatalf("over- or under-masked: %q", out)
	}
	if strings.Contains(out, "worker-10 ") {
		t.Fatalf("worker-10 not masked: %q", out)
	}
	if !strings.Contains(out, " app ") {
		// names shorter than 4 characters are intentionally left alone
		t.Fatalf("short name should be untouched: %q", out)
	}
	if New(true).Mask("worker-1") != "worker-1" {
		t.Fatal("reveal mode must not mask")
	}
}
