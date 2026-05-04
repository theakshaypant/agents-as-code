# AgentRun API Redesign

## Implementation Status

| Part | Description | Status |
|------|-------------|--------|
| 1a | AI Settings (extended) | Types implemented |
| 1b | MCP Server Catalog | Types implemented |
| 1c | Network Policy | Types implemented |
| 1d | Security Policy | Deferred (see [deferred-decisions.md](deferred-decisions.md)) |
| 2 | Agent Definition redesign | Types implemented |
| 3 | AgentRun redesign | Types implemented |
| 4 | Template Variables | Design complete, not started |
| 5 | Result Hooks | Design complete, not started |
| — | Adapter/controller logic | Not started |
| — | Provider event enrichment | Not started |

**Convention deviation**: All JSON tags use `snake_case` instead of the `camelCase` shown in this design doc. This was a deliberate project-wide convention decision applied during implementation.

## Context

The current AgentRun API is minimal — it captures a "purpose" string, a git event, token/timeout limits, and optional KG context. It hardcodes OpenHands as the only harness and has no way to express what tools the agent has, what instructions it should follow, or what it's allowed to access.

We want to redesign the API with a clear separation of concerns:

- **Repository CR** (infra team manages): policy, budgets, network, security, MCP server catalog, AI/model settings
- **Agent definition** (developer manages): system prompt, instructions, which tools to use from the catalog
- **AgentRun** (runtime snapshot): resolved config from both + event context

KG context is deferred — removed from AgentRun for now.

---

## Part 1: Repository CR — Policy & Infrastructure (infra team)

Add new sections to `RepositorySpec.Settings` for policies that apply to ALL agents in this repo.

### 1a. AI Settings (extended)

```go
type AIConfig struct {
    Enabled   bool   `json:"enabled"`
    Provider  string `json:"provider"`
    SecretRef Secret `json:"secret_ref"`
    Model     string `json:"model,omitempty"`
    BaseURL   string `json:"baseURL,omitempty"`

    // Per-run budget cap (USD). Controller kills runs exceeding this.
    MaxCostPerRun string `json:"maxCostPerRun,omitempty"`

    // Default token limit. Agent can set lower, not higher.
    MaxTokensPerRun int `json:"maxTokensPerRun,omitempty"`

    // Default timeout. Agent can set lower, not higher.
    MaxTimeoutSeconds int `json:"maxTimeoutSeconds,omitempty"`

    // Model-specific configuration.
    ModelConfig *ModelConfig `json:"modelConfig,omitempty"`
}

type ModelConfig struct {
    // Temperature controls randomness (0.0 = deterministic, 1.0+ = creative).
    Temperature *float64 `json:"temperature,omitempty"`

    // MaxOutputTokens limits the length of each model response.
    MaxOutputTokens int `json:"maxOutputTokens,omitempty"`

    // MaxContextTokens limits the total context window usage.
    // The controller truncates instruction content to fit within this budget.
    MaxContextTokens int `json:"maxContextTokens,omitempty"`

    // Thinking configures extended thinking / chain-of-thought.
    // Provider-specific (e.g. Claude's budget_tokens, OpenAI's reasoning_effort).
    Thinking *ThinkingConfig `json:"thinking,omitempty"`

    // Parameters is a provider-specific key-value map for settings
    // not covered by the common fields above (e.g. top_p, frequency_penalty).
    Parameters map[string]string `json:"parameters,omitempty"`
}

type ThinkingConfig struct {
    // Enabled controls whether extended thinking is active.
    Enabled bool `json:"enabled"`

    // BudgetTokens is the max tokens the model can use for thinking.
    // Maps to Claude's budget_tokens or OpenAI's max_completion_tokens for reasoning.
    BudgetTokens int `json:"budgetTokens,omitempty"`
}
```

### 1b. MCP Server Catalog

Infra team declares which MCP servers are available for agents to use. Agents reference these by name.

