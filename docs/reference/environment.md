# Environment variables

cgroupguard reads very few environment variables, and never takes a secret as a command-line flag.

| Variable | Used by | Meaning |
|---|---|---|
| `KUBECONFIG` | every command that talks to a cluster | The usual Kubernetes client rules apply (`--kubeconfig` overrides it, `--context` picks a context) |
| `ANTHROPIC_API_KEY` | `agent explain`, `agent mcp` with `--provider anthropic` | The API key. Use `--api-key-env NAME` to read it from a different variable. |
| *(any name you choose)* | `agent explain` with `--provider openai-compatible` | Pass its **name** with `--api-key-env NAME` if your server needs a key |
| `GITHUB_TOKEN`, `GITLAB_TOKEN` | `fix --pr` | The access token for the service (`--pr-forge github` or `gitlab`). `--pr-token-env NAME` reads another variable. |

Tokens are read once, never printed or written to a log, and never sent anywhere except the service named by `--pr-forge`/`--pr-base-url`.

With `fix --validate`, the external tools (`terraform`, `tflint`, `trivy`, `yamllint`, `ansible-playbook`, `ansible-lint`) run with a minimal environment of `PATH`, `HOME`
and a few fixed values, not with your whole environment.

The in-cluster controller is configured with Helm values and command-line flags, not environment variables. See [Controller and monitoring](../guides/controller-and-monitoring.md).
