# The assistant (optional)

Nothing in cgroupguard needs a language model. `cgroupguard agent explain` asks a model **you** configure to explain a scan and its plan in plain language. It is
off unless you run it, and the deterministic report and plan stay the authoritative answer.

## What it can and cannot do

- The model can only call **read-only tools** over a report that was already collected. It cannot reach your cluster, run commands, change the plan or open a pull request.
  The tools are: `list_nodes`, `probe_node` (the recorded evidence for one node, not a live probe), `scan_workloads`, `inspect_image`, `lookup_docs`, `generate_patch`
  (describes an advisory change) and `suggest_parameters`. A tool that would open a pull request exists only to refuse.
- Its answer is labeled **advisory and unverified**. Models make mistakes.

## Quick examples

Local model with Ollama, offline snapshot, nothing leaves your machine:

```console
$ cgroupguard agent explain --input snapshot.json \
      --provider openai-compatible --base-url http://localhost:11434/v1 --model qwen2.5:7b-instruct
```

Any OpenAI-compatible server (vLLM, LM Studio, a hosted gateway) works the same way. If it needs a key, name the environment variable that holds it:

```console
$ export MY_GATEWAY_KEY=...
$ cgroupguard agent explain --context my-cluster --provider openai-compatible \
      --base-url https://llm.example.com/v1 --model my-model --api-key-env MY_GATEWAY_KEY --audit-log audit.jsonl
```

Anthropic:

```console
$ export ANTHROPIC_API_KEY=...
$ cgroupguard agent explain --context my-cluster --provider anthropic --model MODEL_NAME --audit-log audit.jsonl
```

**Never pass keys as flags.** They would show up in shell history and process lists; the tool only accepts the *name* of an environment variable.

## What is sent to the model

Before anything is sent, the report is sanitized:

| Replaced by a pseudonym | Removed entirely |
|---|---|
| node, namespace, workload, pool and image names (for example `node-1`) | secrets, tokens and credentials, including ones embedded in URLs |

The model sees names such as `node-1`, `ns-2` and `wl-3`, not your real ones, and its answer is mapped back to the real names for you. Two flags opt out of the pseudonyms and **never** of the secret removal:

- `--reveal-names` sends real node, namespace and workload names.
- `--send-images` also sends full image references.

Use the pseudonyms unless you have a reason not to. Plain `http` is refused except to a local address unless you pass `--allow-insecure-http`.

## Limits and the audit log

| Flag | Default | Purpose |
|---|---|---|
| `--max-steps` | 8 | Most model calls in one run |
| `--max-tokens` | 60000 | Most tokens in one run |
| `--audit-log FILE` | none | Record every request, tool call and refusal in a **new** hash-chained file |

```console
$ cgroupguard agent verify-log audit.jsonl
```

`verify-log` detects any edited, inserted or removed entry. The log stores what was sent and received after sanitizing.

## Use the same tools from your own assistant (MCP)

```console
$ cgroupguard agent mcp --input snapshot.json
```

This serves the same read-only tools over the Model Context Protocol on standard input and output. For a client that launches MCP servers from a config file, the shape is
generally:

```json
{ "mcpServers": { "cgroupguard": { "command": "cgroupguard", "args": ["agent", "mcp", "--input", "/path/to/snapshot.json"] } } }
```

Check your client's documentation for the exact file and keys. The server only sees the report you point it at.

## When not to use it

Do not use the assistant to decide whether an upgrade is safe, to approve a migration step, or with data you may not share with the provider. For those, read the
[report](read-a-report.md) and the generated runbook.