```go
type Settings struct {
    AI   *AIConfig   `json:"ai,omitempty"`

    // MCP servers available to agents in this repo.
    MCPServers []MCPServerSpec `json:"mcpServers,omitempty"`
}

type MCPServerSpec struct {
    // Name identifies this MCP server. Agents reference this name.
    Name string `json:"name"`

    // Image is a container image for the MCP server (sidecar model).
    // Mutually exclusive with Command.
    Image string `json:"image,omitempty"`

    // Command is the command to launch the MCP server (stdio transport).
    // Mutually exclusive with Image.
    Command []string `json:"command,omitempty"`

    // Args passed to the MCP server.
    Args []string `json:"args,omitempty"`

    // Env are environment variables for the MCP server.
    Env []EnvVar `json:"env,omitempty"`
}

type EnvVar struct {
    Name      string        `json:"name"`
    Value     string        `json:"value,omitempty"`
    ValueFrom *EnvVarSource `json:"valueFrom,omitempty"`
}

type EnvVarSource struct {
    SecretKeyRef    *KeyRef `json:"secretKeyRef,omitempty"`
    ConfigMapKeyRef *KeyRef `json:"configMapKeyRef,omitempty"`
}

type KeyRef struct {
    Name string `json:"name"`
    Key  string `json:"key"`
}
```

### 1c. Network Policy

```go
type Settings struct {
    // ...
    // Network policy for agent sandboxes.
    Network *NetworkPolicy `json:"network,omitempty"`
}

type NetworkPolicy struct {
    // Preset is the base network policy.
    // "restricted": deny all egress except explicitly allowed hosts
    // "permissive": allow all egress (default)
    // "air-gapped": no network access
    // +kubebuilder:validation:Enum=restricted;permissive;air-gapped
    // +kubebuilder:default=permissive
    Preset string `json:"preset"`

    // Egress allows specific outbound connections (used with "restricted").
    Egress []EgressRule `json:"egress,omitempty"`
}

type EgressRule struct {
    Host  string `json:"host,omitempty"`
    CIDR  string `json:"cidr,omitempty"`
    Ports []int  `json:"ports,omitempty"`
}
```

### 1d. Security Policy

```go
type Settings struct {
    // ...
    // Security policy for agent sandboxes.
    Security *SecurityPolicy `json:"security,omitempty"`
}

type SecurityPolicy struct {
    // Git controls what git operations agents can perform.
    Git *GitSecurityPolicy `json:"git,omitempty"`

    // Sandbox controls sandbox-level security.
    Sandbox *SandboxSecurityPolicy `json:"sandbox,omitempty"`
}

type GitSecurityPolicy struct {
    // Permissions is the set of allowed git operations.
    // Values: "read", "push", "comment", "createPR", "createReview",
    //         "createIssue", "merge", "label"
    Permissions []string `json:"permissions,omitempty"`

    // ProtectedBranches the agent cannot push to. Glob patterns supported.
    ProtectedBranches []string `json:"protectedBranches,omitempty"`
}

type SandboxSecurityPolicy struct {
    // ReadOnly makes the sandbox filesystem read-only.
    ReadOnly bool `json:"readOnly,omitempty"`

    // AllowShell controls whether agents can execute shell commands.
    // +kubebuilder:default=true
    AllowShell *bool `json:"allowShell,omitempty"`

    // RunAsUser sets the UID the agent process runs as.
    RunAsUser *int64 `json:"runAsUser,omitempty"`
}
```

### Example Repository CR

