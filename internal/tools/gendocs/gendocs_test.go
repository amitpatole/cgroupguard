package gendocs

import (
	"os"
	"path/filepath"
	"testing"
)

// The committed reference in docs/cli must equal what the program generates.
// Regenerate with: UPDATE_DOCS=1 go test ./internal/tools/gendocs
func TestCLIReferenceIsCurrent(t *testing.T) {
	want := filepath.Join("..", "..", "..", "docs", "cli")
	if os.Getenv("UPDATE_DOCS") == "1" {
		if err := os.MkdirAll(want, 0o755); err != nil {
			t.Fatal(err)
		}
		old, _ := filepath.Glob(filepath.Join(want, "*.md"))
		for _, f := range old {
			_ = os.Remove(f)
		}
		if err := Generate(want); err != nil {
			t.Fatal(err)
		}
		return
	}
	got := t.TempDir()
	if err := Generate(got); err != nil {
		t.Fatal(err)
	}
	gotFiles, _ := filepath.Glob(filepath.Join(got, "*.md"))
	wantFiles, _ := filepath.Glob(filepath.Join(want, "*.md"))
	if len(gotFiles) != len(wantFiles) || len(gotFiles) == 0 {
		t.Fatalf("command reference is out of date: %d generated files, %d committed (run UPDATE_DOCS=1 go test ./internal/tools/gendocs)", len(gotFiles), len(wantFiles))
	}
	for _, g := range gotFiles {
		a, _ := os.ReadFile(g)
		b, err := os.ReadFile(filepath.Join(want, filepath.Base(g)))
		if err != nil || string(a) != string(b) {
			t.Fatalf("%s is out of date (run UPDATE_DOCS=1 go test ./internal/tools/gendocs)", filepath.Base(g))
		}
	}
}
