package forge

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/amitpatole/cgroupguard/internal/plan"
)

const tok = "ghp" + "_TESTTOKENTESTTOKENTESTTOKEN123456789"

func files() []plan.File {
	return []plan.File{{Path: "RUNBOOK.md", Content: "# runbook"}, {Path: "terraform/gke-p/main.tf", Content: "resource {}"}}
}

func TestGitHubFlowCreatesBranchCommitAndDraft(t *testing.T) {
	var calls []string
	var bodies = map[string]map[string]any{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+tok {
			t.Errorf("missing token on %s", r.URL.Path)
		}
		b, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		key := r.Method + " " + r.URL.Path
		calls = append(calls, key)
		bodies[key] = m
		switch key {
		case "GET /repos/acme/infra/git/ref/heads/main":
			io.WriteString(w, `{"object":{"sha":"basesha"}}`)
		case "GET /repos/acme/infra/git/commits/basesha":
			io.WriteString(w, `{"tree":{"sha":"basetree"}}`)
		case "POST /repos/acme/infra/git/trees":
			io.WriteString(w, `{"sha":"newtree"}`)
		case "POST /repos/acme/infra/git/commits":
			io.WriteString(w, `{"sha":"newcommit"}`)
		case "POST /repos/acme/infra/git/refs":
			w.WriteHeader(201)
			io.WriteString(w, `{}`)
		case "POST /repos/acme/infra/pulls":
			w.WriteHeader(201)
			io.WriteString(w, `{"html_url":"https://github.com/acme/infra/pull/7"}`)
		default:
			t.Errorf("unexpected call %s", key)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	u, err := Open(context.Background(), Spec{Kind: "github", BaseURL: srv.URL, Repo: "acme/infra", Files: files(), Title: "t", Body: "b"}, tok)
	if err != nil || u != "https://github.com/acme/infra/pull/7" {
		t.Fatalf("%v %q", err, u)
	}
	if len(calls) != 6 {
		t.Fatalf("calls: %v", calls)
	}
	if bodies["POST /repos/acme/infra/pulls"]["draft"] != true {
		t.Fatal("pull request is not a draft")
	}
	if m := bodies["POST /repos/acme/infra/git/commits"]["message"].(string); strings.Contains(strings.ToLower(m), "secur") || strings.Contains(strings.ToLower(m), "vulnerab") {
		t.Fatalf("commit message: %q", m)
	}
	entries := bodies["POST /repos/acme/infra/git/trees"]["tree"].([]any)
	for _, e := range entries {
		if !strings.HasPrefix(e.(map[string]any)["path"].(string), "cgroupguard-remediation/") {
			t.Fatalf("file outside the remediation directory: %v", e)
		}
	}
	if br := bodies["POST /repos/acme/infra/git/refs"]["ref"].(string); !strings.HasPrefix(br, "refs/heads/cgroupguard/cgroup-v2-plan-") {
		t.Fatalf("branch %q", br)
	}
}

func TestGitLabFlow(t *testing.T) {
	var calls []string
	var mrBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("PRIVATE-TOKEN") != tok {
			t.Errorf("missing token")
		}
		calls = append(calls, r.Method+" "+r.URL.EscapedPath())
		b, _ := io.ReadAll(r.Body)
		switch {
		case strings.HasSuffix(r.URL.Path, "/repository/commits"):
			w.WriteHeader(201)
			io.WriteString(w, `{}`)
		case strings.HasSuffix(r.URL.Path, "/merge_requests"):
			_ = json.Unmarshal(b, &mrBody)
			w.WriteHeader(201)
			io.WriteString(w, `{"web_url":"https://gitlab.example/g/p/-/merge_requests/3"}`)
		}
	}))
	defer srv.Close()
	u, err := Open(context.Background(), Spec{Kind: "gitlab", BaseURL: srv.URL, Repo: "group/sub/project", Files: files()}, tok)
	if err != nil || !strings.HasSuffix(u, "/merge_requests/3") {
		t.Fatalf("%v %q", err, u)
	}
	if calls[0] != "POST /api/v4/projects/group%2Fsub%2Fproject/repository/commits" {
		t.Fatalf("%v", calls)
	}
	if !strings.HasPrefix(mrBody["title"].(string), "Draft: ") {
		t.Fatalf("merge request is not a draft: %v", mrBody["title"])
	}
}

func TestExistingBranchAndErrorsNeverLeakTheToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			w.WriteHeader(422) // reference already exists
			io.WriteString(w, `{"message":"Reference already exists `+r.Header.Get("Authorization")+`"}`)
		}
	}))
	defer srv.Close()
	_, err := Open(context.Background(), Spec{Kind: "github", BaseURL: srv.URL, Repo: "acme/infra", Files: files()}, tok)
	if err == nil || strings.Contains(err.Error(), tok) || strings.Contains(err.Error(), "Reference already exists") {
		t.Fatalf("%v", err)
	}
	if _, err := Open(context.Background(), Spec{Kind: "github", BaseURL: srv.URL, Repo: "acme/infra", Files: files()}, ""); err == nil {
		t.Fatal("opened a request without a token")
	}
}

