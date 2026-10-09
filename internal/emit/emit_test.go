package emit_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/amitpatole/cgroupguard/internal/emit"
	"github.com/amitpatole/cgroupguard/internal/model"
	"github.com/amitpatole/cgroupguard/internal/plan"
	"github.com/amitpatole/cgroupguard/internal/rules"
	"github.com/amitpatole/cgroupguard/internal/validate"
)

func fixturePlan(t *testing.T) plan.Plan {
	t.Helper()
	data, e := os.ReadFile("../../examples/plan-mixed.json")
	if e != nil {
		t.Fatal(e)
	}
	var s model.Snapshot
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if e = d.Decode(&s); e != nil {
		t.Fatal(e)
	}
	set, _ := rules.Load()
	r, e := rules.EvaluateFor(set, s.Nodes, "1.35")
	if e != nil {
		t.Fatal(e)
	}
	if e = rules.AddWorkloads(&r, s.Workloads); e != nil {
		t.Fatal(e)
	}
	p, e := plan.Build(r, plan.Options{})
	if e != nil {
		t.Fatal(e)
	}
	return p
}

func TestGenerateDeterministicAndGolden(t *testing.T) {
	p := fixturePlan(t)
	a, e := emit.Generate(p, []string{"terraform", "ansible"})
	if e != nil {
		t.Fatal(e)
	}
	b, _ := emit.Generate(p, []string{"ansible", "terraform"})
	if len(a) != len(b) {
		t.Fatal("not deterministic")
	}
	var all strings.Builder
	for i, f := range a {
		if f.Path != b[i].Path || f.Content != b[i].Content {
			t.Fatalf("file %s differs between runs", f.Path)
		}
		all.WriteString("=== " + f.Path + "\n" + f.Content)
	}
	path := "testdata/generated.golden"
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if e := os.WriteFile(path, []byte(all.String()), 0o644); e != nil {
			t.Fatal(e)
		}
	}
	want, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	if string(want) != all.String() {
		t.Fatal("generated files differ from reviewed golden output")
	}
}

func TestGeneratedContentPolicy(t *testing.T) {
	files, _ := emit.Generate(fixturePlan(t), []string{"terraform", "ansible"})
	text := ""
	for _, f := range files {
		text += f.Path + "\n" + f.Content + "\n"
	}
	for _, must := range []string{"CGROUP_MODE_V2", "AL2023_x86_64_STANDARD", "Ubuntu2204", "create_before_destroy = true", "serial: 1", "any_errors_fatal: true", "disable_eviction: false", "no_log:", "kubernetes.core", "cgroup2fs"} {
		if !strings.Contains(text, must) {
			t.Errorf("generated output lacks %q", must)
		}
	}
	for _, bad := range []string{"password", "secret_key", "access_key", "client_secret", "BEGIN ", "force: true", "--force", "disable_eviction: true", "tfstate"} {
		if strings.Contains(strings.ToLower(text), strings.ToLower(bad)) {
			t.Errorf("generated output contains %q", bad)
		}
	}
}

func TestRejectsUnknownKindAndHostileNames(t *testing.T) {
	p := fixturePlan(t)
	if _, e := emit.Generate(p, []string{"puppet"}); e == nil {
		t.Fatal("accepted unknown kind")
	}
	p.Groups[0].Nodes = append(p.Groups[0].Nodes, "x; rm -rf /")
	if _, e := emit.Generate(p, []string{"terraform", "ansible"}); e == nil {
		t.Fatal("accepted hostile node name")
	}
}

// Runs the real linters when CGROUPGUARD_VALIDATE=1 (needs terraform, yamllint, ansible, ansible-lint).
func TestValidatorsOnGeneratedFiles(t *testing.T) {
	if os.Getenv("CGROUPGUARD_VALIDATE") != "1" {
		t.Skip("set CGROUPGUARD_VALIDATE=1 to run terraform/ansible validators")
	}
	files, e := emit.Generate(fixturePlan(t), []string{"terraform", "ansible"})
	if e != nil {
		t.Fatal(e)
	}
	rs := validate.Files(context.Background(), files)
	for _, r := range rs {
		t.Log(r.String())
		if !r.OK && !r.Skipped {
			t.Errorf("%s\n%s", r, r.Output)
		}
	}
}

func FuzzGroupNamesNeverReachOutput(f *testing.F) {
	for _, s := range []string{"pool-1", "p; $(id)", "a\"b", "${x}", "%{x}", "..", "a/b", "é"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, name string) {
		g := plan.Group{Provider: "gke", Pool: name, Nodes: []string{"n1"}}
		files, e := emit.Generate(plan.Plan{SchemaVersion: plan.SchemaVersion, Groups: []plan.Group{g}}, []string{"terraform", "ansible"})
		if e != nil {
			return // rejected is fine
		}
		for _, file := range files {
			if strings.Contains(file.Path, "..") || strings.HasPrefix(file.Path, "/") {
				t.Fatalf("unsafe path %q for pool %q", file.Path, name)
			}
			if name != "" && !plan.SafeName(name) && strings.Contains(file.Content, name) {
				t.Fatalf("unvalidated pool name %q reached %s", name, file.Path)
			}
		}
	})
}

// Regression tests for defects found only by running the generated role on a real
// cgroup v1 node. Each assertion corresponds to a failure that was observed.
func generated(t *testing.T) map[string]string {
	t.Helper()
	files, e := emit.Generate(fixturePlan(t), []string{"ansible"})
	if e != nil {
		t.Fatal(e)
	}
	m := map[string]string{}
	for _, f := range files {
		m[f.Path] = f.Content
	}
	return m
}

