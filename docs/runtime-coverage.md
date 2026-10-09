# Runtime and agent coverage

Rules are versioned data shipped inside the binary. A family is either
`verified` (every claim is stated by the cited primary source) or `unverified`
(the loader only allows `review` or `info` findings, never a version threshold).
Image tags are mutable hints, not measurements of the installed runtime.

| Family | Class | Status | Behavior | Basis |
|---|---|---|---|---|
| HotSpot/OpenJDK (`openjdk`, `eclipse-temurin`) | runtime | verified | risk below 8u372 / 11.0.16 / 15 | Kubernetes cgroups page |
| Azul Zulu | runtime | verified | same thresholds (tags lead with the upstream JDK version) | Azul April 2023 release notes (JDK-8230305 in 8.70.0.23) |
| BellSoft Liberica | runtime | verified | same thresholds | Liberica 11.0.16+8 release notes (JDK-8230305) |
| cAdvisor | agent | verified | risk below 0.43.0 | Kubernetes cgroups page |
| Node.js | runtime | verified | risk below 20.3.0 (CG108); major-only tags are review | Kubernetes cgroups page; libuv 1.45.0 |
| .NET | runtime | verified | risk below 5.0 (CG109); major.minor tags are decisive | dotnet/runtime#30337 (milestone 5.0.0) |
| Go (`golang`) | runtime | verified | hint at 1.25+, review below (needs automaxprocs ≥1.5.1 evidence) | Go 1.25 release notes |
| Fluent Bit | agent | verified | risk below 2.2.0 (CG110), docker input only | Fluent Bit 2.2 announcement |
| Elastic Beats | agent | verified | risk below 7.15.0 (CG110) | Beats 7.15.0 release notes |
| IBM Semeru / IBM Java | runtime | verified | review; thresholds in the note (tags do not carry build numbers) | Kubernetes cgroups page |
| Corretto, SapMachine, Microsoft OpenJDK | runtime | unverified | review; vendors do not state cgroup v2 support, Microsoft tags are major-only | see sources.md |
| Datadog, Falco, node_exporter, Telegraf, OTel, Splunk OTel, Grafana, Sysdig, New Relic, Dynatrace, Elastic Agent | agent | unverified | review with per-agent notes; no version encoded | Kubernetes cgroups page (general guidance) |
| Python, Rust, BEAM | runtime | unverified | info only (CPU-count behavior, not a v1 path dependency) | see sources.md |
| Kubernetes core, kind add-ons | platform | unverified | info (CG107) | not assessed |

Known source conflict: the OKD documentation lists older IBM Semeru minimums (8u345-b01, 11.0.16.0, 17.0.4.0, 18.0.2.0, with OpenJ9 0.33
as the first release) than the Kubernetes page (8.0.382.0, 11.0.20.0, 17.0.8.0). The newer Kubernetes numbers are used and Semeru stays review-only.

Finding IDs: CG101 HotSpot risk, CG102 cadvisor risk, CG103 legacy path in manifest,
CG104 unrecognized image, CG105 review, CG106 meets documented baseline (hint),
CG107 informational, CG108 Node.js risk, CG109 .NET risk, CG110 agent risk.

## Not covered yet

Per-agent minimum versions beyond Fluent Bit and Beats (vendor documentation could not be fetched
or stated no first-support version), explicit Corretto/SapMachine/Microsoft statements, Ruby/PHP (no v1/v2 difference found), image-layer or SBOM based
detection of automaxprocs and similar libraries.
