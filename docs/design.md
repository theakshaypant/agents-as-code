# Agents as Code (AAC) Design

## Core Concepts

**Knowledge Graph (KG)** — a persistent, incrementally-updated graph of a repository's code structure and relationships, stored on a PV. Built using graphify (tree-sitter for AST extraction, LLM for semantic extraction, Leiden/Louvain for community detection). Configured as a field on the Repository CR, updated incrementally on each push.

**Agent** — a user-defined YAML template in `.tekton/agents/` describing what the agent does, which git events trigger it, and which tools it uses. The `system_prompt` field is the agent's core identity and behavioral instructions. Agents select MCP servers from the Repository's catalog and can scope their tool access with an allowlist.

**AgentRun** — an execution instance spawned per git event. Contains a resolved snapshot of the Agent definition (system prompt, instructions, tools) plus enriched event context (PR details, comment body, labels, changed files). Tracks what the agent did (comments posted, commits pushed, PRs created) for audit.

All triggers are git events. Agents interact with the outside world exclusively through git primitives (PR comments, commits, status checks, labels), which re-enter AAC as new events — enabling agent chaining without special inter-agent protocols.

## Architecture

### Event Flow

```
GitHub Webhook (push | pull_request | pull_request_review | issue_comment | issues | pull_request labeled)
    |
    v
Webhook Handler (validates signature, parses payload)
    |
    v
Repository CR lookup (match webhook repo URL to CR)
    |
    +---> KG Controller (if push event + branch tracked by knowledge_graph)
    |         |
    |         v
    |     Incremental KG update via graphify --update
    |
    +---> Agent Controller (match event to .tekton/agents/*.yaml definitions)
              |
              v
          AgentRun creation (sandbox with resolved instructions + tools + event context)
              |
              v
          Agent executes (LLM calls, produces git actions)
              |
              v
          Status update on AgentRun CR
```

### Controllers

Two independent controllers, both triggered by git events:

1. **KG Controller** — watches Repository CRs with `knowledge_graph.enabled: true`. On push events to tracked branches, runs graphify incrementally. Manages PV lifecycle.

2. **Agent Controller** — on any matching git event, reads `.tekton/agents/*.yaml` from the repo, matches against event type, creates AgentRun CR, spawns execution.

Both controllers share the webhook handler but have separate control loops.

## Custom Resources

### Repository CR

The Repository CR is the anchor. It owns the KG configuration, provider credentials, LLM settings, MCP server catalog, and network policy. This follows PaC's pattern where the Repository CR holds provider-level configuration while the logic lives in git.

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
        image: ghcr.io/modelcontextprotocol/github:latest
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

  knowledge_graph:
    enabled: true
    storageClassName: gp3
    branches:
      - name: main
        storage: 1Gi
        scope:
          paths:
            - pkg/
            - cmd/
            - docs/
          ignore:
            - vendor/
            - "**/*_test.go"
            - "*.pb.go"
            - pkg/generated/
      - name: "release-*"
        storage: 512Mi
        scope:
          paths:
            - pkg/
            - cmd/

status:
  knowledge_graph:
    branches:
      - name: main
        conditions:
          - type: Ready
            status: "True"
        lastUpdated: "2026-04-23T10:30:00Z"
        lastCommitSHA: abc123f
        nodeCount: 2400
        edgeCount: 3800
        communityCount: 15
```

**Settings** configure policy and infrastructure that applies to all agents in this repo:

- **AI** — LLM provider, model, API keys, budget caps (`max_cost_per_run`, `max_tokens_per_run`, `max_timeout_seconds`), and model tuning (`model_config` with temperature, thinking, context window limits)
- **MCP Servers** — catalog of MCP servers agents can reference by name (container images or stdio commands, with env vars sourced from Secrets/ConfigMaps)
- **Network** — egress policy for agent sandboxes (`restricted`, `permissive`, or `air-gapped` preset with explicit egress allowlist)

Each branch entry has its own `scope` — different branches can graph different directories. If `scope` is omitted, graphify runs on the entire repo with its built-in skip list. Each branch gets its own PV; `storageClassName` is shared at top level.

### Agent Definitions (`.tekton/agents/`)

Agent definitions live in the repository under `.tekton/agents/`. They are version-controlled, reviewable in PRs, and scoped to the repo — not Kubernetes CRDs.

Agents declare a `system_prompt` — the agent's core identity and behavioral instructions describing what the agent does. Agents select tools from the Repository's MCP server catalog and can scope access with an allowlist. Instructions (additional context files) are referenced via annotations pointing to repo-relative paths or remote URLs.

```yaml
apiVersion: agent.tekton.dev/v1alpha1
kind: Agent
metadata:
  name: coding-agent
  annotations:
    agent.tekton.dev/on-event: "issue_comment"
    agent.tekton.dev/on-comment: "/assign|/implement"