func TestRoleIsNotTaggedWithEveryTag(t *testing.T) {
	// Tagging the role entry with preflight, migrate and verify made `--tags preflight`
	// run the whole migration on a real node.
	pb := generated(t)["ansible/cgroupv2-migrate.yml"]
	if strings.Contains(pb, "tags: [preflight, migrate, verify]") {
		t.Fatal("role entry is tagged with every tag")
	}
	main := generated(t)["ansible/roles/cgroupv2/tasks/main.yml"]
	for _, tag := range []string{"[preflight]", "[migrate]", "[verify]"} {
		if !strings.Contains(main, "tags: "+tag) {
			t.Fatalf("role main.yml lacks tags: %s", tag)
		}
	}
	pre := generated(t)["ansible/roles/cgroupv2/tasks/preflight.yml"]
	if strings.Count(pre, "tags: [always]") != 2 {
		t.Fatal("detection tasks must run under any tag selection")
	}
}

func TestGrubDropinCheckFailsClosed(t *testing.T) {
	pre := generated(t)["ansible/roles/cgroupv2/tasks/preflight.yml"]
	// find applies `contains` as an anchored match per line, and use_regex turns the glob into an invalid regex.
	if strings.Contains(pre, "use_regex") {
		t.Fatal("use_regex breaks the glob pattern and silently skips the directory")
	}
	if !strings.Contains(pre, "contains: '.*systemd") {
		t.Fatal("contains pattern must be anchored-match safe (leading .*)")
	}
	if !strings.Contains(pre, "skipped_paths | default({}) | length == 0") {
		t.Fatal("a skipped path must fail the check")
	}
}

func TestMigrateEditsBeforeDisruption(t *testing.T) {
	m := generated(t)["ansible/roles/cgroupv2/tasks/migrate.yml"]
	order := []string{"Back up boot", "Remove arguments that force cgroup v1", "Add the unified cgroup hierarchy", "Use the systemd cgroup driver in containerd", "Use the systemd cgroup driver in the kubelet", "Cordon the node", "Mark the node as being migrated", "Drain the node", "Reboot the node"}
	last := -1
	for _, name := range order {
		i := strings.Index(m, name)
		if i < 0 || i < last {
			t.Fatalf("task %q is missing or out of order (configuration must be edited before the node is cordoned)", name)
		}
		last = i
	}
}

func TestVerifyIsIdempotentAndRobust(t *testing.T) {
	v := generated(t)["ansible/roles/cgroupv2/tasks/verify.yml"]
	if !strings.Contains(v, "cgroupv2_node.resources is defined") {
		t.Fatal("Ready wait must tolerate an unreachable API server right after a reboot")
	}
	if !strings.Contains(v, "cgroupguard.io/migrating") || strings.Contains(v, "when: cgroupv2_needed | bool and not ansible_check_mode\n\n- name: Uncordon") {
		t.Fatal("uncordon must depend on the marker annotation, not on cgroupv2_needed")
	}
}

func TestRollbackDeletesRestoredBackups(t *testing.T) {
	r := generated(t)["ansible/roles/cgroupv2/tasks/rollback.yml"]
	if !strings.Contains(r, "state: absent") || !strings.Contains(r, ".cgroupguard.bak") {
		t.Fatal("rollback must delete restored backups so a later attempt does not restore stale files")
	}
}

// Defects found by running the role on a real Rocky Linux 9 (RedHat-like) cgroup v1 node.
func TestRedHatPathHandlesMissingDropinDirAndRecordsKernelArgs(t *testing.T) {
	g := generated(t)
	pre := g["ansible/roles/cgroupv2/tasks/preflight.yml"]
	if !strings.Contains(pre, "cgroupv2_grub_dropin_dir_stat.stat.isdir | default(false)") {
		t.Fatal("the drop-in check must only run when the directory exists (it does not on RedHat-like hosts)")
	}
	if strings.Contains(pre, "or could not be checked") {
		t.Fatal("a missing directory must not be reported as files forcing cgroup v1")
	}
	mig := g["ansible/roles/cgroupv2/tasks/migrate.yml"]
	if i, j := strings.Index(mig, "Keep a copy of the kernel arguments for rollback"), strings.Index(mig, "Switch the kernel command line to cgroup v2 with grubby"); i < 0 || j < 0 || i > j {
		t.Fatal("kernel arguments must be recorded before grubby changes them")
	}
	rb := g["ansible/roles/cgroupv2/tasks/rollback.yml"]
	for _, want := range []string{"Restore the original cgroup v1 arguments with grubby", "systemd.legacy_systemd_cgroup_controller=1", "Delete the recorded kernel arguments"} {
		if !strings.Contains(rb, want) {
			t.Fatalf("rollback lacks %q", want)
		}
	}
}

func TestDryRunDoesNotAssertOnSimulatedChanges(t *testing.T) {
	// In --check mode the edit is only simulated, so a follow-up check of the file would fail the dry run.
	mig := generated(t)["ansible/roles/cgroupv2/tasks/migrate.yml"]
	i := strings.Index(mig, "Require SystemdCgroup = true")
	if i < 0 {
		t.Fatal("task missing")
	}
	next := strings.Index(mig[i:], "- name:")
	block := mig[i:]
	if j := strings.Index(block[1:], "    - name:"); j > 0 {
		block = block[:j+1]
	}
	_ = next
	if !strings.Contains(block, "when: not ansible_check_mode") {
		t.Fatal("the post-edit check must be skipped in check mode")
	}
}
