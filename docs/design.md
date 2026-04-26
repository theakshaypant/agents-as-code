# Yeet Design

## Core Concepts

**Knowledge Graph (KG)** — a persistent, incrementally-updated graph of a repository's code structure and relationships, stored on a PV. Built using graphify (tree-sitter for AST extraction, LLM for semantic extraction, Leiden/Louvain for community detection). Configured as a field on the Repository CR, updated incrementally on each push.

**Agent** — a user-defined YAML template in `.tekton/agents/` describing what the agent does and which git events trigger it. The `purpose` field is natural language — yeet infers the right KG filtering strategy from it. No prompts, no context flags — just declare intent.

**AgentRun** — an execution instance spawned per git event, receiving a filtered KG subgraph. The execution record, analogous to PipelineRun. Tracks what the agent did (comments posted, commits pushed, PRs created) for audit.

All triggers are git events. Agents interact with the outside world exclusively through git primitives (PR comments, commits, status checks, labels), which re-enter yeet as new events — enabling agent chaining without special inter-agent protocols.

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
          Context Filter (seed extraction -> strategy -> traversal -> truncation)
              |
              v
          AgentRun creation (sandbox with filtered KG + git creds)
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

2. **Agent Controller** — on any matching git event, reads `.tekton/agents/*.yaml` from the repo, matches against event type, filters KG context, creates AgentRun CR, spawns execution.

Both controllers share the webhook handler but have separate control loops.

## Custom Resources

### Repository CR

The Repository CR is the anchor. It owns the KG configuration, provider credentials, and LLM settings. This follows PaC's pattern where the Repository CR holds provider-level configuration while the logic lives in git.

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
      provider: gemini
      secret_ref:
        name: ai-api-key
        key: api-key

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

- Each branch entry has its own `scope` — different branches can graph different directories
- If `scope` is omitted, graphify runs on the entire repo with its built-in skip list
- Each branch gets its own PV; `storageClassName` is shared at top level
- LLM provider configuration lives on the Repository CR, not on individual agents — keeps agent definitions simple and avoids secret sprawl

### Agent Definitions (`.tekton/agents/`)

Agent definitions live in the repository under `.tekton/agents/`. They are version-controlled, reviewable in PRs, and scoped to the repo — not Kubernetes CRDs.

Agents declare a `purpose` instead of a prompt. The purpose is natural language describing what the agent does. Yeet infers the KG filtering strategy from it — no manual context configuration needed.

```yaml
apiVersion: agent.tekton.dev/v1alpha1
kind: Agent
metadata:
  name: coding-agent
spec:
  purpose: "Implement code changes for assigned issues"

  triggers:
    - event: issue_comment
      match: "/assign"
    - event: issue_comment
      match: "/implement"

  limits:
    maxTokens: 100000
    timeoutSeconds: 600
```

```yaml
apiVersion: agent.tekton.dev/v1alpha1
kind: Agent
metadata:
  name: review-agent
spec:
  purpose: "Review pull requests for bugs, security issues, and style"
  triggers:
    - event: pull_request
      branches:
        - main
    - event: issue_comment
      match: "/review"
  limits:
    maxTokens: 50000
    timeoutSeconds: 300
```

**Trigger fields:**

| Field | Description |
|-------|-------------|
| `event` | Required. One of: `push`, `pull_request`, `pull_request_review`, `issue_comment`, `issues_labeled`, `pull_request_labeled`. |
| `branches` | Optional. Glob patterns for target branch filtering. |
| `match` | Optional. For `issue_comment`: matches if the comment body contains this string. For `issues_labeled` / `pull_request_labeled`: matches if the label name equals this string exactly. |

**Optional context override** — for users who want explicit control over KG filtering instead of relying on purpose-based inference:

```yaml
spec:
  purpose: "Review pull requests for security vulnerabilities"
  context:
    strategy: dfs              # bfs | dfs | community | impact
    depth: 4                   # max hops from seed nodes
    tokenBudget: 8000          # cap on context size
    nodeFilter:
      - function
      - interface
      - type
    edgeFilter:
      - calls
      - implements
      - depends_on
```

