# FAQ

**Does a clean result mean I can upgrade?** No. Cluster readiness is always `unknown`. A clean result means no known finding was found with the
evidence available; other upgrade prerequisites are not assessed.

**Why are my nodes "unknown"?** The Kubernetes node API does not expose the cgroup mode. Add evidence (see the [Quickstart](quickstart.md)).

**Does it change my cluster?** Not by default. `fix --apply` and `--probe` are the only things that touch the cluster, and each needs an explicit flag and,
for apply, a typed confirmation per step.

**Does it send my data anywhere?** Only if you run `agent` with a provider you chose, and then only pseudonymized, secret-free data.

**Which platforms have been tested?** Linux amd64 is built and exercised, including a complete migration and rollback on real cgroup v1 nodes running Ubuntu 24.04 and Rocky Linux 9, a multi-node
test cluster, and packages for Debian-like and RPM-like systems. Linux arm64 was verified on a real arm64 kernel in v1, hybrid and v2 modes. The Windows build passes its test suite under a
compatibility layer. The macOS build is compiled and checked but not yet run on a Mac; it is for reading snapshots and clusters, since local probing needs Linux. Linux arm64, macOS and Windows binaries and the arm64 container image are built but not yet run on
real hardware. Managed GKE, EKS and AKS clusters have not been tested; their plans are based on the providers' documentation.

**What about hybrid cgroup layouts?** They are reported as `hybrid` and treated as blocking, like v1: a hybrid layout is not the unified v2 hierarchy.

**Why does it say a rule is "unverified"?** The project only turns a claim into a pass or fail when a primary source states it. Everything else says "review".

**How do I report a security problem?** See [Security](security.md); do not open a public issue.
