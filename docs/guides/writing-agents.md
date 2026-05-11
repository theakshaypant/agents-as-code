# Writing Agents

Agent definitions are YAML files that live in `.tekton/agents/` in your repository. They describe what an agent does, when it triggers, and what tools it uses. Agent definitions are version-controlled, reviewable in PRs, and discovered automatically by AAC on each git event.

## Agent Definition Structure

```yaml
apiVersion: agent.tekton.dev/v1alpha1
kind: Agent
metadata:
  name: my-agent
  annotations:
    agent.tekton.dev/on-event: "pull_request"
    # ... trigger and behavior annotations
spec:
  system_prompt: |
    Your agent's instructions here.
  tools:
    mcp_servers:
      - github
    allowed:
      - "github:get_file_contents"
  limits:
    max_tokens: 50000
    timeout_seconds: 300
```

### Fields

| Field | Description |
|-------|-------------|
| `system_prompt` | The agent's core identity and behavioral instructions. Supports [template variables](#template-variables). |
| `tools.mcp_servers` | MCP servers to use, referenced by name from the [Repository CR catalog](../reference/configuration.md#mcp-server-catalog). |
| `tools.allowed` | Allowlist of specific tools in `"server:tool"` format. If empty, all tools from selected servers are available. |
| `limits.max_tokens` | Maximum token budget for this agent. Bounded by the Repository CR's `max_tokens_per_run`. |
| `limits.timeout_seconds` | Maximum execution time. Bounded by the Repository CR's `max_timeout_seconds`. |

---

## Trigger Annotations

Triggers are declared as annotations on Agent metadata using the `agent.tekton.dev/` prefix.

| Annotation | Description | Default (absent) |
|------------|-------------|------------------|
| `on-event` | Event types to match (**required**). | — |
| `on-target-branch` | Glob patterns for target branch filtering. | Match all |
| `on-comment` | Regex match on comment body. | Pass |
| `on-path-change` | Glob match on changed files. | Pass |
| `on-label` | Label match for `issues_labeled` / `pull_request_labeled` events. | Pass |

**Value format:** Single value (`"push"`) or bracket array (`"[push, pull_request]"`). Use `&#44;` for literal commas within values.

**Supported events:** `push`, `pull_request`, `pull_request_review`, `issue_comment`, `issues_labeled`, `pull_request_labeled`

### Matching Semantics

- **`on-comment` is a separate matching track.** If present and matched, the agent is selected immediately — other annotations are not checked. If the event is a comment but `on-comment` doesn't match, the agent is skipped entirely.
- **Standard path:** All present annotations are AND'd (all must match). Within each annotation, array values are OR'd.
- **Label events** without an `on-label` annotation do not match — this prevents unintended triggers.

---

## Behavior Annotations

| Annotation | Description |
|------------|-------------|
| `agent.tekton.dev/result-hooks` | Set to `"true"` to auto-inject the [result hooks](#result-hooks) instruction. Teaches the agent the structured JSON output format. |
| `agent.tekton.dev/clone-repo` | Set to `"true"` to clone the repository into the sandbox at `/workspace/repo`. Clones the PR head branch for PR events, or the event branch otherwise. |

---

## Instructions

Instructions are additional context files loaded via annotations. Developers explicitly opt-in to whatever instruction files they want — there is no auto-discovery of well-known files.

```yaml
annotations:
  agent.tekton.dev/instruction: "CLAUDE.md"
  agent.tekton.dev/instruction-1: ".tekton/agents/review-guide.md"
  agent.tekton.dev/instruction-2: "CONTRIBUTING.md"
  agent.tekton.dev/instruction-3: "https://raw.githubusercontent.com/org/shared/main/security-checklist.md"
```

**Supported sources:**
- **Repo-relative paths** — resolved from the repo's default branch
- **Remote HTTP(S) URLs** — fetched at AgentRun creation time

Common files developers might reference:
- `CLAUDE.md`, `.cursorrules`, `.github/copilot-instructions.md`, `AGENTS.md`
- `CONTRIBUTING.md`, `README.md`
- Custom files anywhere in the repo or at remote URLs

---

## Template Variables

Agent system prompts support `{{ variable_name }}` template variables that are resolved at AgentRun creation time. Variables use flat names — no nesting, no CEL expressions.

### Resolution Flow

1. The adapter extracts all `{{ variable_name }}` references from the agent's `system_prompt`
2. Metadata variables (from the event) are resolved immediately
3. Provider variables (requiring GitHub API calls) are fetched lazily — only if actually referenced
4. Provider API results are cached per variable name to avoid duplicate calls
5. Unresolvable variables are replaced with an empty string and a warning is logged

### Available Variables

| Variable | Source | Description |
|----------|--------|-------------|
| **Repo context** | | |
| `repo_url` | event | Repository HTML URL |
| `repo_name` | event | Repository name |
| `repo_owner` | event | Repository owner/organization |
| `default_branch` | event | Default branch name |
| **Event context** | | |
| `event_type` | event | Trigger type (`push`, `pull_request`, etc.) |
| `sender` | event | User who triggered the event |
| `sha` | event | Head commit SHA |
| `branch` | event | Target/base branch |
| `comment_body` | event | Full comment body (for comment events) |
| `trigger_comment_args` | event | Comment text after the matched `on-comment` pattern |
| **Pull request** | | |
| `pull_request_number` | event | PR number |
| `pull_request_title` | event | PR title |
| `pull_request_author` | event | PR author login |
| `pull_request_url` | event | PR HTML URL |
| `pull_request_head_sha` | event | Head SHA of the PR branch |
| `pull_request_description` | provider API | PR body/description |
| `pull_request_diff` | provider API | Full PR diff |
| `pull_request_files` | provider API | Changed file paths, one per line |
| `pull_request_reviews` | provider API | PR reviews formatted as `@user (STATE): body` |
| `pull_request_comments` | provider API | PR/issue comments formatted as `@user: body` |
| **Issue** | | |
| `issue_number` | event | Issue/PR number (alias for `pull_request_number`) |
| `issue_title` | provider API | Issue title |
| `issue_body` | provider API | Issue body |
| `issue_comments` | provider API | Issue comments formatted as `@user: body` |
| `issue_labels` | event/provider | Comma-separated label names |
| **Sandbox** | | |
| `repo_clone_path` | constant | Path where repo is cloned in sandbox (`/workspace/repo`) |

### Example

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

This agent receives all PR context through template variables without needing MCP read tools. The adapter fetches the diff, file list, and reviews from the GitHub API and substitutes them into the prompt before creating the AgentRun.

---

## Tools

Agents select MCP servers from the Repository CR's catalog by name and can optionally scope tool access with an allowlist.

```yaml
spec:
  tools:
    mcp_servers:
      - github
      - linear
    allowed:
      - "github:get_file_contents"
      - "github:create_pull_request_review"
```

- `mcp_servers` references servers by name from the Repository CR's `settings.mcp_servers` catalog
- `allowed` uses `"server:tool"` format to restrict which tools the agent can use
- If `allowed` is empty, all tools from the selected servers are available
- The infra team controls what MCP servers exist (via the Repository CR); developers control which ones each agent uses

Agents can work without any MCP tools at all — template variables handle the read path (controller fetches context) and [result hooks](#result-hooks) handle the write path (controller executes actions).

---

## Result Hooks

Result hooks let agents produce structured output that the controller executes as git actions. The agent writes a JSON result; the controller posts comments, creates reviews, adds labels, etc. using the Repository CR's git provider credentials. The agent never sees the git provider token.

### Enabling Result Hooks

Add the `result-hooks` annotation to your agent definition:

```yaml
annotations:
  agent.tekton.dev/result-hooks: "true"
```

This auto-injects a built-in instruction file that teaches the agent the structured JSON output format for `result.json`.

### Result Format

The agent writes a JSON file to `result.json`:

```json
{
  "actions": [
    { "type": "comment", "body": "..." },
    { "type": "review", "event": "COMMENT", "body": "...", "comments": [...] }
  ],
  "tokens_used": 28400,
  "cost_usd": "0.42"
}
```

### Action Types

**comment** — Post a PR or issue comment

```json
{"type": "comment", "body": "Your comment text here."}
```

**review** — Submit a pull request review

```json
{
  "type": "review",
  "event": "COMMENT",
  "body": "Overall review summary.",
  "comments": [
    {"path": "pkg/api/user.go", "line": 42, "body": "Potential nil dereference here."}
  ]
}
```

`event` must be one of: `COMMENT`, `APPROVE`, `REQUEST_CHANGES`. Both `body` and `comments` are optional.

**label** — Add or remove labels

```json
{"type": "label", "add": ["bug", "needs-fix"], "remove": ["needs-triage"]}
```

At least one of `add` or `remove` must be provided.

**create-pr** — Create a new pull request

```json
{
  "type": "create-pr",
  "title": "Fix authentication bug",
  "body": "This PR fixes the login flow by...",
  "head": "fix/auth-bug",
  "base": "main"
}
```

`title`, `head`, and `base` are required.

**status** — Set a commit status check

```json
{
  "type": "status",
  "context": "agent/code-review",
  "state": "success",
  "description": "Code review passed with no issues."
}
```

`context` and `state` are required. `state` must be one of: `success`, `failure`, `error`, `pending`.

**commit** — Create a multi-file commit on the PR branch

```json
{
  "type": "commit",
  "message": "Fix linting issues and update docs",
  "files": {
    "README.md": "# Updated README\n\nNew content here.",
    "src/main.go": "package main\n\nfunc main() {\n\t// Fixed code\n}\n"
  }
}
```

`message` is required. `files` maps repo-relative paths to their complete content. The commit is made on the PR's head branch.

### Partial Success

The controller uses a partial success model — if one action fails, it continues executing the remaining actions. Both successful audit records and errors are tracked in the AgentRun status.

---

## Examples

### Triage Agent

Fires on new PRs and `/triage` comments. No tools needed — uses template variables for context and result hooks for output.

```yaml
# .tekton/agents/triage.yaml
apiVersion: agent.tekton.dev/v1alpha1
kind: Agent
metadata:
  name: triage
  annotations:
    agent.tekton.dev/on-event: "[pull_request, issue_comment]"
    agent.tekton.dev/on-target-branch: "[main, release-*]"
    agent.tekton.dev/on-comment: "/triage"
    agent.tekton.dev/result-hooks: "true"
spec:
  system_prompt: |
    Triage incoming pull requests. Label by area (bug, feature, docs, tests),
    assess complexity, identify reviewers based on changed files,
    and post a summary comment.

    PR #{{ pull_request_number }}: {{ pull_request_title }}
    Changed files: {{ pull_request_files }}
  limits:
    max_tokens: 8000
    timeout_seconds: 120
```

### Code Reviewer

Uses template variables for PR context and MCP tools to post reviews.

```yaml
# .tekton/agents/reviewer.yaml
apiVersion: agent.tekton.dev/v1alpha1
kind: Agent
metadata:
  name: reviewer
  annotations:
    agent.tekton.dev/on-event: "[pull_request, issue_comment]"
    agent.tekton.dev/on-target-branch: "main"
    agent.tekton.dev/on-comment: "/review"
    agent.tekton.dev/instruction: ".tekton/agents/review-standards.md"
spec:
  system_prompt: |
    Review PR #{{ pull_request_number }} ("{{ pull_request_title }}") for bugs,
    security issues, and style. {{ trigger_comment_args }}

    ## Diff
    {{ pull_request_diff }}

    ## Existing reviews
    {{ pull_request_reviews }}
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

### Zero-MCP Reviewer

Uses only template variables and result hooks — no MCP tools at all. The agent never sees the GitHub token.

```yaml
# .tekton/agents/simple-reviewer.yaml
apiVersion: agent.tekton.dev/v1alpha1
kind: Agent
metadata:
  name: simple-reviewer
  annotations:
    agent.tekton.dev/on-event: "pull_request"
    agent.tekton.dev/on-target-branch: "main"
    agent.tekton.dev/result-hooks: "true"
spec:
  system_prompt: |
    Review this pull request for bugs, security issues, and performance.
    {{ pull_request_diff }}
  limits:
    max_tokens: 50000
    timeout_seconds: 300
```

### Issue Investigator

Reacts to a specific issue label.

```yaml
# .tekton/agents/investigator.yaml
apiVersion: agent.tekton.dev/v1alpha1
kind: Agent
metadata:
  name: investigator
  annotations:
    agent.tekton.dev/on-event: "issues_labeled"
    agent.tekton.dev/on-label: "needs-investigation"
    agent.tekton.dev/result-hooks: "true"
spec:
  system_prompt: |
    Analyze this issue, find related code, and post an initial
    investigation comment.

    Issue: {{ issue_title }}
    {{ issue_body }}

    Discussion so far:
    {{ issue_comments }}
  limits:
    max_tokens: 8000
    timeout_seconds: 120
```

### Coding Agent

Needs filesystem access via repo clone and full MCP tool access.

```yaml
# .tekton/agents/implementer.yaml
apiVersion: agent.tekton.dev/v1alpha1
kind: Agent
metadata:
  name: implementer
  annotations:
    agent.tekton.dev/on-event: "issue_comment"
    agent.tekton.dev/on-comment: "/implement"
    agent.tekton.dev/clone-repo: "true"
spec:
  system_prompt: |
    You are a coding agent for {{ repo_owner }}/{{ repo_name }}.
    The user said: {{ trigger_comment_args }}

    The repo is cloned at {{ repo_clone_path }}.
    Implement the requested changes and create a pull request.
  tools:
    mcp_servers:
      - github
  limits:
    max_tokens: 200000
    timeout_seconds: 600
```
