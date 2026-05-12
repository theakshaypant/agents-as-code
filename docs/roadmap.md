# Roadmap

AAC is a proof of concept that demonstrates a complete end-to-end flow for running AI agents on git events. This document outlines what the POC has proven and where the project can go.

---

## What the POC Demonstrates

The core pipeline is working end-to-end:

**Webhook reception** → **Agent matching** → **Template variable resolution** → **Sandbox execution** → **Result hook actions** → **Git operations**

Specifically:
- GitHub webhooks are received, validated, and parsed for push, pull_request, pull_request_review, issue_comment, issues, and label events
- Agent definitions in `.tekton/agents/` are discovered and matched against events using annotation-based triggers with PaC-style semantics
- Template variables in system prompts (`{{ pull_request_diff }}`, `{{ issue_comments }}`, etc.) are lazily resolved from the GitHub API with caching
- Instructions are loaded from repo files and remote URLs via annotation references
- The AgentRun reconciler manages sandbox lifecycle via K8s SIG Agent Sandbox, writes runtime config, runs the PydanticAI agent, reads structured results, and executes git actions
- Opt-in repo cloning into the sandbox (PR head branch or event branch) via `clone-repo` annotation
- Result hooks execute 6 action types (comment, review, label, create-pr, status, commit) with partial success handling
- The GitHub provider implements all read and write methods (PR diffs, reviews, comments, issue data, label management, commit creation)

The architecture is in place. What follows is what it enables.

---

## Sandbox Runtime