func TestSpecValidation(t *testing.T) {
	bad := []Spec{
		{Kind: "svn", Repo: "a/b", Files: files()},
		{Kind: "github", Repo: "a", Files: files()},
		{Kind: "github", Repo: "a/b/c", Files: files()},
		{Kind: "github", Repo: "a/../b", Files: files()},
		{Kind: "github", Repo: "a/b", Base: "x..y", Files: files()},
		{Kind: "github", Repo: "a/b", Branch: "main", Files: files()},
		{Kind: "github", Repo: "a/b", Branch: "bad branch", Files: files()},
		{Kind: "github", Repo: "a/b", BaseURL: "http://api.example.com", Files: files()},
		{Kind: "github", Repo: "a/b", BaseURL: "https://u:p@api.example.com", Files: files()},
		{Kind: "github", Repo: "a/b", BaseURL: "file:///x", Files: files()},
		{Kind: "github", Repo: "a/b"},
		{Kind: "github", Repo: "a/b", Files: []plan.File{{Path: "../x", Content: "x"}}},
		{Kind: "github", Repo: "a/b", Files: []plan.File{{Path: "/abs", Content: "x"}}},
		{Kind: "github", Repo: "a/b", Files: []plan.File{{Path: ".git/config", Content: "x"}}},
		{Kind: "github", Repo: "a/b", Files: []plan.File{{Path: "a", Content: "x"}, {Path: "./a", Content: "y"}}},
		{Kind: "github", Repo: "a/b", Files: []plan.File{{Path: "big", Content: strings.Repeat("x", maxBytes+1)}}},
	}
	for i, s := range bad {
		if err := s.Prepare(); err == nil {
			t.Errorf("case %d accepted: %+v", i, s)
		}
	}
	ok := Spec{Kind: "github", Repo: "a/b", Files: files()}
	if err := ok.Prepare(); err != nil || ok.Base != "main" || !strings.HasPrefix(ok.Branch, "cgroupguard/cgroup-v2-plan-") {
		t.Fatalf("%v %+v", err, ok)
	}
	// The branch name is derived from the content, so identical plans give identical names.
	again := Spec{Kind: "github", Repo: "a/b", Files: files()}
	_ = again.Prepare()
	if again.Branch != ok.Branch {
		t.Fatal("branch name is not deterministic")
	}
	if d := ok.Describe(); !strings.Contains(d, "DRAFT") || !strings.Contains(d, "cgroupguard-remediation/RUNBOOK.md") {
		t.Fatalf("%s", d)
	}
}

func TestRedirectsAreNotFollowed(t *testing.T) {
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("redirect followed with the token") }))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL, http.StatusTemporaryRedirect)
	}))
	defer srv.Close()
	if _, err := Open(context.Background(), Spec{Kind: "github", BaseURL: srv.URL, Repo: "a/b", Files: files()}, tok); err == nil {
		t.Fatal("redirect treated as success")
	}
}

func FuzzSpecPrepare(f *testing.F) {
	for _, s := range []string{"acme/infra", "a/../b", "a/b/c", "", "a b/c", "a/b\n", "../x"} {
		f.Add(s, "main", "cgroupguard/x", "dir/file.txt")
	}
	f.Fuzz(func(t *testing.T, repo, base, branch, path string) {
		s := Spec{Kind: "gitlab", Repo: repo, Base: base, Branch: branch, Files: []plan.File{{Path: path, Content: "x"}}}
		if err := s.Prepare(); err != nil {
			return
		}
		for _, f := range s.Files {
			if !strings.HasPrefix(f.Path, "cgroupguard-remediation/") || strings.Contains(f.Path, "..") || strings.Contains(f.Path, "\x00") || strings.HasPrefix(f.Path, "/") {
				t.Fatalf("unsafe path %q accepted for %q", f.Path, path)
			}
		}
		if strings.Contains(s.Repo, "..") || strings.ContainsAny(s.Repo, " \n\t") || strings.ContainsAny(s.Branch, " \n\t~^:?*[\\") {
			t.Fatalf("unsafe repo or branch accepted: %q %q", s.Repo, s.Branch)
		}
	})
}

func TestRepositoryPathsUseSlashSemantics(t *testing.T) {
	for _, p := range []string{"/abs", "//x", ".", "a/../../b", ".git/hooks/x", "a/./.git/../../.git/x"} {
		s := Spec{Kind: "github", Repo: "a/b", Files: []plan.File{{Path: p, Content: "x"}}}
		if err := s.Prepare(); err == nil {
			t.Errorf("accepted %q", p)
		}
	}
}
