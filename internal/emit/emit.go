// Package emit renders reviewable infrastructure-as-code from a plan. It only
// produces text; it never runs Terraform or Ansible. Every interpolated value is a
// validated name or a fixed constant.
package emit

import (
	"fmt"
	"sort"
	"strings"

	"github.com/amitpatole/cgroupguard/internal/plan"
)

// Generate returns files for the requested kinds ("terraform", "ansible"), sorted by path.
func Generate(p plan.Plan, kinds []string) ([]plan.File, error) {
	want := map[string]bool{}
	for _, k := range kinds {
		switch k {
		case "terraform", "ansible":
			want[k] = true
		default:
			return nil, fmt.Errorf("unknown --emit value %q (use terraform and/or ansible)", k)
		}
	}
	var files []plan.File
	var selfManaged []plan.Group
	for _, g := range p.Groups {
		for _, n := range g.Nodes {
			if !plan.SafeName(n) {
				return nil, fmt.Errorf("node name %q failed validation", n)
			}
		}
		if g.Pool != "" && !plan.SafeName(g.Pool) {
			return nil, fmt.Errorf("pool name %q failed validation", g.Pool)
		}
		switch g.Provider {
		case "gke", "eks", "aks":
			if want["terraform"] && g.Pool != "" {
				f, ok := terraformModule(g)
				if ok {
					files = append(files, f...)
				}
			}
		case "self-managed":
			selfManaged = append(selfManaged, g)
		}
	}
	if want["ansible"] && len(selfManaged) > 0 {
		files = append(files, ansibleProject(p.Target, selfManaged)...)
	}
	if want["terraform"] && hasTerraform(files) {
		files = append(files, plan.File{Path: "terraform/README.md", Content: terraformReadme})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

func hasTerraform(files []plan.File) bool {
	for _, f := range files {
		if strings.HasPrefix(f.Path, "terraform/") {
			return true
		}
	}
	return false
}

// hcl returns an HCL double-quoted string literal with interpolation disabled.
func hcl(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`, "\t", `\t`, "${", "$${", "%{", "%%{")
	return `"` + r.Replace(s) + `"`
}

// yamlStr returns a YAML single-quoted scalar.
func yamlStr(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

const terraformReadme = `# Generated Terraform

Nothing here has been applied. Review every file before use.

## Plan-review checklist (put this in the pull request)

- [ ] Remote state is encrypted and locked; no state file or credential is committed.
- [ ] Authentication uses workload identity / OIDC / a cloud SSO role, never keys in variables or files.
- [ ] ` + "`terraform plan`" + ` shows only **additions** for the replacement pool/group (no destroy, no replace of existing resources).
- [ ] Machine type, size, zones, labels, taints, service account / IAM role and subnets match the old pool; values marked REPLACE_WITH_ were filled in.
- [ ] Stateful pools carry prevent_destroy; the control plane is not managed by these files.
- [ ] The old pool is removed only after RUNBOOK.md cordon, drain, verification and soak are complete.
- [ ] Provider versions are pinned exactly (the versions this module was validated with). Run terraform init and commit the generated .terraform.lock.hcl.
- [ ] terraform fmt, terraform validate and your policy scanner pass in CI.
`
