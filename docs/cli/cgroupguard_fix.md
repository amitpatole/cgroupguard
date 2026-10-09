## cgroupguard fix

Generate a remediation plan; optionally apply its Kubernetes steps or open a draft pull request

### Synopsis

Builds a deterministic remediation plan from a fresh scan. --plan changes nothing. --apply performs only typed cordon, drain and uncordon steps through the Kubernetes API (evictions respect PodDisruptionBudgets; nothing is forced), asks you to type each step ID, and writes an audit log. --pr opens a draft pull request. Terraform and Ansible are never run by this command, and files are never written outside --output-dir.
With --emit, --validate checks the generated files with real tools; terraform init and ansible-galaxy download and run provider plugins and collections from the network, so validation is opt-in.
Cloud-provider, node-local and reboot steps remain manual by design.

```
cgroupguard fix (--plan | --apply | --pr) [flags]
```

### Options

```
      --apply                       Perform the plan's typed Kubernetes steps (cordon, drain, uncordon) one at a time, each confirmed by typing its ID
      --audit-log string            New file for the hash-chained audit log (required with --apply and --pr)
      --context string              Kubeconfig context
      --dry-run                     With --apply: server-side dry run of every step; with --pr: show what would be created without any network access
      --emit strings                Also generate reviewable files: terraform, ansible (needs --output-dir)
      --format string               Stdout format when --output-dir is not used: markdown or json (default "markdown")
  -h, --help                        help for fix
      --input string                Offline snapshot JSON (schemaVersion 1)
      --kubeconfig string           Kubeconfig path; defaults to standard client-go loading
      --kubelet-metrics             Read kubelet_cgroup_version through the API-server node proxy (needs nodes/proxy; opt-in)
      --local                       Inspect this Linux mount namespace; no cluster connection
      --namespace string            Existing namespace for probe pods
      --no-validate                 Write generated files without validating them (recorded in the plan); you must validate them yourself
      --node-events                 Also read kubelet CgroupV1 Warning Events (needs list on events; absence is never evidence of v2)
      --nodes-only                  Skip the live workload inventory and read node information only
      --output-dir string           Directory for RUNBOOK.md, plan.json and generated files (created if missing)
      --overwrite                   Replace existing files in --output-dir (default: refuse)
      --path string                 Cgroup root for --local (default "/sys/fs/cgroup")
      --plan                        Generate a plan (nothing is changed)
      --pr                          Open a DRAFT pull/merge request with the plan and generated files (needs --pr-repo; confirmed by typing the repository)
      --pr-base string              Target branch (default "main")
      --pr-base-url string          API base URL (for GitHub Enterprise or self-hosted GitLab)
      --pr-branch string            New branch name (default: derived from the content)
      --pr-forge string             github or gitlab (default "github")
      --pr-repo string              Repository (owner/name, or the full GitLab project path)
      --pr-token-env string         Environment variable holding the access token (default GITHUB_TOKEN or GITLAB_TOKEN)
      --probe                       Explicitly authorize temporary hostPath probe pod creation/deletion
      --probe-image string          Trusted cgroupguard image pinned by digest
      --prometheus-url string       Read kubelet_cgroup_version from an existing Prometheus (http/https URL)
      --stopgap-until string        Include the temporary failCgroupV1=false step with this expiry date (YYYY-MM-DD)
  -t, --target-version string       Planned Kubernetes upgrade target; scales finding severity (for example 1.35) (default "1.35")
      --timeout duration            Overall live scan timeout (default 5m0s)
      --validate                    Validate generated files with terraform, tflint, trivy, yamllint and ansible-lint first. This downloads provider plugins and Ansible collections from the network and runs them
      --workload-namespace string   Only scan workloads in this namespace (default: all)
```

### SEE ALSO

* [cgroupguard](cgroupguard.md)	 - Evidence-based cgroup v1 to v2 readiness checker