Partial overrides work — specifying only `depth: 4` keeps the inferred strategy but increases traversal depth.

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
spec:
  agentRef: coding-agent
  event:
    type: issue_comment
    sha: abc123f
    branch: main
    sender: octocat
    url: https://github.com/org/repo/issues/42#issuecomment-123
  context:
    subgraphRef: /data/kg/agentrun-f7a3b-context.json
    tokenCount: 3200
    seedNodes:
      - "pkg/provider/gitlab/status.go"
      - "CreateStatus"
    strategy: bfs
    depth: 2

status:
  conditions:
    - type: Succeeded
      status: "True"
  startTime: "2026-04-23T10:31:00Z"
  completionTime: "2026-04-23T10:33:45Z"
  tokensUsed: 28400
  actions:
    - type: pr-comment
      url: https://github.com/org/repo/pull/43#issuecomment-456
    - type: commit
      sha: def456a
```

- `context.subgraphRef` points to the filtered KG subgraph
- `status.actions` tracks what the agent did for audit
- Immutable after completion
- Lifecycle: `Created -> Pending (waiting for KG filter) -> Running -> Succeeded | Failed`

## Context Filtering

Given a KG with thousands of nodes and a specific git event, produce a small, relevant subgraph that gives the agent what it needs without overwhelming its context window.

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
Strategy Selection (inferred from Agent purpose + event type)
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
    |  - Truncate to fit within Agent's maxTokens budget
    |
    v
Filtered Subgraph -> provided to AgentRun
```

### Preset Profiles

The Agent's `purpose` field is mapped to a filtering profile. Users don't configure these directly.

| Profile | Purpose keywords | Event types | Strategy | Depth | Focus |
|---|---|---|---|---|---|
| `implement` | implement, code, build, create | issue, issue_comment | keyword -> community -> code nodes | 3 | broad understanding, relevant packages |
| `review` | review, check, audit, verify | pull_request | changed files -> DFS outward | 3 | what could break, interface contracts |
| `triage` | triage, categorize, prioritize | issues | keyword -> package-level summary | 1 | architecture, ownership, not code details |
| `fix` | fix, debug, bug, error | issue (with bug label), issue_comment | stack trace / error -> callers/callees | 2 | narrow, deep, focused on the broken path |

Default to `implement` (broadest context) if purpose doesn't clearly match a profile.

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

- Filtered KG subgraph (read-only)
- Git credentials for the target repo (from Repository CR's secret)
- Network access to LLM provider API
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
5. **`purpose` drives filtering** — users describe intent in natural language, system infers strategy
6. **Per-branch KG with per-branch PV** — different branches can have different scopes and storage
7. **RBAC inherited from Repository CR** — no separate agent-specific RBAC
8. **Agent results are git actions only** — comments, commits, PRs, status checks, labels
9. **Graphify as the KG engine** — accept current limitations, iterate later
10. **LLM config on Repository CR, not on agents** — avoids secret sprawl, agents stay declarative

## Open Questions

### Graphify Quality
Graphify uses tree-sitter for AST extraction, which gives broad language support but is less accurate than language-specific tooling. Known weaknesses on Go repos: node deduplication issues, fragmented communities, low cohesion scores. The context filtering pipeline must be resilient to noisy graphs.

### Cost Control
LLM API calls cost money. Need to surface cost tracking per Agent, per AgentRun, per Repository. Consider namespace-level or cluster-level budget caps.

### Observability
Where do AgentRun logs go? What metrics to track (duration, token usage, success rate, KG query latency)? OpenTelemetry integration for the agent path?

### Agent Chaining
Agent-to-agent chaining (e.g., reviewer agent posts a comment that triggers a coding agent) is a natural extension of the git-event-driven model. Key design questions around seed extraction from comments, loop prevention, and chain depth limits are deferred.

### Execution Backend
The concrete runtime for AgentRuns — pods, external runners, or something else — needs to be determined. Considerations include isolation guarantees, startup latency, resource efficiency, and whether agents need filesystem access to the repo clone.
