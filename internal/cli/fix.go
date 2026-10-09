package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/amitpatole/cgroupguard/internal/apply"
	"github.com/amitpatole/cgroupguard/internal/audit"
	"github.com/amitpatole/cgroupguard/internal/detect"
	"github.com/amitpatole/cgroupguard/internal/emit"
	"github.com/amitpatole/cgroupguard/internal/forge"
	"github.com/amitpatole/cgroupguard/internal/plan"
	"github.com/amitpatole/cgroupguard/internal/validate"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// stdin and stdinIsTerminal are variables so tests can supply a terminal.
var (
	stdin           io.Reader = os.Stdin
	stdinIsTerminal           = func() bool { return term.IsTerminal(int(os.Stdin.Fd())) }
)

type typedConfirmer struct {
	in  *bufio.Reader
	out io.Writer
}

// Confirm requires the operator to type the step ID. Anything else declines.
func (c *typedConfirmer) Confirm(s plan.Step) (bool, error) {
	_, _ = fmt.Fprintf(c.out, "  type %s to run this step, or press Enter to stop: ", s.ID)
	line, err := c.in.ReadString('\n')
	if err != nil && line == "" {
		return false, nil
	}
	return strings.TrimSpace(line) == s.ID, nil
}

func newAuditFile(path string) (*audit.Log, func(), error) { return audit.Create(path) }

