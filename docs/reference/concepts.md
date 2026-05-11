# Architecture & Concepts

## Core Concepts

**Agent** — A user-defined YAML template in `.tekton/agents/` describing what the agent does, which git events trigger it, and which tools it uses. The `system_prompt` field supports `{{ variable_name }}` template variables that get resolved with event and PR/issue context at AgentRun creation time. Agents select MCP servers from the Repository CR's catalog and can scope their tool access with an allowlist. See [Writing Agents](../guides/writing-agents.md).

**AgentRun** — An execution instance spawned per git event. Contains a resolved snapshot of the Agent definition — template variables are substituted with actual values (PR diff, issue body, comment args, etc.) before the AgentRun is created. Includes enriched event context (PR details, comment body, labels, changed files). Every AgentRun executes in an isolated sandbox. Tracks what the agent did (comments posted, commits pushed, PRs created) for audit.

**Repository CR** — The Kubernetes anchor resource. Owns provider credentials, LLM settings, MCP server catalog, network policy, and runtime configuration. The infra team manages this; developers manage Agent definitions. See [Configuration](configuration.md).

All triggers are git events. Agents interact with the outside world exclusively through git primitives (PR comments, commits, status checks, labels), which re-enter AAC as new events — enabling agent chaining without special inter-agent protocols.

---

## Event Flow

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
Sandbox creation (isolated execution environment)
    |
    v
Agent executes in sandbox (LLM calls via runtime, produces structured result)
    |
    v
Controller reads result, executes git actions, updates AgentRun status
```

The **Agent Controller** watches for git events via the webhook handler. On any matching event, it reads `.tekton/agents/*.yaml` from the repo, matches against event type, creates an AgentRun CR, provisions a sandbox, and manages the agent lifecycle.

---

## Trust Model

The API has a clear separation of concerns between what agents are **allowed** to do (infra team) and what agents **should** do (developers):

```
┌─────────────────────────────────────────────────────────┐
│  Repository CR (infra team)                             │
│                                                         │
│  - AI provider, model, API keys, model config           │
│  - Budget caps (max_cost_per_run, max_tokens_per_run)   │
│  - MCP server catalog (commands, credentials)            │
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

---

## Execution Model

### Sandbox Isolation

Every AgentRun executes in an isolated sandbox. The sandbox is the security boundary — it provides process isolation, network policy enforcement, and resource limits. Isolation is never optional, regardless of whether the agent has MCP tools, a repo clone, or neither.

The controller creates sandboxes from a `SandboxTemplate` configured on the Repository CR (`settings.runtime.sandbox_template`). The template defines the container image, resource limits, and base configuration.

### Runtime Contract

The controller interacts with the sandbox exclusively through the [K8s SIG Agent Sandbox](https://github.com/kubernetes-sigs/agent-sandbox) SDK:

1. **Write config** — `WriteFile(config.json)` with resolved system prompt, instructions, model settings, MCP server commands, and limits
2. **Run agent** — `Run(agent-run)` executes the PydanticAI-based agent runtime inside the sandbox
3. **Read result** — `ReadFile(result.json)` to collect structured actions the agent produced
4. **Execute actions** — controller posts comments, creates reviews, adds labels via the git provider API using Repository CR credentials
5. **Destroy** — sandbox is torn down after result collection

The agent runtime is a PydanticAI-based Python program that reads `config.json`, runs an agentic tool-use loop with workspace tools and MCP servers, and writes structured results to `result.json`.

### Repo Clone

The repo clone is opt-in via an annotation on the Agent definition:

```yaml
annotations:
  agent.tekton.dev/clone-repo: "true"
```

Agents that only need prompt context (labeler reading PR title, reviewer getting diff via `{{ pull_request_diff }}`) skip the clone. Agents that need filesystem access (implementation, test fixing) opt in.

### What Agents Can Do

Agents produce structured results (JSON at `result.json`) that the controller executes as git actions via **result hooks**:

| Hook | Agent output | Controller action |
|------|-------------|-------------------|
| `comment` | Comment body text | Post as PR/issue comment |
| `review` | Review body + per-file inline comments | Submit PR review (COMMENT, APPROVE, REQUEST_CHANGES) |
| `label` | Label names to add/remove | Update labels |
| `create-pr` | PR title, body, head branch, base branch | Create pull request |
| `status` | Context name, state, description | Set commit status check |
| `commit` | Commit message + file path-to-content map | Create atomic multi-file commit on PR head branch |

Result hooks are controller-mediated — the agent never sees the git provider token. The controller reads the result, validates it, executes each action via the provider API using Repository CR credentials, and records audit entries in `AgentRunStatus.Actions`.

The executor uses a **partial success model**: if one action fails, it continues executing the remaining actions and returns both the successful audit records and aggregated errors.

Agents can also perform actions directly via MCP tools (e.g. GitHub MCP server with a token).

Agents cannot: modify Kubernetes resources, access other repos, or trigger non-git side effects.

### RBAC

- Agents inherit the Repository CR's RBAC policies
- The same approval rules that gate PipelineRuns also gate AgentRuns
- Changes to `.tekton/agents/` are governed by repo permissions (branch protection, CODEOWNERS)

---

## AgentRun CR

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
    agent.tekton.dev/clone-repo: "true"
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
    issue_number: 42
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

Key properties:
- `system_prompt` contains the **resolved** prompt — all template variables are already substituted
- `instructions` references resolved instruction files (repo paths or remote URLs)
- `event` contains enriched event context (PR details, comment body, labels, changed files)
- `limits` are resolved as `min(agent.limits, repo.settings.ai.max*)`
- `status.actions` tracks what the agent did for audit
- Immutable after completion
- Lifecycle: `Created -> Pending -> Running -> Succeeded | Failed`

---

## Key Design Decisions

1. **All triggers are git events** — no custom event sources, no direct agent-to-agent protocol
2. **Agent definitions live in `.tekton/agents/`** — version-controlled in the repo, not K8s CRDs
3. **AgentRun is a CRD** — the execution record, created by the controller
4. **`system_prompt` is the agent's identity** — natural language describing what the agent does
5. **Declarative tool selection** — MCP servers declared on Repository CR (infra), agents select by name (dev)
6. **Every agent runs in an isolated sandbox** — isolation is never optional
7. **Runtime contract** — controller writes config, runs agent, reads results; doesn't call LLM APIs itself
8. **RBAC inherited from Repository CR** — no separate agent-specific RBAC
9. **Agent results are git actions only** — comments, commits, PRs, status checks, labels
10. **LLM config on Repository CR, not on agents** — avoids secret sprawl, agents stay declarative
11. **Annotation-based triggers** — trigger matching uses `metadata.annotations` with PaC-style semantics
12. **Explicit instructions** — no auto-discovery of well-known files; developers opt-in via instruction annotations
13. **Template variables are flat and lazy** — `{{ variable_name }}` with no nesting or CEL; provider API calls only happen for variables actually referenced
14. **Result hooks are controller-mediated** — agents write structured JSON, controller executes git actions using Repository CR credentials; agent never sees the token
