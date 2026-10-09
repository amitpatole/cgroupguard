// Package validate runs external linters over generated infrastructure-as-code in a
// temporary directory. It never applies, plans against real infrastructure, or
// contacts a cluster; terraform init only downloads provider plugins.
package validate

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/amitpatole/cgroupguard/internal/plan"
)

// Result is one tool run. Skipped means the tool is not installed, which callers
// must treat as "not validated", never as a pass.
type Result struct {
	Tool    string
	Target  string
	OK      bool
	Skipped bool
	Output  string
}

func (r Result) String() string {
	state := "ok"
	switch {
	case r.Skipped:
		state = "SKIPPED (not installed)"
	case !r.OK:
		state = "FAILED"
	}
	return fmt.Sprintf("%s %s: %s", r.Tool, r.Target, state)
}

// Failed reports whether any tool failed or was missing.
func Failed(rs []Result) bool {
	for _, r := range rs {
		if !r.OK {
			return true
		}
	}
	return false
}

func write(dir string, files []plan.File) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, f := range files {
		clean := filepath.Clean(f.Path)
		if filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
			return fmt.Errorf("unsafe path %q", f.Path)
		}
		if d := filepath.Dir(clean); d != "." {
			if err := root.MkdirAll(d, 0o750); err != nil {
				return err
			}
		}
		if err := root.WriteFile(clean, []byte(f.Content), 0o640); err != nil {
			return err
		}
	}
	return nil
}

// Closed set of external commands. Every command name and argument is a literal
// below, so no cluster- or user-supplied text can reach a subprocess.
const (
	cmdTfFmt = iota
	cmdTfInit
	cmdTfValidate
	cmdTfTest
	cmdTflint
	cmdTrivyConfig
	cmdYamllint
	cmdGalaxy
	cmdSyntaxMigrate
	cmdSyntaxRollback
	cmdAnsibleLint
)

func command(ctx context.Context, id int) *exec.Cmd {
	switch id {
	case cmdTfFmt:
		return exec.CommandContext(ctx, "terraform", "fmt", "-check", "-diff", "-recursive")
	case cmdTfInit:
		return exec.CommandContext(ctx, "terraform", "init", "-backend=false", "-input=false", "-no-color")
	case cmdTfValidate:
		return exec.CommandContext(ctx, "terraform", "validate", "-no-color")
	case cmdTfTest:
		return exec.CommandContext(ctx, "terraform", "test", "-no-color")
	case cmdTflint:
		return exec.CommandContext(ctx, "tflint")
	case cmdTrivyConfig:
		return exec.CommandContext(ctx, "trivy", "config", "--quiet", "--severity", "CRITICAL,HIGH,MEDIUM", "--exit-code", "1", ".")
	case cmdYamllint:
		return exec.CommandContext(ctx, "yamllint", "-c", ".yamllint", ".")
	case cmdGalaxy:
		return exec.CommandContext(ctx, "ansible-galaxy", "collection", "install", "-r", "requirements.yml", "-p", ".ansible/collections", "--force-with-deps")
	case cmdSyntaxMigrate:
		return exec.CommandContext(ctx, "ansible-playbook", "--syntax-check", "-i", "inventory.yml", "cgroupv2-migrate.yml")
	case cmdSyntaxRollback:
		return exec.CommandContext(ctx, "ansible-playbook", "--syntax-check", "-i", "inventory.yml", "cgroupv2-rollback.yml")
	case cmdAnsibleLint:
		return exec.CommandContext(ctx, "ansible-lint", "--offline", "-c", ".ansible-lint", "cgroupv2-migrate.yml", "cgroupv2-rollback.yml", "roles")
	}
	return nil
}

func run(ctx context.Context, dir string, env []string, id int) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	cmd := command(ctx, id)
	if cmd == nil {
		return "", fmt.Errorf("unknown validator command %d", id)
	}
	cmd.Dir = dir
	cmd.Env = append([]string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"), "TERM=dumb", "NO_COLOR=1"}, env...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	return out.String(), err
}

