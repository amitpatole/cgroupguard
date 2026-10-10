# CI gating

Use cgroupguard in a pipeline to stop an upgrade while cgroup v1 nodes or risky workloads remain. It exits with a code your CI understands and
can publish a SARIF report to a code-scanning dashboard.

## The exit codes

| Command | Exit | Meaning |
|---|---|---|
| `check` | `0` | Never happens today: coverage is always incomplete, so the default mode exits `1` even for a clean report. |
| `check` | `1` | Findings at or above `--fail-on`, **or** incomplete coverage (always true today). |
| `check` | `2` | Bad input, permission or API failure, timeout. |
| `check --exit-error` (`-e`) | `0` | Nothing at or above `--fail-on`. A note on stderr says coverage is incomplete: this is **not** an upgrade certification. |
| `check --exit-error` | `200` | Findings at or above `--fail-on` (the same convention as kubent's `--exit-error`). |
| `check --exit-error` | `2` | Same as above: an error. |

`--fail-on` is `high`, `warning` (the default), `note` or `none`. For a pipeline, use `--exit-error`:

```console
$ cgroupguard check --input examples/plan-mixed.json -e;                   echo "exit $?"
exit 200
$ cgroupguard check --input examples/plan-mixed.json -e --fail-on high;    echo "exit $?"
exit 200
$ cgroupguard check --input examples/plan-mixed.json -e --fail-on none;    echo "exit $?"
exit 0
```

The sample has six `high` findings (five v1 nodes and one hybrid), so both of the first two fail. Treat exit `2` as "the gate could not run", not as "passed".

## Install the tool in CI, and verify it

Do not pipe a download into a shell. The repository contains a verifying installer, `distribution/action/install.sh`, that downloads a release, checks the
Cosign signature of `checksums.txt` against **your pinned copy of the public key**, checks the archive's checksum, and only then installs the binary.
It fails closed on any problem.

1. Copy `distribution/action/install.sh` to `ci/install-cgroupguard.sh` and `distribution/cosign.pub` to `ci/cgroupguard-cosign.pub` in your own repository, and read the script once.
2. Have `cosign` on the `PATH`.
3. Call it with the version and the release host:

```console
$ VERSION=0.1.0 \
  BASE_URL=https://github.com/amitpatole/cgroupguard/releases/download \
  PUBKEY=ci/cgroupguard-cosign.pub \
  DEST="$HOME/bin" \
  ci/install-cgroupguard.sh
```

The script runs on Linux and macOS (amd64 and arm64). On Windows, download the archive and verify it by hand as shown in [Install](../install.md).

## Give the pipeline a read-only identity

The scan needs only the two read-only roles in `deploy/`, see [Scan a cluster](scan-a-cluster.md). Use a short-lived identity (workload identity, OIDC or a
cloud SSO role) for the pipeline's kubeconfig, never a long-lived admin token.

## Recipes

Ready-made files are in the `examples/ci/` folder. They run kubent, Pluto and cgroupguard together, because the three look at different problems
(API deprecations, API removals, cgroup readiness).

| System | File |
|---|---|
| Shell, any system | `examples/ci/upgrade-gate.sh` |
| GitHub Actions | `examples/ci/github-actions.yml` |
| GitLab CI | `examples/ci/gitlab-ci.yml` |
| Jenkins | `examples/ci/Jenkinsfile` |
| TeamCity (Kotlin DSL) | `examples/ci/teamcity-kotlin.kts` |
| Tekton | `examples/ci/tekton-task.yaml` |

A minimal GitLab job:

```yaml
upgrade-gate:
  stage: test
  script:
    - ci/install-cgroupguard.sh        # with VERSION, BASE_URL, PUBKEY and DEST set as variables
    - export PATH="$CI_PROJECT_DIR/bin:$PATH"
    - cgroupguard check --target-version 1.35 --exit-error --fail-on warning --format sarif > cgroupguard.sarif
  artifacts:
    when: always
    paths: [cgroupguard.sarif]
```

## SARIF in GitHub code scanning

```yaml
      - name: Run the check
        run: cgroupguard check --target-version 1.35 -e --format sarif > cgroupguard.sarif
        continue-on-error: true            # let the upload run even when the gate fails
      - uses: github/codeql-action/upload-sarif@<full-commit-sha>
        with: { sarif_file: cgroupguard.sarif }
```

Each result carries the rule ID, the severity (`high` → error, `warning` → warning), the node or workload as a logical location, a stable fingerprint so the
same finding is tracked between runs, and the blast radius in the result properties.

## Reading the result in a script

```console
$ cgroupguard check --context my-cluster --format json > report.json
$ jq -r '.findings[] | select(.severity=="high") | "\(.ruleId) \(.node)"' report.json
CG001 aks-c-1
CG001 eks-b-1
CG001 gke-a-1
```

```console
$ jq '[.findings[] | .severity] | group_by(.) | map({(.[0]): length}) | add' report.json
{"high": 6, "warning": 6}
```

See [Output and exit codes](../output.md) for every field. A target below 1.35 downgrades cgroup v1 nodes from `high` to `warning`:
run the check with the version you are *actually* upgrading to.

## Gating on a trend

Reports are byte-for-byte reproducible for the same evidence, so you can store `report.json` and fail only when the number of `high` findings **grows**:

```console
$ before=$(jq '[.findings[] | select(.severity=="high")] | length' previous.json)
$ after=$(jq  '[.findings[] | select(.severity=="high")] | length' report.json)
$ [ "$after" -le "$before" ] || { echo "new high findings"; exit 1; }
```