spec:
  system_prompt: "Implement code changes for assigned issues"
  tools:
    mcp_servers:
      - github
    allowed:
      - "github:get_file_contents"
      - "github:create_or_update_file"
  limits:
    max_tokens: 100000
    timeout_seconds: 600
```

```yaml
apiVersion: agent.tekton.dev/v1alpha1
kind: Agent
metadata:
  name: review-agent
  annotations:
    agent.tekton.dev/on-event: "[pull_request, issue_comment]"
    agent.tekton.dev/on-target-branch: "main"
    agent.tekton.dev/on-comment: "/review"
    agent.tekton.dev/instruction: ".tekton/agents/review-standards.md"
spec:
  system_prompt: |
    Review pull requests for bugs, security issues, and style.
    Provide actionable feedback as a PR review.
  tools:
    mcp_servers:
      - github
    allowed:
      - "github:get_file_contents"
      - "github:create_pull_request_review"
  limits:
    max_tokens: 50000
    timeout_seconds: 300
```

**Trigger annotations:**

Triggers are declared as annotations on Agent metadata using the `agent.tekton.dev/` prefix, following PaC's annotation-based matching model.

| Annotation | Description | Default (absent) |
|------------|-------------|------------------|
| `on-event` | Required. Event types: `push`, `pull_request`, `pull_request_review`, `issue_comment`, `issues_labeled`, `pull_request_labeled`. |  — |
| `on-target-branch` | Glob patterns for target branch filtering. | Match all |
| `on-comment` | Regex match on comment body. Checked as a separate matching track — if present and matched, the agent is selected immediately. | Pass |
| `on-path-change` | Glob match on changed files. | Pass |
| `on-label` | Label match for `issues_labeled` / `pull_request_labeled` events. Label events without this annotation do not match. | Pass |

**Value format:** single value (`"push"`) or bracket array (`"[push, pull_request]"`).

**Matching semantics:** `on-comment` is a separate matching track (checked first, bypasses other annotations when matched). For the standard path, all present annotations are AND'd — all must match. Within each annotation, array values are OR'd.

**Instruction annotations:**

Instructions are loaded via PAC-style annotations. Developers explicitly opt-in to whatever instruction files they want — no auto-discovery of well-known files.

| Annotation | Description |
|------------|-------------|
| `instruction` | Primary instruction file (repo-relative path or HTTP(S) URL) |
| `instruction-1`, `instruction-2`, ... | Additional instruction files |

Supported sources:
- Repo-relative paths (resolved from the repo's default branch)
- Remote HTTP(S) URLs (fetched at AgentRun creation time)

### AgentRun CR

Created automatically by the Agent Controller when a matching git event arrives. Not authored by users.

```yaml
apiVersion: agent.tekton.dev/v1alpha1
kind: AgentRun
metadata:
  name: coding-agent-run-f7a3b
  namespace: my-ns
  labels:
    agent.tekton.dev/agent: coding-agent
    agent.tekton.dev/repository: my-repo
  annotations:
    agent.tekton.dev/on-event: "issue_comment"
    agent.tekton.dev/on-comment: "/assign|/implement"
spec:
  agent_ref: coding-agent
  repository_ref: my-repo
  system_prompt: "Implement code changes for assigned issues"
  instructions:
    - name: CLAUDE.md
      path: CLAUDE.md
  tools:
    mcp_servers:
      - github
    allowed:
      - "github:get_file_contents"
      - "github:create_or_update_file"
  event:
    type: issue_comment
    action: created
    sha: abc123f
    branch: main
    sender: octocat
    url: https://github.com/org/repo/issues/42#issuecomment-123
    comment:
      body: "/assign implement the new API endpoint"
    pull_request:
      number: 42
      title: "Add user API endpoint"
      head_branch: feature/user-api
      base_branch: main
    labels:
      - enhancement
    changed_files:
      - pkg/api/user.go
      - pkg/api/user_test.go
  limits:
    max_tokens: 100000
    timeout_seconds: 600

status:
  conditions:
    - type: Succeeded
      status: "True"
  start_time: "2026-04-23T10:31:00Z"
  completion_time: "2026-04-23T10:33:45Z"
  tokens_used: 28400
  cost_usd: "0.42"
  sandbox_name: coding-agent-run-f7a3b-sandbox
  exec_id: exec-abc123
  actions:
    - type: pr-comment
      url: https://github.com/org/repo/pull/43#issuecomment-456
    - type: commit
      sha: def456a
