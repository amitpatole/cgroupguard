package audit

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCreateIsExclusivePrivateAndConfined(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.jsonl")
	l, closeLog, err := Create(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = l.Log("x", map[string]any{"n": 1})
	closeLog()
	if fi, err := os.Stat(path); err != nil || (runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600) {
		t.Fatalf("%v %v", fi, err)
	}
	if _, _, err := Create(path); err == nil {
		t.Fatal("an existing log was reused")
	}
	// A symlink at the final component must not be followed to another file.
	victim := filepath.Join(dir, "victim.txt")
	_ = os.WriteFile(victim, []byte("keep"), 0o600)
	link := filepath.Join(dir, "link.jsonl")
	_ = os.Symlink(victim, link)
	if _, _, err := Create(link); err == nil {
		t.Fatal("wrote through a symlink")
	}
	if b, _ := os.ReadFile(victim); string(b) != "keep" {
		t.Fatal("the symlink target was modified")
	}
	if _, _, err := Create(filepath.Join(dir, "missing", "a.jsonl")); err == nil {
		t.Fatal("created a log in a directory that does not exist")
	}
}