```yaml
apiVersion: agent.tekton.dev/v1alpha1
kind: Repository
metadata:
  name: my-repo
spec:
  url: https://github.com/org/my-repo
  git_provider:
    secret:
      name: github-token
      key: token
  settings:
    ai:
      enabled: true
      provider: anthropic
      secret_ref:
        name: anthropic-key
        key: api-key
      model: claude-sonnet-4-20250514
      maxCostPerRun: "1.00"
      maxTokensPerRun: 200000
      maxTimeoutSeconds: 600
      modelConfig:
        temperature: 0.2
        maxOutputTokens: 8192
        maxContextTokens: 128000
        thinking:
          enabled: true
          budgetTokens: 10000
        parameters:
          top_p: "0.95"

    mcpServers:
      - name: github
        image: ghcr.io/modelcontextprotocol/github:latest
        env:
          - name: GITHUB_TOKEN
            valueFrom:
              secretKeyRef:
                name: github-token
                key: token
      - name: linear
        command: ["npx", "-y", "@anthropic-ai/linear-mcp-server"]
        env:
          - name: LINEAR_API_KEY
            valueFrom:
              secretKeyRef:
                name: linear-token
                key: api-key

    network:
      preset: restricted
      egress:
        - host: "api.github.com"
          ports: [443]
        - host: "api.anthropic.com"
          ports: [443]

    security:
      git:
        permissions:
          - read
          - comment
          - createPR
          - createReview
        protectedBranches:
          - main
          - "release-*"
      sandbox:
        readOnly: false
        allowShell: true
```

---

## Part 2: Agent Definition — Behavior (developer)

The Agent definition in `.tekton/agents/*.yaml` focuses on WHAT the agent does, not what it's allowed to do.

```go
type AgentSpec struct {
    // SystemPrompt is the agent's core identity and behavioral instructions.
    // Replaces the old "purpose" field.
    SystemPrompt string `json:"systemPrompt"`

    // Tools selects which MCP servers (from the Repository catalog) to use
    // and which specific tools to enable.
    Tools *AgentToolsSpec `json:"tools,omitempty"`

    // Limits are agent-specific constraints.
    // Bounded by Repository settings — agent can set lower, not higher.
    Limits AgentLimits `json:"limits"`
}

type AgentToolsSpec struct {
    // MCPServers references MCP servers by name from the Repository catalog.
    MCPServers []string `json:"mcpServers,omitempty"`

    // Allowed is an allowlist of specific tools.
    // Format: "server:tool" or "server:*".
    // If empty, all tools from selected servers are available.
    Allowed []string `json:"allowed,omitempty"`
}

type AgentLimits struct {
    MaxTokens      int `json:"maxTokens"`
    TimeoutSeconds int `json:"timeoutSeconds"`
}
```

### Instructions

Instructions are loaded via PAC-style annotations. Everything is explicit — no auto-discovery of well-known files (CLAUDE.md, .cursorrules, etc.). Developers opt-in to whatever instruction files they want:

```yaml
annotations:
  agent.tekton.dev/on-event: "pull_request"
  agent.tekton.dev/on-target-branch: "main"
  # Repo-relative files (any file — CLAUDE.md, .cursorrules, custom docs)
  agent.tekton.dev/instruction: "CLAUDE.md"
  agent.tekton.dev/instruction-1: ".tekton/agents/review-guide.md"
  agent.tekton.dev/instruction-2: "CONTRIBUTING.md"
  # Remote URLs
  agent.tekton.dev/instruction-3: "https://raw.githubusercontent.com/org/shared/main/security-checklist.md"
```