```

- `system_prompt` is the agent's instructions, copied from the Agent definition
- `instructions` references resolved instruction files (repo paths or remote URLs)
- `tools` is the resolved MCP server selection and tool allowlist
- `event` contains enriched event context (PR details, comment body, labels, changed files)
- `limits` are resolved as `min(agent.limits, repo.settings.ai.max*)`
- `status.actions` tracks what the agent did for audit
- `status.cost_usd` tracks the cost of the run
- Immutable after completion
- Lifecycle: `Created -> Pending -> Running -> Succeeded | Failed`

## Trust Model

The API has a clear separation of concerns:

```
┌─────────────────────────────────────────────────────────┐
│  Repository CR (infra team)                             │
│                                                         │
│  - AI provider, model, API keys, model config           │
│  - Budget caps (max_cost_per_run, max_tokens_per_run)   │
│  - MCP server catalog (images, credentials)             │
│  - Network policy (preset + egress rules)               │
│                                                         │
│  "What agents are ALLOWED to do"                        │
└──────────────────────┬──────────────────────────────────┘
                       │ controller reads policy at reconcile time
                       │
┌──────────────────────▼──────────────────────────────────┐
│  Agent Definition (developer, in-repo YAML)             │
│                                                         │
│  - System prompt                                        │
│  - Instructions (repo files + remote URLs, explicit)    │
│  - Tool selection (from repo catalog) + allowlist       │
│  - Limits (bounded by repo maximums)                    │
│  - Trigger annotations (events, branches, patterns)     │
│                                                         │
│  "What the agent SHOULD do"                             │
└──────────────────────┬──────────────────────────────────┘
                       │ adapter snapshots into AgentRun
                       │
┌──────────────────────▼──────────────────────────────────┐
│  AgentRun CR (runtime)                                  │
│                                                         │
│  - Resolved snapshot of Agent config                    │
│  - Event context (enriched with PR/comment/files)       │
│  - Resolved limits (min of agent + repo caps)           │
│  - NO policy fields — controller reads from Repo CR     │
│                                                         │
│  "What actually happened"                               │
└─────────────────────────────────────────────────────────┘
```

## Context Filtering (deferred)

KG-based context filtering is designed but not yet implemented. When implemented, given a KG with thousands of nodes and a specific git event, it will produce a small, relevant subgraph that gives the agent what it needs without overwhelming its context window.

### Pipeline

```
Git Event
    |
    v
Seed Extraction (event-type-specific)
    |  - push/PR: changed files -> map to KG nodes
    |  - issue/comment: text -> keyword match against KG node labels
    |
    v
Strategy Selection (inferred from Agent system_prompt + event type)
    |
    v
Graph Traversal (from seed nodes using selected strategy)
    |  - BFS with depth limit for broad context
    |  - DFS for dependency/impact chains
    |  - Community expansion for architectural context
    |
    v
Token Budget Truncation
    |  - Rank nodes by relevance (distance from seed, edge confidence, node degree)
    |  - Truncate to fit within Agent's max_tokens budget
    |
    v
