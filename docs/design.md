# Agents as Code (AAC) Design

## Core Concepts

**Agent** — a user-defined YAML template in `.tekton/agents/` describing what the agent does, which git events trigger it, and which tools it uses. The `system_prompt` field is the agent's core identity and behavioral instructions — it supports `{{ variable_name }}` template variables that get resolved with event and PR/issue context at AgentRun creation time. Agents select MCP servers from the Repository's catalog and can scope their tool access with an allowlist.

**AgentRun** — an execution instance spawned per git event. Contains a resolved snapshot of the Agent definition — template variables in the system prompt are substituted with actual values (PR diff, issue body, comment args, etc.) before the AgentRun is created. Includes enriched event context (PR details, comment body, labels, changed files). Every AgentRun executes in an isolated sandbox (K8s SIG Agent Sandbox). Tracks what the agent did (comments posted, commits pushed, PRs created) for audit.

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
    v
Agent Controller (match event to .tekton/agents/*.yaml definitions)
    |
    v
AgentRun creation (resolve template variables + instructions + tools + event context)
    |
    v
Sandbox creation (K8s SIG Agent Sandbox, from SandboxTemplate on Repository CR)
    |
    v
Agent executes in sandbox (LLM calls via runtime, produces structured result)
    |
    v
Controller reads result, executes git actions, updates AgentRun status
```

### Controller

The **Agent Controller** watches for git events via the webhook handler. On any matching event, it reads `.tekton/agents/*.yaml` from the repo, matches against event type, creates an AgentRun CR, provisions a sandbox, and manages the agent lifecycle.

## Custom Resources

### Repository CR

The Repository CR is the anchor. It owns the provider credentials, LLM settings, MCP server catalog, network policy, and runtime configuration. This follows PaC's pattern where the Repository CR holds provider-level configuration while the logic lives in git.

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

    runtime:
      sandbox_template: aac-default
      service_account_name: aac-agent
```

**Settings** configure policy and infrastructure that applies to all agents in this repo:

- **AI** — LLM provider, model, API keys, budget caps (`max_cost_per_run`, `max_tokens_per_run`, `max_timeout_seconds`), and model tuning (`model_config` with temperature, thinking, context window limits)
- **MCP Servers** — catalog of MCP servers agents can reference by name (container images or stdio commands, with env vars sourced from Secrets/ConfigMaps)
- **Network** — egress policy for agent sandboxes (`restricted`, `permissive`, or `air-gapped` preset with explicit egress allowlist)
- **Runtime** — sandbox execution configuration (`sandbox_template` name, service account for sandbox Pods)

### Agent Definitions (`.tekton/agents/`)

Agent definitions live in the repository under `.tekton/agents/`. They are version-controlled, reviewable in PRs, and scoped to the repo — not Kubernetes CRDs.

Agents declare a `system_prompt` — the agent's core identity and behavioral instructions describing what the agent does. The system prompt supports `{{ variable_name }}` template variables that are resolved with event metadata, PR/issue context, and provider API data at AgentRun creation time (see [Template Variables](#template-variables)).

Agents select tools from the Repository's MCP server catalog and can scope access with an allowlist. Instructions (additional context files) are referenced via annotations pointing to repo-relative paths or remote URLs.

```yaml
apiVersion: agent.tekton.dev/v1alpha1
kind: Agent
metadata:
  name: coding-agent
  annotations:
    agent.tekton.dev/on-event: "issue_comment"
    agent.tekton.dev/on-comment: "/assign|/implement"
spec:
  system_prompt: |
    You are a coding agent for {{ repo_owner }}/{{ repo_name }}.
    The user said: {{ trigger_comment_args }}

    The repo is cloned at {{ repo_clone_path }}.
    Implement the requested changes and create a pull request.
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
    Review PR #{{ pull_request_number }} ("{{ pull_request_title }}") for bugs,
    security issues, and style. {{ trigger_comment_args }}

    Here is the diff:
    {{ pull_request_diff }}
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

### Template Variables

Agent system prompts support `{{ variable_name }}` template variables that are resolved at AgentRun creation time. Variables use the PaC-style `{{ }}` syntax with flat names (no nesting, no CEL expressions).

**Resolution flow:**
1. Adapter extracts all `{{ variable_name }}` references from the agent's `system_prompt`
2. Metadata variables (from the event struct) are resolved immediately — these are free
3. Provider variables (requiring GitHub API calls) are fetched lazily — only if actually referenced in the template
4. Provider API results are cached per variable name to avoid duplicate calls
5. Unresolvable variables are replaced with an empty string and a warning is logged

**Variable categories:**

| Variable | Source | Description |
|----------|--------|-------------|
| `repo_url` | event | Repository HTML URL |
| `repo_name` | event | Repository name |
| `repo_owner` | event | Repository owner/organization |
| `default_branch` | event | Default branch name |
| `event_type` | event | Trigger type (`push`, `pull_request`, etc.) |
| `sender` | event | User who triggered the event |
| `sha` | event | Head commit SHA |
| `branch` | event | Target/base branch |
| `comment_body` | event | Full comment body (for comment events) |
| `repo_clone_path` | constant | Path where repo is cloned in sandbox (`/workspace/repo`) |
| `pull_request_number` | event | PR number |
| `pull_request_title` | event | PR title |
| `pull_request_author` | event | PR author login |
| `pull_request_url` | event | PR HTML URL |
| `pull_request_head_sha` | event | Head SHA of the PR branch |
| `issue_number` | event | Issue/PR number (alias for `pull_request_number`) |
| `issue_labels` | event/provider | Comma-separated label names |
| `trigger_comment_args` | event | Comment text after the matched `on-comment` pattern (e.g., `/review focus on security` → `focus on security`) |
| `pull_request_description` | provider API | PR body/description |
| `pull_request_diff` | provider API | Full PR diff |
| `pull_request_reviews` | provider API | PR reviews formatted as `@user (STATE): body` |
| `pull_request_comments` | provider API | PR/issue comments formatted as `@user: body` |
| `pull_request_files` | provider API | Changed file paths, one per line |
| `issue_title` | provider API | Issue title |
| `issue_body` | provider API | Issue body |
| `issue_comments` | provider API | Issue comments formatted as `@user: body` |

**Example — review agent with PR context:**
```yaml
spec:
  system_prompt: |
    Review PR #{{ pull_request_number }} by @{{ pull_request_author }}.
    Focus area: {{ trigger_comment_args }}

    ## Changed files
    {{ pull_request_files }}

    ## Diff
    {{ pull_request_diff }}

    ## Existing reviews
    {{ pull_request_reviews }}
```

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
  system_prompt: |
    You are a coding agent for org/my-repo.
    The user said: implement the new API endpoint

    The repo is cloned at /workspace/repo.
    Implement the requested changes and create a pull request.
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

- `system_prompt` is the agent's instructions with all `{{ variable_name }}` template variables resolved — contains the final prompt, not the raw template
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
                       │ adapter resolves templates + snapshots into AgentRun
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

KG-based context filtering is deferred. See [Deferred Decisions](deferred-decisions.md) for the full design and rationale.

## AgentRun Execution Model

### Sandbox Isolation

Every AgentRun executes in an isolated sandbox provisioned by the [K8s SIG Agent Sandbox](https://github.com/kubernetes-sigs/agent-sandbox). The sandbox is the security boundary — it provides process isolation, network policy enforcement, and resource limits. Isolation is never optional, regardless of whether the agent has MCP tools, a repo clone, or neither.

The controller creates sandboxes from a `SandboxTemplate` configured on the Repository CR (`settings.runtime.sandbox_template`). The template defines the container image, resource limits, and base configuration.

### Runtime Contract

The controller interacts with the sandbox exclusively through the agent-sandbox SDK:

1. **Write config** — `WriteFile(/etc/aac/config.json)` with resolved system prompt, instructions, model settings, MCP server endpoints, and limits
2. **Run agent** — `Run()` executes the runtime command inside the sandbox
3. **Read result** — `ReadFile(/output/result.json)` to collect structured actions the agent produced
4. **Execute actions** — controller posts comments, creates reviews, adds labels via the git provider API using Repository CR credentials
5. **Destroy** — sandbox is torn down after result collection

The runtime is whatever runs inside the sandbox — it could be a thin Go binary, Claude Code, or any agent framework. The controller doesn't care as long as it reads from `/etc/aac/config.json` and writes to `/output/result.json`.

### Repo Clone

The repo clone is opt-in via an annotation on the Agent definition:

```yaml
annotations:
  agent.tekton.dev/clone-repo: "true"
```

Agents that only need prompt context (labeler reading PR title, reviewer getting diff via `{{ pull_request_diff }}` template variables) skip the clone. Agents that need filesystem access (implementation, test fixing) opt in.

### What Agents Can Do

Agents produce structured results that the controller executes as git actions:
- **Post PR/issue comments** — via provider API
- **Submit PR reviews** — with per-file inline comments
- **Add/remove labels** — via provider API
- **Create PRs** — via provider API
- **Set status checks** — via provider API

Agents can also perform actions directly via MCP tools (e.g. GitHub MCP server with a token). The controller records all actions in `AgentRunStatus.Actions` for audit.

Agents cannot: modify Kubernetes resources, access other repos, or trigger non-git side effects.

### RBAC

- Agents inherit the Repository CR's RBAC policies
- The same approval rules that gate PipelineRuns also gate AgentRuns
- Changes to `.tekton/agents/` are governed by repo permissions (branch protection, CODEOWNERS)

## Key Design Decisions

1. **All triggers are git events** — no custom event sources, no direct agent-to-agent protocol
2. **Agent definitions live in `.tekton/agents/`** — version-controlled in the repo, not K8s CRDs
3. **AgentRun is a CRD** — the execution record, created by the controller
4. **`system_prompt` is the agent's identity** — natural language describing what the agent does
5. **Declarative tool selection** — MCP servers declared on Repository CR (infra), agents select by name (dev)
6. **Every agent runs in an isolated sandbox** — K8s SIG Agent Sandbox provides the security boundary; isolation is never optional
7. **Runtime contract** — controller writes config, runs agent, reads results via sandbox SDK; doesn't call LLM APIs itself
8. **RBAC inherited from Repository CR** — no separate agent-specific RBAC
9. **Agent results are git actions only** — comments, commits, PRs, status checks, labels
10. **LLM config on Repository CR, not on agents** — avoids secret sprawl, agents stay declarative
11. **Annotation-based triggers** — trigger matching uses `metadata.annotations` with PaC-style semantics, not structured spec fields
12. **Explicit instructions** — no auto-discovery of well-known files; developers opt-in via instruction annotations
13. **Template variables are flat and lazy** — `{{ variable_name }}` with no nesting or CEL; provider API calls only happen for variables actually referenced in the template

## Open Questions

### Cost Control
LLM API calls cost money. `max_cost_per_run` on the Repository CR defines a budget cap, but enforcement requires integration with LLM provider billing/usage APIs to track spend mid-run and terminate when exceeded.

### Observability
Where do AgentRun logs go? What metrics to track (duration, token usage, success rate)? OpenTelemetry integration for the agent path?

### Agent Chaining
Agent-to-agent chaining (e.g., reviewer agent posts a comment that triggers a coding agent) is a natural extension of the git-event-driven model. Key design questions around seed extraction from comments, loop prevention, and chain depth limits are deferred.

### Security Policy
Deferred for MVP. See [Deferred Decisions](deferred-decisions.md) for the full analysis of why git security policy needs more design work (MCP-mediated vs controller-mediated git operations).
