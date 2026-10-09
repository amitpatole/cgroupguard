//go:build !linux

package detect

import "fmt"

func Local(path string) (string, string, error) {
	return "", "", fmt.Errorf("local cgroup inspection requires Linux; use check --input or a Kubernetes context")
}
