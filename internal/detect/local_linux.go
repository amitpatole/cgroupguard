//go:build linux

package detect

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

const cgroup2Magic = 0x63677270
const cgroup1Magic = 0x27e0eb
const tmpfsMagic = 0x01021994

// Local inspects a cgroup root, including direct controller mounts. tmpfs alone
// is not proof of v1. Hybrid layouts must not be classified as unified v2.
func Local(path string) (string, string, error) {
	var root syscall.Statfs_t
	if err := syscall.Statfs(path, &root); err != nil {
		return "", "", err
	}
	if root.Type == cgroup2Magic {
		return "v2", "statfs: cgroup2fs at " + path, nil
	}
	if root.Type == cgroup1Magic {
		return "v1", "statfs: cgroup v1 filesystem at " + path, nil
	}
	if root.Type != tmpfsMagic {
		return "unknown", fmt.Sprintf("unrecognized filesystem magic %#x at %s", root.Type, path), nil
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return "", "", err
	}
	v1, v2 := false, false
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		var fs syscall.Statfs_t
		if err := syscall.Statfs(filepath.Join(path, entry.Name()), &fs); err != nil {
			return "", "", err
		}
		v1 = v1 || fs.Type == cgroup1Magic
		v2 = v2 || fs.Type == cgroup2Magic
	}
	return classify(v1, v2), fmt.Sprintf("tmpfs root; direct cgroup v1 mounts=%t; direct cgroup v2 mounts=%t", v1, v2), nil
}