**Status:** Implemented. The controller uses [K8s SIG Agent Sandbox](https://github.com/kubernetes-sigs/agent-sandbox) for process isolation and a PydanticAI-based Python runtime for agent execution.

The runtime contract is minimal — input at `config.json`, output at `result.json`. The controller writes config via the agent-sandbox SDK, runs `agent-run`, and reads results.

The agent runtime (`runtime/agent_run.py`) uses PydanticAI for the agentic tool-use loop:
- **Model-agnostic** — supports Anthropic, OpenAI, Gemini, and other providers via PydanticAI
- **Workspace tools** — read/write files, list directories, run shell commands
- **MCP integration** — MCP servers from the config are launched as subprocesses via `MCPServerStdio`. For npm packages, use `npx -y @scope/package` — Node.js is included in the runtime image.
- **Structured output** — `Result` Pydantic model validates and retries, producing `result.json` with actions the controller executes

---

## Knowledge Graph Context

**Status:** Types and controller skeleton exist. Not exercised by the agent execution path.

The vision is giving agents structural understanding of the codebase — not just diffs and comments, but how code relates to other code.

Possible directions:

- **KG-powered template variables** — variables like `{{ related_code }}` or `{{ architectural_context }}` backed by graph queries, giving agents relevant code context without needing to search for it
- **KG as an MCP server** — expose the knowledge graph as an MCP server that agents can query directly, letting the agent decide what context it needs rather than the controller pre-selecting it
- **Automatic graph building** — incremental knowledge graph updates on push events, powered by AST extraction (tree-sitter) and semantic extraction, with community detection for architectural clustering

The Repository CR already has `knowledge_graph` configuration fields (branch scoping, storage, ignore patterns). The context filtering pipeline (seed extraction → strategy selection → graph traversal → token budget truncation) is designed but not yet implemented.

---

## Security Policy

**Status:** Not implemented. The agent can use all MCP tools available in the catalog without restriction.

A meaningful security policy needs to unify two paths for git operations:

- **MCP-mediated** — agent calls GitHub directly via MCP server with a token
- **Controller-mediated** — agent produces a result, controller calls GitHub

Key areas:

- **Per-MCP-server permissions** — infra team controls which tools each MCP server exposes; agent tool selection becomes an intersection of what's available and what's allowed
- **Protected branches** — prevent agents from pushing to specific branches
- **Sandbox hardening** — readOnly filesystems, shell access control, runAsUser constraints (maps to Kubernetes pod security context)
- **Token scoping** — the most effective enforcement is using least-privilege tokens. Given an MCP server token, the model can do anything the token allows regardless of software-level permission lists

---

## Multi-Agent Runs

**Status:** Not implemented. Each AgentRun currently executes a single agent.

An AgentRun could orchestrate multiple agents in sequence — similar to how a Tekton Pipeline composes Tasks. A single event could trigger a coordinated workflow:

```yaml
# .tekton/agents/pr-workflow.yaml
apiVersion: agent.tekton.dev/v1alpha1
kind: AgentPipeline
metadata:
  name: pr-workflow
  annotations:
    agent.tekton.dev/on-event: "pull_request"
    agent.tekton.dev/on-target-branch: "main"
spec:
  agents:
    - name: review
      agent_ref: code-reviewer
    - name: security
      agent_ref: security-scanner
    - name: summarize
      agent_ref: pr-summarizer
      run_after: [review, security]
```

Key design areas:

- **Sequential and parallel execution** — agents can run in parallel by default, with `run_after` for ordering dependencies
- **Result passing** — earlier agents' results (comments, reviews, labels) available as context to later agents via template variables
- **Failure policy** — continue on failure, fail-fast, or conditional execution based on prior agent outcomes
- **Shared sandbox vs isolated** — whether agents in the same run share a sandbox (cheaper, can share filesystem state) or get separate sandboxes (stronger isolation)

---

## Long-Running Agents

**Status:** Not implemented. Every AgentRun currently creates a sandbox, runs a single agent, and destroys the sandbox — no state survives between runs.

Today, agents triggered on the same issue or PR operate in complete isolation. In the [working examples](https://github.com/theakshaypant/yeet-test), the triage agent classifies issue #1, the implementer agent creates PR #6 from that issue, and the reviewer agent reviews the PR — but none of them know what the others did. The implementer doesn't see the triage analysis, and the reviewer doesn't know why the implementer made specific design choices.

A long-running agent would persist for the lifecycle of a GitHub object (issue, PR, or both) and accumulate context across every interaction:

```
Issue #1 opened
  └─ /triage → agent analyzes, labels, posts summary
       context saved: classification, complexity, suggested approach
  └─ /implement → same agent context knows the triage analysis
       context saved: implementation rationale, files changed, trade-offs
  └─ PR #6 created (linked to issue #1)
       └─ reviewer triggers → same context includes triage + implementation reasoning
       └─ reviewer posts comments → context updated with review feedback
       └─ developer pushes fixes → agent re-reviews with full history
  └─ PR #6 merged → issue #1 closed → agent context archived
```

Key design areas:

- **Lifecycle binding** — an agent session is bound to a GitHub object (issue, PR, or an issue + its linked PRs). The session stays alive until the object is closed/merged, then the context is archived or destroyed
- **Shared memory across agent types** — triage, implementer, and reviewer agents on the same issue/PR read and write to a shared context store. Each agent appends its observations, decisions, and rationale so downstream agents can build on prior work rather than starting from scratch
- **Sandbox strategy** — long-running doesn't necessarily mean a long-running sandbox. The sandbox can still be ephemeral per invocation, while the persistent context lives outside the sandbox (in a PVC, CRD status, or external store like a vector DB). The controller injects accumulated context into each new sandbox via `config.json`
- **Context as template variables** — new template variables like `{{ agent_history }}`, `{{ prior_analysis }}`, or `{{ implementation_rationale }}` that resolve from the accumulated context, keeping the agent definition format unchanged
- **Warm sandboxes (optional)** — for latency-sensitive use cases, agent-sandbox's suspend/resume could keep a sandbox warm and idle between interactions instead of recreating it each time. This trades resource cost for faster response
- **Context budget** — as interactions accumulate, the context grows. A summarization or relevance-filtering strategy is needed to keep the context within token limits while preserving the most useful information
- **Eviction and cleanup** — stale sessions (inactive issues, abandoned PRs) need TTL-based eviction. Archived context could be queryable for post-mortem or audit

This is where alternatives like [kagent](https://kagent.dev/) become relevant — kagent provides persistent vector-backed memory across agent sessions natively. The choice is between building lifecycle-bound context into AAC's controller or integrating with a framework that already solves agent memory.

---

## Agent Chaining

**Status:** The mechanism exists naturally — agents post git actions which trigger new events which can trigger other agents. No explicit chaining protocol needed.

What's needed for production use:

- **Loop prevention** — detect and break circular agent triggers
- **Chain depth limits** — cap how many agents can chain from a single originating event
- **Seed extraction from comments** — when an agent's comment triggers another agent, extract the relevant context from the comment body
- **Coordination patterns** — reviewer agent posts a comment that triggers a coding agent, which creates a PR that triggers the reviewer again

---

## PipelineRun Integration

**Status:** Not implemented. AgentRuns and PipelineRuns are currently independent — the same git event can trigger both, but they don't interact.

Bidirectional integration between agents and pipelines:

- **Post-PipelineRun agents** — trigger an agent when a PipelineRun completes or fails. A failure-analysis agent could read pipeline logs, diagnose the root cause, and post a comment — or a release-notes agent could generate changelogs after a successful release pipeline.
- **Pre-PipelineRun agents** — run an agent before a pipeline starts. A pre-flight agent could validate configuration, check dependencies, or gate the pipeline based on code analysis.
- **Agent-triggered pipelines** — let agents create or trigger PipelineRuns as a result hook action. A coding agent could push a fix and then kick off the CI pipeline, or a triage agent could start a specific test suite based on its analysis.
- **Shared context** — pass PipelineRun results (logs, status, artifacts) into agent template variables, and pass agent analysis back as PipelineRun parameters or annotations.

This turns AAC and PaC into a unified system where pipelines handle deterministic build/test/deploy workflows and agents handle the judgment calls around them.

---

## Cost Control & Observability

**Status:** `max_cost_per_run` and token/timeout caps exist as fields but are not enforced mid-run.

- **Cost enforcement** — integrate with LLM provider billing/usage APIs to track spend mid-run and terminate when the budget cap is exceeded
- **Namespace/cluster budget caps** — aggregate cost controls beyond per-run limits
- **OpenTelemetry tracing** — trace the full agent path from webhook to result execution
- **Structured audit logging** — beyond `AgentRunStatus.Actions`, a queryable audit log of all agent actions across runs
- **Metrics** — AgentRun duration, token usage, success rate, cost per agent

---

## Multi-Provider Support

**Status:** GitHub is the only implemented provider. The provider interface is already abstracted.

The `provider.Interface` defines all operations (event parsing, file access, PR/issue data, comments, reviews, labels, status checks, commits). Adding GitLab, Bitbucket, or Gitea/Forgejo requires implementing this interface — no architecture changes needed. This follows PaC's multi-provider model.

---

## Additional Future Work

- **MCP transport** — support SSE and streamable HTTP transports alongside the current stdio (command) model
- **Sandbox tool installation** — allow agents to declare CLI tools or packages to install in the sandbox before execution (linters, language runtimes, custom binaries)
- **Streaming results** — process agent actions as they're produced rather than waiting for sandbox completion, for better UX on long-running agents
- **Improved KG quality** — address known weaknesses in tree-sitter-based extraction (node deduplication, community fragmentation) for better structural context
- **GitHub App support** — JWT-based token generation with dynamic installation discovery, alongside the current PAT-based auth