Supported sources:
- Repo-relative paths (resolved from the repo's default branch)
- Remote HTTP(S) URLs (fetched at AgentRun creation time)

Common files developers might reference:
- `CLAUDE.md`, `.cursorrules`, `.github/copilot-instructions.md`, `AGENTS.md`, `.clinerules`
- `CONTRIBUTING.md`, `README.md` (general context, opt-in)
- Custom files anywhere in the repo or at remote URLs

### Example Agent Definition

```yaml
# .tekton/agents/code-reviewer.yaml
apiVersion: agent.tekton.dev/v1alpha1
kind: Agent
metadata:
  name: code-reviewer
  annotations:
    agent.tekton.dev/on-event: "pull_request"
    agent.tekton.dev/on-target-branch: "main"
    agent.tekton.dev/instruction: ".tekton/agents/review-standards.md"
    agent.tekton.dev/instruction-1: "https://raw.githubusercontent.com/org/shared/main/security-checklist.md"
spec:
  systemPrompt: |
    You are a senior code reviewer. Review every pull request for:
    - Correctness and logic errors
    - Security vulnerabilities (OWASP top 10)
    - Performance regressions
    - Test coverage gaps
    Provide actionable feedback as a PR review.

  tools:
    mcpServers:
      - github
    allowed:
      - "github:get_file_contents"
      - "github:create_pull_request_review"

  limits:
    maxTokens: 100000
    timeoutSeconds: 300
```

---

## Part 3: AgentRun — Runtime Snapshot

AgentRun is the resolved, immutable execution record. Created by the adapter, it snapshots the Agent definition + resolved instructions + event context. Policy (network, security, budget) is NOT in the AgentRun spec — the controller reads it from the Repository CR at reconciliation time.

```go
type AgentRunSpec struct {
    // AgentRef is the name of the matched Agent definition.
    AgentRef string `json:"agentRef"`

    // RepositoryRef is the name of the Repository CR.
    RepositoryRef string `json:"repositoryRef"`

    // SystemPrompt is the agent's core instructions, copied from Agent.
    SystemPrompt string `json:"systemPrompt"`

    // Instructions are resolved instruction file contents/refs.
    // Repo paths and remote URLs are resolved at creation time.
    Instructions []InstructionRef `json:"instructions,omitempty"`

    // Tools is the resolved tool configuration.
    // MCPServer names resolved against the Repository catalog.
    Tools *AgentToolsSpec `json:"tools,omitempty"`

    // Event describes the git event that triggered this run.
    Event AgentRunEventInfo `json:"event"`

    // Limits are the resolved execution constraints.
    // min(Agent.limits, Repository.settings.ai.max*)
    Limits AgentLimits `json:"limits"`
}

type InstructionRef struct {
    // Name is a human-readable identifier.
    Name string `json:"name,omitempty"`

    // Path is a repo-relative file path.
    // Mutually exclusive with URL.
    Path string `json:"path,omitempty"`

    // URL is a remote HTTP(S) URL.
    // Mutually exclusive with Path.
    URL string `json:"url,omitempty"`
}
```

### Enriched Event

```go
type AgentRunEventInfo struct {
    Type   string `json:"type"`
    Action string `json:"action,omitempty"`
    SHA    string `json:"sha,omitempty"`
    Branch string `json:"branch,omitempty"`
    Sender string `json:"sender"`
    URL    string `json:"url,omitempty"`

    // PR-specific details
    PullRequest *PullRequestInfo `json:"pullRequest,omitempty"`

    // Comment body for comment events
    Comment *CommentInfo `json:"comment,omitempty"`

    // Labels on the issue/PR at event time
    Labels []string `json:"labels,omitempty"`

    // Files affected by this event
    ChangedFiles []string `json:"changedFiles,omitempty"`
}

type PullRequestInfo struct {
    Number     int    `json:"number"`
    Title      string `json:"title"`
    HeadBranch string `json:"headBranch"`
    BaseBranch string `json:"baseBranch"`
}

type CommentInfo struct {
    Body string `json:"body"`
}
```

### Status

```go
type AgentRunStatus struct {
    duckv1.Status `json:",inline"`

    StartTime      *metav1.Time  `json:"startTime,omitempty"`
    CompletionTime *metav1.Time  `json:"completionTime,omitempty"`
    TokensUsed     int           `json:"tokensUsed,omitempty"`
    CostUSD        string        `json:"costUSD,omitempty"`
    Actions        []AgentAction `json:"actions,omitempty"`
    SandboxName    string        `json:"sandboxName,omitempty"`
    ExecID         string        `json:"execID,omitempty"`
}
```

---

## Trust Model

```
┌─────────────────────────────────────────────────────────┐
│  Repository CR (infra team)                             │
│                                                         │
│  - AI provider, model, API keys, model config           │
│  - Budget caps (maxCostPerRun, maxTokensPerRun)         │
│  - Model tuning (temperature, thinking, context window) │
│  - MCP server catalog (images, credentials)             │
│  - Network policy (preset + egress rules)               │
│  - Security policy (git perms, protected branches,      │
│    sandbox constraints)                                 │
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

---

## Part 4: Template Variables — Context Injection

The system prompt and instruction files support PaC-style template variables (`{{ variable_name }}`). Variables serve a dual purpose:

1. **Data injection** — the resolved value is substituted into the prompt at AgentRun creation time
2. **Sandbox signal** — the controller infers what to provision based on which variables are used (e.g. `{{ repo_clone_path }}` triggers a repo clone, `{{ pull_request_diff }}` triggers a diff fetch)

This lets agents work without any MCP tools for reading — the controller fetches all context and injects it into the prompt. MCP tools are only needed for *write* operations (posting reviews, creating PRs), and even those can be handled by result hooks (see Part 5).

### Variable Categories

| Category | Variable | Description | Controller action |
|----------|----------|-------------|-------------------|
| Repo | `repo_url` | Repository URL | None (metadata) |
| Repo | `repo_name` | Repository name | None (metadata) |
| Repo | `repo_owner` | Org/owner | None (metadata) |
| Repo | `default_branch` | Default branch name | None (metadata) |
| Event | `event_type` | push, pull_request, issue_comment, etc. | None (from event) |
| Event | `sender` | User who triggered the event | None (from event) |
| Event | `sha` | Commit SHA | None (from event) |
| Event | `branch` | Target branch | None (from event) |
| PR | `pull_request_number` | PR number | None (from event) |
| PR | `pull_request_title` | PR title | None (from event) |
| PR | `pull_request_description` | PR body/description | Fetch from provider API |
| PR | `pull_request_diff` | Full PR diff | Fetch from provider API |
| PR | `pull_request_files` | List of changed files | Fetch from provider API |
| PR | `pull_request_reviews` | Existing review comments | Fetch from provider API |
| PR | `pull_request_comments` | PR conversation thread | Fetch from provider API |
| Issue | `issue_number` | Issue number | None (from event) |
| Issue | `issue_title` | Issue title | Fetch from provider API |
| Issue | `issue_body` | Issue description | Fetch from provider API |
| Issue | `issue_comments` | Issue comment thread | Fetch from provider API |
| Issue | `issue_labels` | Labels on the issue | Fetch from provider API |
| Comment | `comment_body` | Triggering comment text | None (from event) |
| Sandbox | `repo_clone_path` | Path to cloned repo in sandbox | Clone repo into sandbox |

### Resolution

The adapter resolves variables when creating the AgentRun:

1. Scan `system_prompt` and instruction file contents for `{{ variable_name }}`
2. For each variable, determine what data is needed and fetch it (provider API calls, repo clone, etc.)
3. Substitute resolved values into the prompt text
4. Store the fully resolved prompt in the AgentRun spec

The AgentRun spec is the immutable snapshot — it always contains the resolved text, never raw template variables.

### Example

```yaml
# .tekton/agents/code-reviewer.yaml
apiVersion: agent.tekton.dev/v1alpha1
kind: Agent
metadata:
  name: code-reviewer
  annotations:
    agent.tekton.dev/on-event: "pull_request"
    agent.tekton.dev/on-target-branch: "main"
spec:
  system_prompt: |
    You are a senior code reviewer. Review this pull request:

    ## PR #{{ pull_request_number }}: {{ pull_request_title }}

    {{ pull_request_description }}

    ## Diff

    {{ pull_request_diff }}

    ## Previous Reviews

    {{ pull_request_reviews }}

    Provide your review focusing on correctness, security, and performance.
  tools:
    mcp_servers:
      - github
    allowed:
      - "github:create_pull_request_review"
  limits:
    max_tokens: 100000
    timeout_seconds: 300
```

This agent uses template variables to receive all context (diff, description, existing reviews) without needing MCP read tools. The only MCP tool it needs is `create_pull_request_review` to post its review. If result hooks are implemented, even that MCP dependency can be removed.

---

## Part 5: Result Hooks — Controller-Mediated Actions

Result hooks allow agents to produce structured output that the controller acts on, without the agent needing direct MCP tool access to the git provider. The agent writes its intent as structured data; the controller executes it using the Repository CR's git provider credentials.

This complements the MCP model:
- **MCP tools**: agent calls the git provider directly (needs token in MCP server env)
- **Result hooks**: agent produces output, controller calls the git provider (agent never sees the token)

Result hooks are useful when:
- The agent only needs to perform a small number of well-defined actions (post a comment, create a review)
- The infra team wants to avoid giving the agent direct API access
- The agent should work without any MCP tools at all

### Proposed Hook Types

| Hook | Agent output | Controller action |
|------|-------------|-------------------|
| `comment` | Comment body text | Post as PR/issue comment via provider API |
| `review` | Review body + per-file comments | Submit PR review via provider API |
| `label` | Label names to add/remove | Update labels via provider API |
| `create-pr` | PR title, body, branch | Create pull request via provider API |
| `status` | Status check name + state | Set commit status via provider API |

### Example: Zero-MCP Reviewer

```yaml
# Agent definition — no MCP tools at all
apiVersion: agent.tekton.dev/v1alpha1
kind: Agent
metadata:
  name: simple-reviewer
  annotations:
    agent.tekton.dev/on-event: "pull_request"
    agent.tekton.dev/on-target-branch: "main"
spec:
  system_prompt: |
    Review this pull request:

    {{ pull_request_diff }}

    Respond with a JSON object:
    {
      "review": {
        "body": "overall review summary",
        "event": "APPROVE" | "REQUEST_CHANGES" | "COMMENT",
        "comments": [{"path": "file.go", "line": 10, "body": "issue here"}]
      }
    }
  limits:
    max_tokens: 50000
    timeout_seconds: 300
```

The controller parses the agent's structured output and posts the review using the git provider credentials from the Repository CR. The agent never had a GitHub token.

---

## What changes from today

| Area | Before | After |
|------|--------|-------|
| Agent identity | `purpose` (flat string) | `system_prompt` (inline prompt with template variables) |
| Context injection | None | PaC-style template variables (`{{ pull_request_diff }}`, etc.) |
| Instructions | None | PAC-style annotation refs (repo + remote URLs), explicit opt-in |
| Tools | Hardcoded OpenHands | Declarative MCP servers on Repo CR, agent selects by name |
| Agent output | Hardcoded harness result | Result hooks (controller-mediated) or MCP tools (agent-mediated) |
| Model config | None | Temperature, thinking, context window, output tokens on Repo CR |
| Network | No controls | Preset + egress allowlist on Repo CR |
| Security | No controls | Deferred (see deferred-decisions.md) |
| Budget | Token cap only on agent | Token + cost + timeout caps on Repo CR (agent bounded) |
| Event info | Sparse (type/SHA/branch/sender) | Rich (PR details, comment body, labels, changed files) |
| KG context | In AgentRun spec | Deferred — removed for now |

## Files to modify

- `pkg/apis/agent/v1alpha1/types.go` — add Settings fields (MCPServers, Network, AI caps, ModelConfig), shared types (EnvVar, KeyRef) **[done]**
- `pkg/apis/agent/v1alpha1/agent_types.go` — redesign AgentSpec (system_prompt, tools ref, limits) **[done]**
- `pkg/apis/agent/v1alpha1/agentrun_types.go` — redesign AgentRunSpec (system_prompt, instructions, tools, enriched event), remove KG context **[done]**
- `config/300-repository.yaml` — regenerate CRD with new settings fields **[done]**
- `config/300-agent.yaml` — regenerate CRD with new agent fields **[done]**
- `config/300-agentrun.yaml` — regenerate CRD **[done]**
- `pkg/matcher/parse.go` — update validation for system_prompt, snake_case limits **[done]**
- `pkg/adapter/adapter.go` — resolve instruction annotations, populate enriched event, resolve limits **[not started]**
- `pkg/provider/github/parse.go` — extract PR number, comment body, labels, changed files into event **[not started]**
- `pkg/agentharness/openhands.go` — update BuildTask to use new AgentRun types **[not started]**