func fixCommand(out, errOut io.Writer) *cobra.Command {
	var so scanOpts
	var makePlan, doApply, doPR, dryRun, validateFiles, noValidate, overwrite bool
	var auditPath, prForge, prRepo, prBase, prBranch, prBaseURL, prTokenEnv string
	var emitKinds []string
	var outputDir, format, stopgapUntil string
	cmd := &cobra.Command{Use: "fix (--plan | --apply | --pr)", Short: "Generate a remediation plan; optionally apply its Kubernetes steps or open a draft pull request", Args: cobra.NoArgs,
		Long: "Builds a deterministic remediation plan from a fresh scan. --plan changes nothing. --apply performs only typed cordon, drain and uncordon steps through the Kubernetes API (evictions respect PodDisruptionBudgets; nothing is forced), asks you to type each step ID, and writes an audit log. --pr opens a draft pull request. Terraform and Ansible are never run by this command, and files are never written outside --output-dir.\n" +
			"With --emit, --validate checks the generated files with real tools; terraform init and ansible-galaxy download and run provider plugins and collections from the network, so validation is opt-in.\n" +
			"Cloud-provider, node-local and reboot steps remain manual by design.",
		RunE: func(cmd *cobra.Command, args []string) error {
			modes := 0
			for _, m := range []bool{makePlan, doApply, doPR} {
				if m {
					modes++
				}
			}
			if modes != 1 {
				return fmt.Errorf("fix needs exactly one of --plan, --apply or --pr")
			}
			if (doApply || doPR) && auditPath == "" {
				return fmt.Errorf("--audit-log is required with --apply and --pr: every action must be recorded")
			}
			if dryRun && makePlan {
				return fmt.Errorf("--dry-run applies to --apply and --pr")
			}
			if doApply && (so.input != "" || so.local) {
				return fmt.Errorf("--apply needs a live cluster scan (it acts on the cluster you are connected to)")
			}
			if format != "markdown" && format != "json" {
				return fmt.Errorf("--format must be markdown or json")
			}
			if len(emitKinds) > 0 && outputDir == "" {
				return fmt.Errorf("--emit requires --output-dir (generated files are never printed to the terminal)")
			}
			if len(emitKinds) > 0 && validateFiles == noValidate {
				return fmt.Errorf("--emit needs exactly one of --validate or --no-validate: validation runs terraform init and ansible-galaxy, which download and run provider plugins and collections from the network")
			}
			if len(emitKinds) == 0 && (validateFiles || noValidate) {
				return fmt.Errorf("--validate and --no-validate only apply with --emit")
			}
			r, e := so.scan(cmd)
			if e != nil {
				return e
			}
			p, e := plan.Build(r, plan.Options{StopgapUntil: stopgapUntil})
			if e != nil {
				return e
			}
			if doApply {
				return runApply(cmd, so, p, dryRun, auditPath, out, errOut)
			}
			files := stepFiles(p)
			if len(emitKinds) > 0 {
				gen, e := emit.Generate(p, emitKinds)
				if e != nil {
					return e
				}
				if len(gen) == 0 {
					p.Limitations = append(p.Limitations, "--emit produced no files: no node group matched the requested generators.")
				}
				if noValidate {
					p.Limitations = append(p.Limitations, "Generated infrastructure-as-code was NOT validated (--no-validate). Run terraform validate, tflint, a policy scanner, yamllint and ansible-lint yourself before any use.")
				} else if len(gen) > 0 {
					results := validate.Files(cmd.Context(), gen)
					for _, res := range results {
						_, _ = fmt.Fprintln(errOut, res.String())
					}
					if validate.Failed(results) {
						for _, res := range results {
							if !res.OK {
								_, _ = fmt.Fprintf(errOut, "--- %s %s ---\n%s\n", res.Tool, res.Target, strings.TrimSpace(res.Output))
							}
						}
						return fmt.Errorf("generated files failed validation or a validator is not installed; nothing was written (use --no-validate only if you will validate them yourself)")
					}
				}
				files = append(files, gen...)
			}
			runbook := p.Runbook()
			js, e := p.JSON()
			if e != nil {
				return e
			}
			if doPR {
				all := append([]plan.File{{Path: "RUNBOOK.md", Content: runbook}, {Path: "plan.json", Content: string(js)}}, files...)
				return runPR(cmd, forge.Spec{Kind: prForge, BaseURL: prBaseURL, Repo: prRepo, Base: prBase, Branch: prBranch, Title: "cgroup v2 remediation plan for Kubernetes " + p.Target, Body: prBody(p), Files: all}, prTokenEnv, dryRun, auditPath, out)
			}
			if outputDir == "" {
				if format == "json" {
					_, e = out.Write(js)
				} else {
					_, e = io.WriteString(out, runbook)
				}
				return e
			}
			all := append([]plan.File{{Path: "RUNBOOK.md", Content: runbook}, {Path: "plan.json", Content: string(js)}}, files...)
			if e := writeOutputs(outputDir, all, overwrite); e != nil {
				return e
			}
			_, _ = fmt.Fprintf(out, "Wrote %d file(s) to %s. Nothing was executed; read RUNBOOK.md first.\n", len(all), outputDir)
			return nil
		}}
	f := cmd.Flags()
	so.bind(f)
	f.BoolVar(&makePlan, "plan", false, "Generate a plan (nothing is changed)")
	f.BoolVar(&doApply, "apply", false, "Perform the plan's typed Kubernetes steps (cordon, drain, uncordon) one at a time, each confirmed by typing its ID")
	f.BoolVar(&doPR, "pr", false, "Open a DRAFT pull/merge request with the plan and generated files (needs --pr-repo; confirmed by typing the repository)")
	f.BoolVar(&dryRun, "dry-run", false, "With --apply: server-side dry run of every step; with --pr: show what would be created without any network access")
	f.StringVar(&auditPath, "audit-log", "", "New file for the hash-chained audit log (required with --apply and --pr)")
	f.StringVar(&prForge, "pr-forge", "github", "github or gitlab")
	f.StringVar(&prRepo, "pr-repo", "", "Repository (owner/name, or the full GitLab project path)")
	f.StringVar(&prBase, "pr-base", "main", "Target branch")
	f.StringVar(&prBranch, "pr-branch", "", "New branch name (default: derived from the content)")
	f.StringVar(&prBaseURL, "pr-base-url", "", "API base URL (for GitHub Enterprise or self-hosted GitLab)")
	f.StringVar(&prTokenEnv, "pr-token-env", "", "Environment variable holding the access token (default GITHUB_TOKEN or GITLAB_TOKEN)")
	f.StringSliceVar(&emitKinds, "emit", nil, "Also generate reviewable files: terraform, ansible (needs --output-dir)")
	f.StringVar(&outputDir, "output-dir", "", "Directory for RUNBOOK.md, plan.json and generated files (created if missing)")
	f.StringVar(&format, "format", "markdown", "Stdout format when --output-dir is not used: markdown or json")
	f.StringVar(&stopgapUntil, "stopgap-until", "", "Include the temporary failCgroupV1=false step with this expiry date (YYYY-MM-DD)")
	f.BoolVar(&validateFiles, "validate", false, "Validate generated files with terraform, tflint, trivy, yamllint and ansible-lint first. This downloads provider plugins and Ansible collections from the network and runs them")
	f.BoolVar(&noValidate, "no-validate", false, "Write generated files without validating them (recorded in the plan); you must validate them yourself")
	f.BoolVar(&overwrite, "overwrite", false, "Replace existing files in --output-dir (default: refuse)")
	cmd.MarkFlagsMutuallyExclusive("input", "local")
	return cmd
}

