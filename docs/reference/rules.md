# Rule catalogue

Every finding has an ID. Rules are versioned data shipped inside the binary, and each one cites a primary source in [Sources](../sources.md).

## Node rules

| ID | Severity | Fires when | What to do |
|---|---|---|---|
| `CG001` | `high` for target 1.35 or later, `warning` for an earlier target | A node is measured on **cgroup v1** | Migrate the node to cgroup v2 before upgrading to 1.35. See the [managed](../guides/plan-managed-clusters.md) and [self-managed](../guides/plan-self-managed-nodes.md) guides. |
| `CG002` | same as `CG001` | A node is on a **hybrid** layout (v1 controllers plus a v2 mount) | A hybrid layout is not the unified v2 hierarchy. Migrate to unified v2. |
| `CG003` | `warning` | The node's mode is **unknown** | Add evidence: [Evidence sources](../guides/evidence-sources.md). OS image, kernel and Kubernetes version are never accepted as proof. |

## Workload rules

Workload rules look at the **image reference** (repository and tag) and at **paths in the manifest**. They do not look inside images. Tags are mutable hints.

| ID | Severity | Fires when | What to do |
|---|---|---|---|
| `CG101` | `warning` | A HotSpot-family JVM image (OpenJDK, Eclipse Temurin, Azul Zulu, BellSoft Liberica) is below 8u372, 11.0.16 or 15 | Move to a build with cgroup v2 container support, then confirm in the running container. |
| `CG102` | `warning` | cAdvisor below 0.43.0 | Update cAdvisor. |
| `CG103` | `warning` | A container's command, arguments or volume mounts reference a legacy v1 path: `/sys/fs/cgroup/memory/`, `/cpu/`, `/cpuacct/`, `/cpuset/` or `/cpu,cpuacct/` | Read the referenced script or mount. Direct reads of v1 controller paths can fail on v2. The tool does not execute anything. |
| `CG104` | `warning` | The image matches no rule | Inspect the image's runtime and any custom cgroup access before migration. This is an "unknown", not a verdict. |
| `CG105` | `warning` | A recognized family has no verified threshold (for example Corretto, SapMachine, Microsoft OpenJDK, IBM Semeru, most monitoring agents) | Check the vendor's notes; the finding's text names the family. |
| `CG106` | `note` | The image meets a documented baseline (for example Go 1.25 or later) | Informational: it is a hint, not proof. |
| `CG107` | `note` | Informational only (Python, Rust, the BEAM, Kubernetes core and add-ons) | Nothing is known to depend on v1 paths. |
| `CG108` | `warning` | Node.js below 20.3.0 (a bare major tag is `CG105`, not decisive) | Update; older Node.js does not reliably detect cgroup v2 memory limits. |
| `CG109` | `warning` | .NET below 5.0 | Update; .NET Core 3.1 and earlier have no Microsoft cgroup v2 support. |
| `CG110` | `warning` | A logging agent below its documented minimum: Fluent Bit below 2.2.0 (docker input), Elastic Beats below 7.15.0 | Update the agent. |

How sure each runtime family is, and the exact thresholds, are in [Runtime coverage](../runtime-coverage.md). A family marked *unverified* can only ever produce `CG105` or
`CG107`: the tool never turns a claim into a pass or fail unless a primary source states it.

## Reading a rule in a report

```text
CG108 [warning] shop/StatefulSet/db/regular/db
  Evidence: Image-tag heuristic for node (runtime, verified rule): node:18.20.4
  Why: Image tag suggests a version below the documented cgroup v2 support threshold 20.3.0; verify the actual binary and vendor backports. Node.js reads cgroup v2 memory limits (via libuv) from v20.3.0; the v18 line does not reliably detect them.
```

`confidence` is `low` for every image-tag finding. A tag says what an image claims to be, not what is inside it.

## Severity at a glance

| Severity | Meaning | Fails `--fail-on` |
|---|---|---|
| `high` | blocks the target version | `high`, `warning`, `note` |
| `warning` | likely trouble or needs review | `warning`, `note` |
| `note` | informational | `note` |
