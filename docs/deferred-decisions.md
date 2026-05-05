# Deferred Decisions & Future Improvements

Decisions and improvements explicitly deferred during the API redesign. Tracked here so they don't get lost.

---

## Repository CR — Settings

### AI Config (Part 1a — types implemented)

- **Temperature as string**: `temperature` is serialized as a `string` because `controller-gen` rejects `float64` types without `crd:allowDangerousTypes=true`. Revisit if controller-gen adds native float support or if string parsing becomes a pain point.

- **Cost enforcement**: `max_cost_per_run` defines a USD budget cap, but the controller doesn't yet enforce it. Needs integration with LLM provider billing/usage APIs to track spend mid-run and terminate when exceeded.

- **Token/timeout cap enforcement**: `max_tokens_per_run` and `max_timeout_seconds` define repo-level ceilings. The controller needs to enforce `min(agent.limits, repo.settings.ai.max*)` at reconciliation time. Not yet implemented.

### MCP Server Catalog (Part 1b — types implemented)

- **Image vs Command mutual exclusivity**: `MCPServerSpec` has both `image` (sidecar container) and `command` (stdio process) fields that are mutually exclusive. Currently documented in comments only — validation should be enforced in the controller, not via CEL schema rules.

- **MCP server health checks**: No liveness/readiness probe configuration for MCP server sidecars. May need this for production reliability.

- **MCP server resource limits**: No CPU/memory resource requests/limits for MCP server containers. Important for multi-tenant clusters.

- **MCP transport configuration**: Only sidecar (image) and stdio (command) transports are supported. SSE and streamable HTTP transports are not yet modeled.

- **MCP server versioning**: No way to pin MCP server versions or declare compatibility constraints. Image tags can drift.

### Network Policy (Part 1c — types implemented)

- **Port ranges**: `EgressRule.Ports` is `[]int` — no support for port ranges (e.g. 8000-9000). Add if needed.

- **Protocol specification**: No way to specify TCP vs UDP on egress rules. Defaults to TCP implicitly.

- **Ingress rules**: Only egress is modeled. Ingress may be needed for MCP servers that expose HTTP endpoints to the agent.

- **NetworkPolicy enforcement**: The controller doesn't yet translate these rules into Kubernetes NetworkPolicy resources or equivalent sandbox network configuration.

