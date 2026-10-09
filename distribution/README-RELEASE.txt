cgroupguard

Evidence-based Kubernetes cgroup v1 to v2 readiness checker. Read-only by default; its output is advisory and
never certifies that a cluster can be upgraded.

  cgroupguard check --context YOUR_CONTEXT          scan nodes and workloads
  cgroupguard fix --plan --context YOUR_CONTEXT     write a remediation runbook (changes nothing)
  cgroupguard --help                                all commands

deploy/        read-only RBAC for the scan, and the guardrail policy required before using --probe
LICENSE, NOTICE    Apache License 2.0 and attribution (keep NOTICE when redistributing)
THIRD-PARTY-NOTICES.txt    open source licenses of included components

Verify your download (checksums.txt.sigstore.json is published next to the archive):
  cosign verify-blob --key cosign.pub --bundle checksums.txt.sigstore.json --insecure-ignore-tlog=true checksums.txt
  sha256sum -c checksums.txt --ignore-missing