Filtered Subgraph -> provided to AgentRun
```

### Preset Profiles

The Agent's `system_prompt` will be mapped to a filtering profile. Users won't configure these directly.

| Profile | Keywords | Event types | Strategy | Depth | Focus |
|---|---|---|---|---|---|
| `implement` | implement, code, build, create | issue, issue_comment | keyword -> community -> code nodes | 3 | broad understanding, relevant packages |
| `review` | review, check, audit, verify | pull_request | changed files -> DFS outward | 3 | what could break, interface contracts |
| `triage` | triage, categorize, prioritize | issues | keyword -> package-level summary | 1 | architecture, ownership, not code details |
| `fix` | fix, debug, bug, error | issue (with bug label), issue_comment | stack trace / error -> callers/callees | 2 | narrow, deep, focused on the broken path |

Default to `implement` (broadest context) if system_prompt doesn't clearly match a profile.

## Knowledge Graph Lifecycle

### Creation

When a Repository CR with `knowledge_graph.enabled: true` is created:

1. KG controller provisions a PV per branch using the specified StorageClass
2. Clones the repo at the specified branch(es)
3. Runs graphify on the configured `scope.paths`, respecting `scope.ignore`
4. Graphify performs AST extraction (tree-sitter, zero LLM cost) for code and semantic extraction (LLM-powered) for docs
5. Stores the graph (JSON + metadata) on the PV
6. Updates `status.knowledge_graph` on the Repository CR

### Incremental Updates

On push events to tracked branches:

1. KG controller determines changed files from push payload
2. Runs graphify with `--update` — re-extracts only changed files
3. Graphify merges updated nodes/edges and re-clusters
4. Updates status on Repository CR

### Graph Contents

**Nodes**: functions, types, interfaces, packages, concepts (from docs/comments)

**Edges**:
- Structural: `contains`, `imports`, `implements`, `method_of`
- Behavioral: `calls`, `tests`, `depends_on`
- Semantic: `related_to`, `rationale_for`

**Node metadata**: `package`, `architectural_layer`, `source_file`, `source_location`

**Communities**: clusters of tightly-related nodes (Leiden/Louvain), power "give me context about subsystem X" queries.

### Storage

- Graph data on PV as JSON + metadata files
- PV mounted read-write by KG controller, read-only by AgentRun executions
- StorageClass is user-configurable — supports any CSI driver the cluster offers
- Typical graph for a large repo: 2-5 MB

## AgentRun Execution Model

### Execution Environment

Each AgentRun executes in an isolated environment with:

- Resolved system prompt and instruction files
- MCP servers from the Repository catalog (configured tools only)
- Git credentials for the target repo (from Repository CR's secret)
- Network access governed by the Repository's network policy
- No access to the Kubernetes API server

The concrete execution backend (pods, external runners, etc.) is TBD.

### What Agents Can Do

Agents interact exclusively through git actions:
- **Post PR/issue comments** — via GitHub API
- **Push commits** — via git credentials
- **Set status checks** — via GitHub API
- **Create PRs** — via GitHub API
- **Add labels** — via GitHub API

Agents cannot: modify Kubernetes resources, access other repos, trigger non-git side effects.

All results are git actions, so visibility is governed by existing VCS permissions — no separate access control needed.

### RBAC

- Agents inherit the Repository CR's RBAC policies
- The same approval rules that gate PipelineRuns also gate AgentRuns
- Changes to `.tekton/agents/` are governed by repo permissions (branch protection, CODEOWNERS)

## Key Design Decisions

1. **All triggers are git events** — no custom event sources, no direct agent-to-agent protocol
2. **KG config lives on Repository CR** — not a separate CRD
3. **Agent definitions live in `.tekton/agents/`** — version-controlled in the repo, not K8s CRDs
4. **AgentRun is a CRD** — the execution record, created by the controller
5. **`system_prompt` is the agent's identity** — natural language describing what the agent does
6. **Declarative tool selection** — MCP servers declared on Repository CR (infra), agents select by name (dev)
7. **Per-branch KG with per-branch PV** — different branches can have different scopes and storage
8. **RBAC inherited from Repository CR** — no separate agent-specific RBAC
9. **Agent results are git actions only** — comments, commits, PRs, status checks, labels
10. **Graphify as the KG engine** — accept current limitations, iterate later
11. **LLM config on Repository CR, not on agents** — avoids secret sprawl, agents stay declarative
12. **Annotation-based triggers** — trigger matching uses `metadata.annotations` with PaC-style semantics, not structured spec fields
13. **Explicit instructions** — no auto-discovery of well-known files; developers opt-in via instruction annotations

## Open Questions

### Graphify Quality
Graphify uses tree-sitter for AST extraction, which gives broad language support but is less accurate than language-specific tooling. Known weaknesses on Go repos: node deduplication issues, fragmented communities, low cohesion scores. The context filtering pipeline must be resilient to noisy graphs.

### Cost Control
LLM API calls cost money. `max_cost_per_run` on the Repository CR defines a budget cap, but enforcement requires integration with LLM provider billing/usage APIs to track spend mid-run and terminate when exceeded.

### Observability
Where do AgentRun logs go? What metrics to track (duration, token usage, success rate, KG query latency)? OpenTelemetry integration for the agent path?

### Agent Chaining
Agent-to-agent chaining (e.g., reviewer agent posts a comment that triggers a coding agent) is a natural extension of the git-event-driven model. Key design questions around seed extraction from comments, loop prevention, and chain depth limits are deferred.

### Execution Backend
The concrete runtime for AgentRuns — pods, external runners, or something else — needs to be determined. Considerations include isolation guarantees, startup latency, resource efficiency, and whether agents need filesystem access to the repo clone.

### Security Policy
Deferred for MVP. See [Deferred Decisions](deferred-decisions.md) for the full analysis of why git security policy needs more design work (MCP-mediated vs controller-mediated git operations).
