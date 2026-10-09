// Package forge opens a draft pull request (GitHub) or merge request (GitLab) containing
// generated remediation files. It writes only to a new branch, never overwrites one,
// never merges, and keeps the access token out of every message.
package forge

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/amitpatole/cgroupguard/internal/plan"
)

type Spec struct {
	Kind          string // github | gitlab
	BaseURL       string // API root; defaults per kind
	Repo          string // owner/name (GitHub) or full project path (GitLab)
	Base          string // target branch
	Branch        string // new branch; derived from the content when empty
	Title, Body   string
	Files         []plan.File
	AllowInsecure bool
}

const (
	maxFiles = 300
	maxBytes = 5 << 20
	// The commit message is fixed and neutral on purpose.
	commitMessage = "Add cgroup v2 remediation plan and generated files"
)

var (
	repoRe   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*(/[A-Za-z0-9][A-Za-z0-9._-]*)+$`)
	branchRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,99}$`)
	// Only plain path characters: no control characters, spaces or shell/markup metacharacters.
	pathRe = regexp.MustCompile(`^[A-Za-z0-9._/-]{1,200}$`)
)

// Prepare validates the spec and fills in derived values. It performs no network access.
func (s *Spec) Prepare() error {
	if s.Kind != "github" && s.Kind != "gitlab" {
		return fmt.Errorf("--pr-forge must be github or gitlab")
	}
	if !repoRe.MatchString(s.Repo) || strings.Contains(s.Repo, "..") || (s.Kind == "github" && strings.Count(s.Repo, "/") != 1) {
		return fmt.Errorf("invalid repository %q", s.Repo)
	}
	if s.Base == "" {
		s.Base = "main"
	}
	if !branchRe.MatchString(s.Base) || strings.Contains(s.Base, "..") {
		return fmt.Errorf("invalid base branch")
	}
	if s.BaseURL == "" {
		s.BaseURL = map[string]string{"github": "https://api.github.com", "gitlab": "https://gitlab.com"}[s.Kind]
	}
	u, err := url.Parse(s.BaseURL)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return fmt.Errorf("invalid API base URL")
	}
	if u.Scheme == "http" && !s.AllowInsecure {
		if ip := net.ParseIP(u.Hostname()); u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return fmt.Errorf("refusing plain http to %s", u.Hostname())
		}
	}
	if len(s.Files) == 0 || len(s.Files) > maxFiles {
		return fmt.Errorf("a pull request needs between 1 and %d files", maxFiles)
	}
	total := 0
	seen := map[string]bool{}
	for i, f := range s.Files {
		// Repository paths are slash-separated on every platform, so they are checked with
		// package path, never with the host's filepath rules.
		clean := path.Clean(f.Path)
		if !pathRe.MatchString(f.Path) || strings.HasPrefix(f.Path, "/") || clean == "." || strings.Contains(clean, "..") || strings.HasPrefix(clean, ".git/") || clean == ".git" || seen[clean] {
			return fmt.Errorf("unsafe or duplicate file path %q", f.Path)
		}
		seen[clean] = true
		s.Files[i].Path = "cgroupguard-remediation/" + clean
		total += len(f.Content)
	}
	if total > maxBytes {
		return fmt.Errorf("files total %d bytes; the limit is %d", total, maxBytes)
	}
	sort.Slice(s.Files, func(i, j int) bool { return s.Files[i].Path < s.Files[j].Path })
	if s.Branch == "" {
		h := sha256.New()
		for _, f := range s.Files {
			h.Write([]byte(f.Path + "\x00" + f.Content + "\x00"))
		}
		s.Branch = "cgroupguard/cgroup-v2-plan-" + hex.EncodeToString(h.Sum(nil))[:10]
	}
	if !branchRe.MatchString(s.Branch) || strings.Contains(s.Branch, "..") || s.Branch == s.Base {
		return fmt.Errorf("invalid branch name")
	}
	if s.Title == "" {
		s.Title = "cgroup v2 remediation plan"
	}
	if len(s.Title) > 200 || len(s.Body) > 60000 {
		return fmt.Errorf("title or body too long")
	}
	return nil
}

// Describe lists what Open would do, for dry runs and confirmation prompts.
func (s Spec) Describe() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s repository %s: create branch %s from %s, add %d file(s), open a DRAFT request titled %q\n", s.Kind, s.Repo, s.Branch, s.Base, len(s.Files), s.Title)
	for _, f := range s.Files {
		fmt.Fprintf(&b, "  + %s (%d bytes)\n", f.Path, len(f.Content))
	}
	return b.String()
}

