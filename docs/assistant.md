# Assistant (optional)

Nothing in cgroupguard needs a language model. `cgroupguard agent explain` asks a model you configure to explain a scan and its plan.

```bash
export ANTHROPIC_API_KEY=...            # read from the environment; never pass keys as flags
cgroupguard agent explain --context YOUR_CONTEXT --provider anthropic --model MODEL --audit-log audit.jsonl
cgroupguard agent explain --input snapshot.json --provider openai-compatible --model llama3.1:8b --base-url http://localhost:11434/v1
cgroupguard agent verify-log audit.jsonl
cgroupguard agent mcp --input snapshot.json           # the same read-only tools over MCP (stdio)
```

- The model can only call **read-only tools** over an already collected report. It cannot reach the cluster, run commands or change the plan.
- **What is sent:** node, namespace, workload, pool and image names are replaced by pseudonyms; secrets, tokens and credentials in URLs are removed.
  `--reveal-names` and `--send-images` opt out of the pseudonyms (secrets are still removed).
- **Limits:** `--max-steps` (default 8), `--max-tokens` (default 60000).
- **Audit log:** a new hash-chained file recording every request, tool call and refusal; `verify-log` detects edits.
- The output is labeled **advisory and unverified**. Models make mistakes; the deterministic report and plan are authoritative.
- https is required except to a local address unless you pass `--allow-insecure-http`.
