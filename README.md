# Agents as Code (AAC)

> **Proof of Concept** — this project demonstrates a working end-to-end flow for running AI agents on git events. See the [Roadmap](docs/roadmap.md) for where it's heading.

Run AI agents on git events with declarative tool access and repo-aware context.

## Why

AI-powered developer workflows — automated code review, PR descriptions, issue triage, inline suggestions, implementation — are driven by LLM analysis of code diffs and CI context. AAC makes these workflows declarative: define agents as YAML in your repo, configure infrastructure once on a Kubernetes CR, and let git events drive everything.

AAC uses template variables (`{{ pull_request_diff }}`, `{{ issue_comments }}`, etc.) to inject rich context into agent prompts, combined with MCP tools for direct API access when needed. Every agent runs in an isolated sandbox with configurable network policy, budget caps, and tool access — the infra team controls what's allowed, developers control what each agent does.

## How It Works

1. Define agents as YAML files in `.tekton/agents/` in your repository
2. Configure a Repository CR pointing at your repo with LLM settings and MCP server catalog
3. When a git event matches an agent's triggers, AAC creates an AgentRun with resolved instructions, tools, and enriched event context

```yaml
# .tekton/agents/triage.yaml
apiVersion: agent.tekton.dev/v1alpha1
kind: Agent
metadata:
  name: triage
  annotations:
    agent.tekton.dev/on-event: "[pull_request, issue_comment]"
    agent.tekton.dev/on-target-branch: "main"
    agent.tekton.dev/on-comment: "/triage"
spec:
  system_prompt: |
    Triage incoming pull requests. Label by area,
    assess complexity, and identify reviewers.
  tools:
    mcp_servers:
      - github
  limits:
    max_tokens: 8000
    timeout_seconds: 120
```

Agents define their behavior via `system_prompt` and select tools from the Repository's MCP server catalog. The infra team controls what's available (LLM settings, MCP servers, network policy, budget caps); developers control what each agent does.

## What's Working

The POC demonstrates a complete end-to-end pipeline:

- **Webhook handling** — receives, validates, and parses GitHub webhook events (push, pull_request, pull_request_review, issue_comment, issues, labeled)
- **Agent matching** — discovers `.tekton/agents/*.yaml` from the repo and matches against events using annotation-based triggers
- **Template variable resolution** — lazily resolves `{{ pull_request_diff }}`, `{{ issue_comments }}`, and 25+ other variables from the GitHub API with caching
- **Instruction loading** — resolves instruction files from repo paths and remote URLs via annotation references
- **Sandbox execution** — real [K8s SIG Agent Sandbox](https://github.com/kubernetes-sigs/agent-sandbox) integration with a PydanticAI-based runtime; full lifecycle management (create → write config → run → read result → destroy)
- **Repo cloning** — opt-in via `clone-repo` annotation; clones the PR head branch or event branch into the sandbox
- **Result hooks** — 6 action types (comment, review, label, create-pr, status, commit) executed by the controller using Repository CR credentials, with partial success handling
- **GitHub provider** — complete implementation of all read and write operations (diffs, reviews, comments, labels, status checks, commits)

See the [Roadmap](docs/roadmap.md) for what's next — knowledge graph context, security policy, agent chaining, and more.

## Documentation

**Guides**
- [Getting Started](docs/guides/getting-started.md) — deploy AAC locally with kind and test it end-to-end
- [Writing Agents](docs/guides/writing-agents.md) — define agents, triggers, template variables, tools, instructions, and result hooks

**Reference**
- [Architecture & Concepts](docs/reference/concepts.md) — event flow, trust model, execution model, design decisions
- [Configuration](docs/reference/configuration.md) — Repository CR settings: AI, MCP servers, network, runtime

**Project**
- [Roadmap](docs/roadmap.md) — vision, future work, and what this POC enables
