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

Today, a git event can only trigger one agent at a time. If a PR needs code review, security scanning, and a summary, that's three separate AgentRuns — each triggered independently, each running in its own sandbox, each posting results without awareness of what the others found. The security scanner might flag a vulnerability that the reviewer also noticed, leading to duplicate comments. The summarizer has no idea what the reviewer or scanner said, so it can only summarize the diff — not the review.

An `AgentPipeline` would orchestrate multiple agents on a single event — similar to how a Tekton Pipeline composes Tasks:

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
      context_from: [review, security]
```

In this example, `review` and `security` run in parallel. Once both complete, `summarize` runs with their results injected as context — it can synthesize a summary that covers both the code quality feedback and the security findings in a single, coherent comment instead of three independent ones.

Key design areas:

- **Sequential and parallel execution** — agents run in parallel by default, with `run_after` for ordering dependencies. This mirrors Tekton Pipeline's DAG model where tasks without dependencies start immediately and dependent tasks wait for their inputs
- **Inter-agent context passing** — a `context_from` field on each agent specifies which prior agents' results should be injected into its prompt. The controller resolves these as template variables (`{{ review.result }}`, `{{ security.result }}`) so downstream agents can reference specific findings, not just raw diffs. This is the key difference from running agents independently — agents can build on each other's analysis
- **Shared sandbox vs isolated sandboxes** — two execution models:
  - **Isolated** (default) — each agent gets its own sandbox. Stronger isolation, but agents can only share structured results via the controller. Best when agents don't need filesystem state from each other
  - **Shared** — agents in the same pipeline share a single sandbox. A coding agent can write files that a test agent then runs, or a security scanner can inspect the exact filesystem state left by a prior agent. Cheaper (one sandbox instead of N) but weaker isolation — a misbehaving agent can corrupt the shared environment
- **Failure policy** — three modes:
  - `fail-fast` — abort the pipeline on the first agent failure
  - `continue` — run all agents regardless of failures, collect partial results
  - `conditional` — skip downstream agents based on prior outcomes (e.g., don't summarize if both review and security failed)
- **Result aggregation** — the controller collects results from all agents in the pipeline and can deduplicate or merge them before executing git actions. If both the reviewer and scanner flag the same line, post one comment instead of two. If multiple agents want to add labels, merge the label sets
- **Unified result hooks** — a pipeline-level `result-hooks` that executes after all agents complete, with access to the aggregated results. This allows a single summary comment that synthesizes all agent outputs, rather than N independent comments flooding the PR

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

## Structured Agent Workflows

**Status:** Not implemented. The runtime is purely prompt-driven — a single `agent.run_sync()` call where the LLM decides everything.

Today, the agent runtime (`runtime/agent_run.py`) works in one mode: give the LLM a system prompt, tools, and MCP servers, then let it figure out the rest. This works well for open-ended tasks like code review or triage where the LLM's judgment is the whole point. But some workflows have known steps that should execute in a defined order, with conditional branching, retries, or human checkpoints — and leaving all of that to the LLM's discretion is both unreliable and wasteful.

Consider an implementation workflow that should always follow a specific process:

```yaml
# .tekton/agents/implement-with-tests.yaml
apiVersion: agent.tekton.dev/v1alpha1
kind: Agent
metadata:
  name: implement-with-tests
  annotations:
    agent.tekton.dev/on-event: "issue_comment"
    agent.tekton.dev/on-comment: "/implement"
    agent.tekton.dev/clone-repo: "true"
    agent.tekton.dev/result-hooks: "true"
spec:
  workflow:
    steps:
      - name: analyze
        prompt: |
          Analyze the issue and the existing codebase. Identify which files
          need to change and outline your implementation approach.
          {{ issue_body }}
        tools: [read_file, list_files, run_command]

      - name: implement
        prompt: |
          Implement the changes based on your analysis.
          {{ steps.analyze.output }}
        tools: [read_file, write_file, list_files, run_command]

      - name: test
        prompt: |
          Write tests for the changes you made. Run the test suite.
          {{ steps.implement.output }}
        tools: [read_file, write_file, run_command]

      - name: lint
        type: command
        image: golangci/golangci-lint:latest
        run: "golangci-lint run ./..."
        on_failure: retry_step(implement, max=2)

      - name: verify
        type: command
        run: "cd {{ repo_clone_path }} && make test"
        on_failure: retry_step(implement, max=2)

      - name: scan
        type: command
        image: aquasec/trivy:latest
        run: "trivy fs --severity HIGH,CRITICAL {{ repo_clone_path }}"

      - name: submit
        prompt: |
          Summarize what you implemented and create a PR.
          Test results: {{ steps.verify.output }}
          Security scan: {{ steps.scan.output }}
        tools: [read_file, list_files]
        condition: "{{ steps.verify.exit_code }} == 0"
```

This is fundamentally different from the current prompt-only model. Instead of hoping the LLM remembers to run tests after implementing, the workflow guarantees it. Instead of the LLM deciding whether to retry after a test failure, the workflow defines the retry policy.

Key design areas:

- **Step types** — two kinds of steps:
  - `prompt` (default) — runs an LLM agent with the given prompt and tools, same as today's runtime but scoped to a single step. Each step can restrict which tools are available, so the analysis step can't write files and the implementation step can't submit results
  - `command` — runs a shell command directly without an LLM, for deterministic operations like running a test suite, linting, or building. The exit code and output are captured for downstream steps
- **Custom step images** — command steps can specify an `image` field to run in a user-provided container instead of the default agent runtime image. This is directly analogous to how Tekton Tasks let you pick a container image per step. A lint step can run in `golangci/golangci-lint`, a security scan in `aquasec/trivy`, a build step in a project-specific image with the right toolchain pre-installed — without bloating the base agent runtime image. The sandbox mounts the workspace volume into the custom container so it has access to the same files. Prompt steps always run in the agent runtime image since they need the PydanticAI runtime and LLM client
- **Step context passing** — each step's output is available to subsequent steps via `{{ steps.<name>.output }}`. The runtime manages the context window: earlier steps can be summarized to stay within token limits while the most recent step's output is passed in full
- **Conditional execution** — steps can have a `condition` field evaluated against prior step outputs or exit codes. A PR creation step can be gated on tests passing. A security scan step can be skipped if the diff only touches documentation
- **Failure handling** — per-step failure policies: `retry_step` (re-run a specific step with the error context), `fail` (abort the workflow), `continue` (proceed anyway), or `goto` (jump to a recovery step). This is where structured workflows shine over prompt-only — the retry loop is explicit, not dependent on the LLM spontaneously deciding to try again
- **Human-in-the-loop checkpoints** — a step can pause the workflow and post a comment asking for human approval before continuing. The workflow resumes when the human responds (via a comment command or reaction). Useful for gating destructive actions like committing to a protected branch or creating a PR with large changes
- **Framework integration** — the runtime contract (`config.json` in, `result.json` out) is framework-agnostic. The `workflow` field could map to [LangGraph](https://langchain-ai.github.io/langgraph/) graphs, [CrewAI](https://www.crewai.com/) crews, or [Google ADK](https://google.github.io/adk-docs/) pipelines under the hood. The agent definition stays declarative YAML; the runtime translates it to the framework's execution model. This also opens the door to agents defined as Python/TypeScript code instead of YAML for teams that need full programmatic control
- **Hybrid mode** — not every agent needs a workflow. Simple agents (triage, review) stay prompt-only — adding steps would just be ceremony. The `workflow` field is optional; if absent, the runtime behaves exactly as it does today. The two modes coexist in the same `.tekton/agents/` directory

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