type client struct {
	c     *http.Client
	base  string
	token string
	kind  string
}

func (c *client) do(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.base, "/")+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "cgroupguard")
	if c.kind == "github" {
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	} else {
		req.Header.Set("PRIVATE-TOKEN", c.token)
	}
	resp, err := c.c.Do(req)
	if err != nil {
		var ue *url.Error
		if e, ok := err.(*url.Error); ok {
			ue = e
		}
		if ue != nil {
			return fmt.Errorf("request failed: %v", ue.Err)
		}
		return fmt.Errorf("request failed")
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode/100 != 2 {
		// Response bodies can echo request fragments; only the status is reported.
		return fmt.Errorf("%s %s: HTTP %d", method, redactPath(path), resp.StatusCode)
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("unreadable response")
		}
	}
	return nil
}

func redactPath(p string) string {
	if i := strings.IndexByte(p, '?'); i >= 0 {
		return p[:i]
	}
	return p
}

// Open creates the branch, commit and draft request, and returns the request's URL.
func Open(ctx context.Context, s Spec, token string) (string, error) {
	if token == "" {
		return "", fmt.Errorf("the access token environment variable is empty")
	}
	if err := s.Prepare(); err != nil {
		return "", err
	}
	c := &client{c: &http.Client{Timeout: 60 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, base: s.BaseURL, token: token, kind: s.Kind}
	if s.Kind == "github" {
		return openGitHub(ctx, c, s)
	}
	return openGitLab(ctx, c, s)
}

func openGitHub(ctx context.Context, c *client, s Spec) (string, error) {
	r := "/repos/" + s.Repo
	var ref struct {
		Object struct{ SHA string } `json:"object"`
	}
	if err := c.do(ctx, http.MethodGet, r+"/git/ref/heads/"+url.PathEscape(s.Base), nil, &ref); err != nil {
		return "", err
	}
	var commit struct {
		Tree struct{ SHA string } `json:"tree"`
	}
	if err := c.do(ctx, http.MethodGet, r+"/git/commits/"+ref.Object.SHA, nil, &commit); err != nil {
		return "", err
	}
	var entries []map[string]string
	for _, f := range s.Files {
		entries = append(entries, map[string]string{"path": f.Path, "mode": "100644", "type": "blob", "content": f.Content})
	}
	var tree struct{ SHA string }
	if err := c.do(ctx, http.MethodPost, r+"/git/trees", map[string]any{"base_tree": commit.Tree.SHA, "tree": entries}, &tree); err != nil {
		return "", err
	}
	var nc struct{ SHA string }
	if err := c.do(ctx, http.MethodPost, r+"/git/commits", map[string]any{"message": commitMessage, "tree": tree.SHA, "parents": []string{ref.Object.SHA}}, &nc); err != nil {
		return "", err
	}
	// Creating a ref fails if it exists, so an existing branch is never overwritten.
	if err := c.do(ctx, http.MethodPost, r+"/git/refs", map[string]string{"ref": "refs/heads/" + s.Branch, "sha": nc.SHA}, nil); err != nil {
		return "", err
	}
	var pr struct {
		URL string `json:"html_url"`
	}
	if err := c.do(ctx, http.MethodPost, r+"/pulls", map[string]any{"title": s.Title, "head": s.Branch, "base": s.Base, "body": s.Body, "draft": true}, &pr); err != nil {
		return "", fmt.Errorf("the branch %s was created but opening the pull request failed: %w", s.Branch, err)
	}
	return pr.URL, nil
}

func openGitLab(ctx context.Context, c *client, s Spec) (string, error) {
	proj := "/api/v4/projects/" + url.PathEscape(s.Repo)
	var actions []map[string]string
	for _, f := range s.Files {
		actions = append(actions, map[string]string{"action": "create", "file_path": f.Path, "content": f.Content})
	}
	if err := c.do(ctx, http.MethodPost, proj+"/repository/commits", map[string]any{"branch": s.Branch, "start_branch": s.Base, "commit_message": commitMessage, "actions": actions}, nil); err != nil {
		return "", err
	}
	var mr struct {
		URL string `json:"web_url"`
	}
	if err := c.do(ctx, http.MethodPost, proj+"/merge_requests", map[string]any{"source_branch": s.Branch, "target_branch": s.Base, "title": "Draft: " + s.Title, "description": s.Body}, &mr); err != nil {
		return "", fmt.Errorf("the branch %s was created but opening the merge request failed: %w", s.Branch, err)
	}
	return mr.URL, nil
}