// stepFiles collects review files attached to plan steps, de-duplicated by path.
func stepFiles(p plan.Plan) []plan.File {
	seen := map[string]bool{}
	var files []plan.File
	for _, ph := range p.Phases {
		for _, s := range ph.Steps {
			for _, f := range s.Files {
				if !seen[f.Path] {
					seen[f.Path] = true
					files = append(files, f)
				}
			}
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files
}

// writeOutputs writes files below dir only. Paths must be relative and stay inside dir;
// symlinks cannot escape (os.Root). Existing files are never replaced unless asked.
func writeOutputs(dir string, files []plan.File, overwrite bool) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	seen := map[string]bool{}
	for _, f := range files {
		clean := filepath.Clean(f.Path)
		if f.Path == "" || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") || seen[clean] {
			return fmt.Errorf("refusing unsafe or duplicate output path %q", f.Path)
		}
		seen[clean] = true
		if !overwrite {
			if _, err := root.Lstat(clean); err == nil {
				return fmt.Errorf("%s already exists in %s (use --overwrite to replace)", clean, dir)
			} else if !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
	for _, f := range files {
		clean := filepath.Clean(f.Path)
		if d := filepath.Dir(clean); d != "." {
			if err := root.MkdirAll(d, 0o750); err != nil {
				return err
			}
		}
		flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
		if !overwrite {
			flags = os.O_WRONLY | os.O_CREATE | os.O_EXCL
		}
		h, err := root.OpenFile(clean, flags, 0o640)
		if err != nil {
			return err
		}
		_, werr := io.WriteString(h, f.Content)
		if err := errors.Join(werr, h.Close()); err != nil {
			return err
		}
	}
	return nil
}

func runApply(cmd *cobra.Command, so scanOpts, p plan.Plan, dryRun bool, auditPath string, out, errOut io.Writer) error {
	au, closeLog, err := newAuditFile(auditPath)
	if err != nil {
		return err
	}
	defer closeLog()
	if !dryRun && !stdinIsTerminal() {
		return fmt.Errorf("--apply needs an interactive terminal to confirm each step; use --dry-run to preview without one")
	}
	client, err := detect.Client(so.kubeconfig, so.contextName)
	if err != nil {
		return err
	}
	conf := &typedConfirmer{in: bufio.NewReader(stdin), out: out}
	err = apply.Run(cmd.Context(), client, p, apply.Options{DryRun: dryRun, Confirm: conf, Audit: au, Out: out})
	if errors.Is(err, apply.ErrDeclined) {
		_, _ = fmt.Fprintln(errOut, "stopped at your request; completed steps are listed in the audit log")
		return nil
	}
	return err
}

func prBody(p plan.Plan) string {
	return "Generated by cgroupguard. Nothing in this change has been applied.\n\n" +
		"Review checklist:\n- [ ] Read RUNBOOK.md end to end, including rollback for every step.\n- [ ] Placeholders (REPLACE_WITH_*) are filled in and values match the old node groups.\n" +
		"- [ ] Terraform: remote state is encrypted and locked, authentication uses workload identity or OIDC, `terraform plan` shows only additions.\n" +
		"- [ ] Ansible: inventory reviewed, canary group first, dry run (`--check --diff`) clean.\n- [ ] A named person approves each disruptive step.\n"
}

func runPR(cmd *cobra.Command, spec forge.Spec, tokenEnv string, dryRun bool, auditPath string, out io.Writer) error {
	if spec.Repo == "" {
		return fmt.Errorf("--pr needs --pr-repo")
	}
	if err := spec.Prepare(); err != nil {
		return err
	}
	au, closeLog, err := newAuditFile(auditPath)
	if err != nil {
		return err
	}
	defer closeLog()
	desc := spec.Describe()
	if dryRun {
		_, _ = fmt.Fprint(out, "DRY RUN, no network access:\n"+desc)
		return au.Log("pr_dry_run", map[string]any{"repo": spec.Repo, "branch": spec.Branch, "files": len(spec.Files)})
	}
	if tokenEnv == "" {
		tokenEnv = map[string]string{"github": "GITHUB_TOKEN", "gitlab": "GITLAB_TOKEN"}[spec.Kind]
	}
	token := os.Getenv(tokenEnv)
	if token == "" {
		return fmt.Errorf("environment variable %q is empty", tokenEnv)
	}
	if !stdinIsTerminal() {
		return fmt.Errorf("--pr needs an interactive terminal to confirm; use --dry-run to preview without one")
	}
	_, _ = fmt.Fprint(out, desc+"This sends the files above to the repository. Type the repository name to continue, or press Enter to stop: ")
	line, _ := bufio.NewReader(stdin).ReadString('\n')
	if strings.TrimSpace(line) != spec.Repo {
		_ = au.Log("pr_declined", map[string]any{"repo": spec.Repo})
		_, _ = fmt.Fprintln(out, "stopped; nothing was sent")
		return nil
	}
	_ = au.Log("pr_confirmed", map[string]any{"repo": spec.Repo, "branch": spec.Branch, "files": len(spec.Files)})
	url, err := forge.Open(cmd.Context(), spec, token)
	if err != nil {
		_ = au.Log("pr_failed", map[string]any{"repo": spec.Repo, "error": err.Error()})
		return err
	}
	_ = au.Log("pr_opened", map[string]any{"repo": spec.Repo, "branch": spec.Branch})
	_, err = fmt.Fprintf(out, "Opened draft: %s\n", url)
	return err
}
