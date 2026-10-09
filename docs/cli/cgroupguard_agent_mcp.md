## cgroupguard agent mcp

Serve the read-only tools over MCP (stdio)

```
cgroupguard agent mcp [flags]
```

### Options

```
      --allow-insecure-http         Allow plain http to a non-loopback host
      --api-key-env string          Name of the environment variable holding the API key (never pass keys on the command line)
      --audit-log string            Write a hash-chained audit log to this new file
      --base-url string             Provider base URL (required for openai-compatible, for example http://localhost:11434/v1)
      --context string              Kubeconfig context
  -h, --help                        help for mcp
      --input string                Offline snapshot JSON (schemaVersion 1)
      --kubeconfig string           Kubeconfig path; defaults to standard client-go loading
      --kubelet-metrics             Read kubelet_cgroup_version through the API-server node proxy (needs nodes/proxy; opt-in)
      --local                       Inspect this Linux mount namespace; no cluster connection
      --max-steps int               Maximum model calls (default 8)
      --max-tokens int              Maximum total tokens for the run (default 60000)
      --model string                Model name
      --namespace string            Existing namespace for probe pods
      --node-events                 Also read kubelet CgroupV1 Warning Events (needs list on events; absence is never evidence of v2)
      --nodes-only                  Skip the live workload inventory and read node information only
      --path string                 Cgroup root for --local (default "/sys/fs/cgroup")
      --probe                       Explicitly authorize temporary hostPath probe pod creation/deletion
      --probe-image string          Trusted cgroupguard image pinned by digest
      --prometheus-url string       Read kubelet_cgroup_version from an existing Prometheus (http/https URL)
      --provider string             Model provider: anthropic or openai-compatible (Ollama, vLLM and others)
      --reveal-names                Send real node, namespace and workload names instead of pseudonyms (secrets are still removed)
      --send-images                 Also send full image references (secrets are still removed)
  -t, --target-version string       Planned Kubernetes upgrade target; scales finding severity (for example 1.35) (default "1.35")
      --timeout duration            Overall live scan timeout (default 5m0s)
      --workload-namespace string   Only scan workloads in this namespace (default: all)
```

### SEE ALSO

* [cgroupguard agent](cgroupguard_agent.md)	 - Optional read-only assistant: explanations and an MCP server (never changes anything)