func lookup(name string) (string, bool) {
	p, err := exec.LookPath(name)
	return p, err == nil
}

// Files validates every generated Terraform module and Ansible project.
func Files(ctx context.Context, files []plan.File) []Result {
	tmp, err := os.MkdirTemp("", "cgroupguard-validate-")
	if err != nil {
		return []Result{{Tool: "setup", Target: "-", Output: err.Error()}}
	}
	defer os.RemoveAll(tmp)
	if err := write(tmp, files); err != nil {
		return []Result{{Tool: "setup", Target: "-", Output: err.Error()}}
	}
	var rs []Result
	modules, ansible := map[string]bool{}, false
	for _, f := range files {
		parts := strings.Split(f.Path, "/")
		if len(parts) >= 3 && parts[0] == "terraform" && strings.HasSuffix(f.Path, ".tf") && parts[2] == "main.tf" {
			modules[parts[0]+"/"+parts[1]] = true
		}
		if parts[0] == "ansible" {
			ansible = true
		}
	}
	var dirs []string
	for m := range modules {
		dirs = append(dirs, m)
	}
	sort.Strings(dirs)
	for _, m := range dirs {
		rs = append(rs, terraform(ctx, tmp, m)...)
	}
	if ansible {
		rs = append(rs, ansibleChecks(ctx, tmp)...)
	}
	return rs
}

func terraform(ctx context.Context, root, module string) []Result {
	if _, ok := lookup("terraform"); !ok {
		return []Result{{Tool: "terraform", Target: module, Skipped: true}}
	}
	dir := filepath.Join(root, module)
	env := []string{"TF_IN_AUTOMATION=1", "TF_INPUT=0", "CHECKPOINT_DISABLE=1"}
	// Provider plugins are cached in the user cache directory so repeated runs do not
	// download them again. The location is fixed; it is not taken from the environment.
	if uc, err := os.UserCacheDir(); err == nil {
		cache := filepath.Join(uc, "cgroupguard", "terraform-plugins")
		if os.MkdirAll(cache, 0o750) == nil {
			env = append(env, "TF_PLUGIN_CACHE_DIR="+cache)
		}
	}
	var rs []Result
	step := func(tool string, id int) bool {
		out, err := run(ctx, dir, env, id)
		rs = append(rs, Result{Tool: tool, Target: module, OK: err == nil, Output: out})
		return err == nil
	}
	step("terraform fmt", cmdTfFmt)
	if step("terraform init", cmdTfInit) {
		if step("terraform validate", cmdTfValidate) {
			step("terraform test", cmdTfTest)
		}
	}
	optional := func(tool, bin string, id int) {
		if _, ok := lookup(bin); !ok {
			rs = append(rs, Result{Tool: tool, Target: module, Skipped: true})
			return
		}
		step(tool, id)
	}
	optional("tflint", "tflint", cmdTflint)
	optional("trivy config", "trivy", cmdTrivyConfig)
	return rs
}

func ansibleChecks(ctx context.Context, root string) []Result {
	dir := filepath.Join(root, "ansible")
	env := []string{"ANSIBLE_COLLECTIONS_PATH=" + filepath.Join(dir, ".ansible", "collections"), "ANSIBLE_NOCOLOR=1", "ANSIBLE_CONFIG=" + filepath.Join(dir, "ansible.cfg")}
	var rs []Result
	one := func(tool string, id int) bool {
		if _, ok := lookup(tool); !ok {
			rs = append(rs, Result{Tool: tool, Target: "ansible", Skipped: true})
			return false
		}
		out, err := run(ctx, dir, env, id)
		rs = append(rs, Result{Tool: tool, Target: "ansible", OK: err == nil, Output: out})
		return err == nil
	}
	one("yamllint", cmdYamllint)
	if one("ansible-galaxy", cmdGalaxy) {
		one("ansible-playbook", cmdSyntaxMigrate)
		one("ansible-playbook", cmdSyntaxRollback)
	}
	one("ansible-lint", cmdAnsibleLint)
	return rs
}