- **Implicit LLM provider egress**: For `restricted` and `air-gapped` presets, the controller must auto-inject an egress rule for the LLM provider endpoint (resolved from `AIConfig.BaseURL` or the provider's default API host). Without this, the agent can't reach the model. The intent is documented in the type comments but enforcement is not yet implemented.

---

## Agent Definition (Part 2 — types implemented)

- **Harness selection**: Currently hardcoded to OpenHands. The design defers harness selection (OpenHands, Claude Code, custom) to a later phase.

- **Agent-level resource limits**: Agent `limits` are bounded by repo caps, but the enforcement logic (`min(agent.limits, repo.settings.ai.max*)`) isn't implemented yet.

- **Tool validation**: Agent tool references (`mcp_servers`, `allowed`) are not validated against the Repository's MCP server catalog at parse time. Invalid references will only fail at reconciliation.

---

## AgentRun (Part 3 — types implemented)

- **Knowledge graph context**: Removed from AgentRun spec. Will be re-added when the KG integration is redesigned.

- **Instruction content resolution**: Instructions are referenced by path/URL in the spec, but the adapter doesn't yet resolve them (fetch file contents, download remote URLs) at AgentRun creation time.

- **Changed files enrichment**: `AgentRunEventInfo.ChangedFiles` requires an extra API call to the git provider to list files in a PR/push. Not yet implemented in the provider layer.

- **PR/comment event enrichment**: `PullRequestInfo` and `CommentInfo` fields exist on the AgentRun event type, but the adapter doesn't yet populate them from the provider's parsed event data. The `provider.Event` struct already has `PullRequestNumber`, `PullRequestTitle`, `CommentBody`, etc. — just needs to be wired through.

- **Labels enrichment**: `AgentRunEventInfo.Labels` requires fetching the current label set from the git provider. Not yet implemented.

- **Cost tracking**: `AgentRunStatus.CostUSD` field exists but the controller doesn't yet compute or populate it. Requires integration with LLM provider usage APIs.

- **Consumer updates**: `pkg/agentharness/openhands.go` still references the removed `AgentRunContext` type. Needs to be updated to use the new AgentRun spec (system_prompt + instructions).

---

## Template Variables (Part 4 — implemented)

PaC-style template variables (`{{ variable_name }}`) in system prompts. The adapter resolves variables at AgentRun creation time by fetching data from the provider API and substituting it into the prompt. See the [redesign doc](agentrun-api-redesign.md) for the full variable list and design.

Variables serve a dual purpose: data injection (resolved value goes into the prompt) and sandbox signaling (controller infers what to provision — e.g. `{{ repo_clone_path }}` triggers a repo clone, `{{ pull_request_diff }}` triggers a diff fetch).

### Decisions made

- **Variable parser**: Simple regex substitution (`{{([^}]{2,})}}`, same as PaC). No CEL, no Go `text/template`. Flat variable name lookup only.

- **Provider API integration**: 8 new methods on `provider.Interface` (`GetPullRequestDescription`, `GetPullRequestDiff`, `GetPullRequestReviews`, `GetPullRequestComments`, `GetIssueTitle`, `GetIssueBody`, `GetIssueComments`, `GetIssueLabels`). GitHub provider implemented with go-github v84.

- **Size limits**: No truncation. Let the model's context window be the natural limit.

- **Error handling**: Empty string + log warning for unresolvable variables. Agent still runs.

- **Caching**: `VariableResolver` caches provider API results per variable name. Lazy fetching — only calls provider methods for variables actually present in the template.

### Items deferred

- **Instruction content resolution**: Template variables in instruction file content (from `agent.tekton.dev/instruction-N` annotations) are not yet resolved — instruction fetching itself is not implemented yet.

- **Sandbox signaling**: The controller does not yet infer sandbox provisioning from variable usage (e.g. `{{ repo_clone_path }}` should trigger a repo clone). This belongs in the AgentRun reconciler.

---

## Result Hooks (Part 5 — designed, not started)

Controller-mediated actions that let agents produce structured output without needing direct MCP tool access to the git provider. The agent writes its intent as structured data; the controller executes it using the Repository CR's git provider credentials. See the [redesign doc](agentrun-api-redesign.md) for the full design.

This is the complement to template variables: variables handle the read path (controller fetches context for the agent), result hooks handle the write path (controller acts on agent output). Together, they enable a fully functional agent with zero MCP tools.

### Items to implement

- **Result format**: The current `agentharness.Result` struct has `Comment`, `PRTitle`, `PRBody`. This needs to be expanded to support reviews (with per-file comments), labels, status checks. Need to define a structured JSON schema the agent outputs.

- **Result collection**: The runtime writes to `/output/result.json`. The controller reads this file after Pod completion. Need to handle the case where the runtime crashes before writing the result file.

- **Multiple actions per result**: An agent might want to post a comment AND add a label AND set a status check in one run. The result format should support a list of actions.

- **Partial failure**: If posting a comment succeeds but adding a label fails, how is this reported in AgentRunStatus? Each action should have its own success/failure status.

- **Interaction with MCP tools**: An agent could have both MCP tools and result hooks. The controller should not duplicate actions the agent already performed via MCP. May need to track which actions came from which path in `AgentRunStatus.Actions`.

---

## Execution Model (Part 6 — types implemented)

Every agent runs in an isolated sandbox (K8s SIG Agent Sandbox). The `SandboxTemplate` is configured on the Repository CR via `RuntimeConfig`. The controller creates sandboxes, writes config, runs the agent, reads results, and executes result actions via the agent-sandbox SDK. OpenHands has been removed (`pkg/agentharness/openhands.go` deleted) — the execution model is generic and runtime-agnostic.

### Items to implement

- **Reference runtime image**: The runtime contract is defined (input at `/etc/aac/config.json`, output at `/output/result.json`), but no reference implementation exists yet. Need to build a thin runtime that reads the config, runs an agentic LLM loop, and writes structured results.

- **MCP server communication**: Image-based MCP servers likely expose HTTP on localhost; command-based ones use stdio. The runtime config should specify the transport per server. The current `MCPServerSpec` doesn't have a transport field.

- **Repo clone in sandbox**: When `agent.tekton.dev/clone-repo: "true"` is set, the controller needs to clone the repo into the sandbox at the right SHA/branch using git credentials from the Repository CR. Could use `WriteFile` + `Run` (write a clone script, execute it) or a dedicated mechanism.

- **Sandbox resource limits**: Resource requests/limits for sandboxes are configured on the SandboxTemplate. May need to expose overrides on `RuntimeConfig` for multi-tenant clusters.

- **Sandbox cleanup policy**: When to destroy completed sandboxes? Immediately after result collection, after a TTL, or keep for debugging? Probably configurable on the Repository CR.

- **Streaming vs batch results**: Currently designed as batch (wait for sandbox completion, read result file). Streaming (controller processes actions as the agent produces them) would give better UX but is more complex.

---

## Knowledge Graph (deferred entirely)

The KG feature (automatic code graph via graphify, context filtering, per-branch PVs) is deferred. The types remain on the Repository CR (`KnowledgeGraphSpec`, `KGStatus`, etc.) but KG is not presented as a core concept. The KG controller exists but is not exercised by the agent execution path.

### Why deferred

- The agent execution model shifted to template variables + MCP tools for context injection, which covers the immediate use cases without a KG
- Graphify quality on Go repos has known weaknesses (node deduplication, fragmented communities)
- The context filtering pipeline (seed extraction → strategy selection → traversal → truncation) adds significant complexity for unclear benefit until the KG quality improves

### Items to revisit

- **KG as an MCP server**: Instead of the controller injecting KG context into the prompt, expose the KG as an MCP server that the agent can query. This is more flexible — the agent decides what context it needs, not the controller.

- **KG-powered template variables**: Variables like `{{ related_code }}` or `{{ architectural_context }}` that are backed by KG queries rather than provider API calls.

- **Remove KG types from Repository CR**: If KG is not coming back soon, remove the types to simplify the API surface. Currently keeping them since they're already implemented and don't cause harm.

---

## Security Policy (Part 1d — deferred entirely)

Skipped for MVP. The agent can use all MCP tools available in the catalog without restriction.

### Why deferred

There are two models for git operations — MCP-mediated (agent calls GitHub directly via MCP server with a token) and controller-mediated (agent produces a result, controller calls GitHub). A git security policy only enforces on the controller-mediated path. If the agent has an MCP server with a GitHub token, it can bypass controller-level git permissions entirely.

A meaningful security policy needs to unify both paths, likely via per-MCP-server permission constraints on the Repository CR (infra team controls which tools each MCP server exposes, agents select from within those bounds). This needs more design work.

### Items to revisit

- **Per-MCP-server permissions**: Extend `MCPServerSpec` with an allowed/denied tool list controlled by the infra team. Agent tool selection becomes an intersection: `agent.allowed ∩ repo.mcp_permissions.allowed - repo.mcp_permissions.denied`.

- **Protected branches**: Whether as a standalone concept or part of MCP permissions, infra team needs a way to prevent agents from pushing to certain branches.

- **Sandbox security** (readOnly, allowShell, runAsUser): These map to Kubernetes pod security context and are independent of the MCP question. Could be implemented separately when sandbox hardening is needed.

- **Token scoping is the real enforcement**: Given an MCP server token, the model can do anything the token allows regardless of any policy we define. Document this clearly — infra teams should use least-privilege tokens (e.g. read-only PATs) rather than relying on software-level permission lists.

---

## Cross-cutting

- **Webhook/CRD migration**: The v1alpha1 API is changing significantly. No migration strategy for existing CRDs — acceptable since this is alpha and there are no production users yet.

- **Audit logging**: `AgentRunStatus.Actions` records what agents did, but there's no structured audit log beyond the CR status.

- **Multi-cluster**: All resources are namespace-scoped. No cross-cluster or federated model.

- **snake_case convention**: All JSON tags use snake_case. This deviates from Kubernetes convention (camelCase) but is consistent across the entire API surface. If upstream tooling breaks on snake_case, may need to reconsider.
