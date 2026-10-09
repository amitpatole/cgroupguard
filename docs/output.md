# Exit codes and output

| Code | Meaning |
|---|---|
| 0 | utility command succeeded; with `--exit-error`, nothing at or above `--fail-on` (a note says coverage is still incomplete) |
| 1 | default `check`: findings at or above `--fail-on`, or coverage is incomplete (always true today) |
| 2 | bad input, permission or API failure, timeout |
| 200 | `--exit-error` only: findings at or above `--fail-on` (the same convention as kubent's `--exit-error`) |

`--fail-on` is `high`, `warning` (default), `note` or `none`. An exit code of 0 is never an upgrade certification.

Formats (`--format` or `-o`): `table` (default), `json`, `markdown`, `sarif` (2.1.0, for code-scanning tools), and `kubent-compatible`
(JSON with kubent's field names: Name, Namespace, Kind, ApiVersion, RuleSet, ReplaceWith, Since; `ApiVersion` carries the cgroupguard
rule ID because these findings are not API deprecations; it is shape-compatible only).

Reports are deterministic: the same evidence always produces byte-identical output.
