# Repository CR Configuration

The Repository CR is the anchor resource for AAC. It owns the git provider credentials, LLM settings, MCP server catalog, network policy, and runtime configuration. The infra team manages this resource; developers manage Agent definitions in the repo.

---

## Git Provider

Configure how AAC connects to the git provider (GitHub):

```yaml
apiVersion: agent.tekton.dev/v1alpha1
kind: Repository
metadata:
  name: my-repo
  namespace: my-ns
spec:
  url: "https://github.com/org/my-repo"
  git_provider:
    secret:
      name: github-token
      key: token
    webhook_secret:
      name: webhook-secret
      key: webhook.secret
```

| Field | Description |
|-------|-------------|
| `url` | Repository URL |
| `git_provider.secret` | Kubernetes Secret reference for the git provider token (PAT or GitHub App) |
| `git_provider.webhook_secret` | Kubernetes Secret reference for the webhook signature verification secret |

---

## AI Settings

Configure the LLM provider, model, and budget caps that apply to all agents in this repo:

```yaml
settings:
  ai:
    enabled: true
    provider: anthropic
    secret_ref:
      name: ai-api-key
      key: api-key
    model: claude-sonnet-4-20250514
    base_url: ""
    max_cost_per_run: "1.00"
    max_tokens_per_run: 200000
    max_timeout_seconds: 600
    model_config:
      temperature: "0.2"
      max_output_tokens: 8192
      max_context_tokens: 128000
      thinking:
        enabled: true
        budget_tokens: 10000
      parameters:
        top_p: "0.95"
```

| Field | Description |
|-------|-------------|
| `enabled` | Whether AI agents are active |
| `provider` | LLM provider name (e.g. `anthropic`, `gemini`) |
| `secret_ref` | Kubernetes Secret reference for the LLM API key |
| `model` | Model identifier |
| `base_url` | Custom API endpoint (optional, for self-hosted or enterprise) |
| `max_cost_per_run` | USD budget cap per AgentRun (string) |
| `max_tokens_per_run` | Maximum token budget — agents can set lower, not higher |
| `max_timeout_seconds` | Maximum execution time — agents can set lower, not higher |

### Model Config

| Field | Description |
|-------|-------------|
| `temperature` | Controls randomness (string; `"0.0"` = deterministic, `"1.0"` = creative) |
| `max_output_tokens` | Maximum tokens per model response |
| `max_context_tokens` | Total context window limit |
| `thinking.enabled` | Enable extended thinking / chain-of-thought |
| `thinking.budget_tokens` | Max tokens for thinking (maps to Claude's `budget_tokens`) |
| `parameters` | Provider-specific key-value overrides (e.g. `top_p`, `frequency_penalty`) |

---

## MCP Server Catalog

Declare which MCP servers are available for agents to use. Agents reference these by name in their `tools.mcp_servers` field. MCP servers run as subprocesses inside the sandbox using stdio transport.

For npm-based servers, use `npx -y` to auto-install and run on the fly — no pre-installation needed (Node.js is included in the runtime image).

```yaml
settings:
  mcp_servers:
    - name: github
      command: ["npx", "-y", "@modelcontextprotocol/server-github"]
      env:
        - name: GITHUB_TOKEN
          value_from:
            secret_key_ref:
              name: github-token
              key: token

    - name: filesystem
      command: ["npx", "-y", "@modelcontextprotocol/server-filesystem"]
      args: ["/workspace"]

    - name: linear
      command: ["npx", "-y", "@anthropic-ai/linear-mcp-server"]
      env:
        - name: LINEAR_API_KEY
          value_from:
            secret_key_ref:
              name: linear-token
              key: api-key

    - name: custom-python
      command: ["python", "/scripts/my-server.py"]
      args: ["--verbose"]
```

| Field | Description |
|-------|-------------|
| `name` | Identifier agents reference |
| `command` | Command to launch the MCP server as a subprocess (stdio transport). Required. |
| `args` | Additional arguments appended after the command |
| `env` | Environment variables — values can come from Secrets or ConfigMaps |

### Environment Variable Sources

```yaml
env:
  - name: MY_VAR
    value: "literal-value"
  - name: SECRET_VAR
    value_from:
      secret_key_ref:
        name: my-secret
        key: api-key
  - name: CONFIG_VAR
    value_from:
      config_map_key_ref:
        name: my-config
        key: setting
```

---

## Network Policy

Control egress from agent sandboxes:

```yaml
settings:
  network:
    preset: restricted
    egress:
      - host: "api.github.com"
        ports: [443]
      - host: "api.anthropic.com"
        ports: [443]
```

| Field | Description |
|-------|-------------|
| `preset` | Base policy: `restricted` (deny all except allowed), `permissive` (allow all, default), `air-gapped` (no network) |
| `egress` | Explicit outbound rules (used with `restricted`) |

### Egress Rules

| Field | Description |
|-------|-------------|
| `host` | Hostname to allow |
| `cidr` | CIDR block to allow (alternative to host) |
| `ports` | Allowed ports |

---

## Runtime Config

Configure sandbox execution:

```yaml
settings:
  runtime:
    sandbox_template: aac-default
    service_account_name: aac-agent
```

| Field | Description |
|-------|-------------|
| `sandbox_template` | Name of the SandboxTemplate CR for agent execution environments |
| `service_account_name` | Kubernetes service account for sandbox pods |

---

## Complete Example

```yaml
apiVersion: agent.tekton.dev/v1alpha1
kind: Repository
metadata:
  name: my-repo
  namespace: my-ns
spec:
  url: "https://github.com/org/my-repo"
  git_provider:
    secret:
      name: github-token
      key: token
    webhook_secret:
      name: webhook-secret
      key: webhook.secret

  settings:
    ai:
      enabled: true
      provider: anthropic
      secret_ref:
        name: ai-api-key
        key: api-key
      model: claude-sonnet-4-20250514
      max_cost_per_run: "1.00"
      max_tokens_per_run: 200000
      max_timeout_seconds: 600
      model_config:
        temperature: "0.2"
        max_output_tokens: 8192
        max_context_tokens: 128000
        thinking:
          enabled: true
          budget_tokens: 10000

    mcp_servers:
      - name: github
        command: ["npx", "-y", "@modelcontextprotocol/server-github"]
        env:
          - name: GITHUB_TOKEN
            value_from:
              secret_key_ref:
                name: github-token
                key: token
      - name: linear
        command: ["npx", "-y", "@anthropic-ai/linear-mcp-server"]
        env:
          - name: LINEAR_API_KEY
            value_from:
              secret_key_ref:
                name: linear-token
                key: api-key

    network:
      preset: restricted
      egress:
        - host: "api.github.com"
          ports: [443]
        - host: "api.anthropic.com"
          ports: [443]

    runtime:
      sandbox_template: aac-default
      service_account_name: aac-agent
```
