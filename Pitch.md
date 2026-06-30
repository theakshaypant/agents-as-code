# Why Do We Need an AgentRun?

## Introduction
The current implementation is heavily influenced by a single use-case - getting Fullsend functionality in pipelines-as-code (PAC), hence the name Agents-as-Code (AAC). However, there are wider use cases in the tekton ecosystem beyond triggering agents on VCS.
In this doc, we try to explore some other use case and a "re-do" of the design for the "AgentRun" paradigm.

As a first step, we separate out the Agents from VCS, we introduce a separate agent controller. Having this agent controller, allows the AgentRun to mingle with pipeline artifacts other than code on SCM.
Instead of having AgentRun being an independent entity, it could be seen at the same level as a "Task" in the tekton ecosystem.
Agents can be run before or after or in parallel with other tasks or even agents. For simplicity, we'll consider running Agents after a task which can consume the results from an earlier task. 

## Use cases

### Agent in Release Pipeline - Konflux Integration
An "Agent" can be appended to a release pipeline which takes the results of other tasks in the pipeline as an input (use in system prompt). Such a paradigm can be helpful for engineers working on a release to get a quick summary of why a release pipeline failed and recommended steps.
Given that we are retaining the network policies, tools/MCP access features of AAC, we can restrict the model from accessing resources it shouldn't and focus solely on the provided inputs such as logs from a failed task.

A concrete Konflux use case: **categorizing failures as flakes and automatically retriggering builds**. Konflux development teams already deal with high volumes of pipeline runs, and a significant chunk of failures are transient — network timeouts, registry blips, resource pressure. An AgentRun that can distinguish a genuine code failure from a flake and comment `/retest` on the merge request would save substantial engineer time. Teams are already scripting this kind of thing manually; a first-class AgentRun makes it auditable, constrained, and consistent.

### [Lightspeed](https://github.com/openshift/lightspeed-service) Integration
Lightspeed deployed in OpenShift clusters already has built-in knowledge about OpenShift and Kubernetes. Rather than requiring teams to provision credentials for an external LLM provider, an AgentRun can use Lightspeed as its model backend. This keeps inference on-cluster, avoids routing sensitive pipeline data to external APIs, and leverages a model that already understands the platform the pipelines run on.

### Signing LM Output using Chains
_[Does this make sense?]_

Tekton Chains already signs TaskRun and PipelineRun provenance. If an AgentRun is a first-class citizen at the same level as a Task, Chains can capture and sign its output - the analysis, the recommendation, the generated code. This gives downstream consumers a cryptographic guarantee that the agent output came from a known pipeline, with a known model, under known constraints.

### Maintaining a Knowledge Graph using Results
_[Does this make sense?]_

Tekton Results persists PipelineRun/TaskRun data to long-term storage. An AgentRun can consume this history and maintain a knowledge graph — correlating failures across runs, tracking which parameters or environments tend to break things. This applies to any pipeline, not just code-based ones: release pipelines, infra provisioning, ML training jobs all produce the same structured Results data.

This also doubles as a memory layer for the agent. If a pipeline fails with the same root cause as last week, the agent should retrieve its prior conclusion rather than burn tokens re-deriving it. Use existing caching mechanism like `tekton-caches`. (?)

### CI/CD Failure Analysis
When a pipeline fails, someone has to read the logs. Often, the logs are verbose, the error is buried, and the fix is non-obvious. An AgentRun appended to a pipeline can consume the logs from failed tasks, correlate them with the diff that triggered the run, and post a concise root-cause analysis. This is especially valuable in multi-task pipelines where a failure in task N might be caused by a subtle output issue in task N-2.

This is the use case with the most immediate, tangible value. Engineers already spend significant time on exactly this — triaging failures, distinguishing flakes from real bugs, figuring out which task actually broke. An AgentRun doesn't need write access to anything to be useful here; read-only analysis of logs and results is enough to save hours per week across a team.

### Security Scan Interpretation
Security scanners (Trivy, Snyk, Grype) produce long vulnerability lists. Lot of it is noise — transitive dependencies, unexploitable paths, already-mitigated issues. An AgentRun can take a scan report as input, cross-reference it with the SBOM from Chains, and produce a prioritized summary: "3 of 47 CVEs are actually exploitable in your context." This turns a wall of CVEs into an actionable decision for the reviewer.

### Release Notes Generation
Release pipelines know exactly what changed — commits, merged PRs, closed issues. An AgentRun triggered on a tag push or release pipeline completion can synthesize this into structured release notes, grouping changes by category (features, fixes, breaking changes, security patches). The output can be committed to a CHANGELOG or published as a GitHub Release — no manual assembly required.

### Compliance and Audit Trail Enrichment
_[Does this make sense?]_

Tekton Chains provides raw attestations and signed provenance. An AgentRun could potentially consume these and generate human-readable compliance reports — bridging the gap between machine-generated attestations and what auditors actually need to see. 
Is this is a meaningful use case or if Chains already covers this?

### Flaky Test Detection
Tekton Results stores historical test outcomes. An AgentRun can query this history, detect non-deterministic test patterns (passes 60% of the time, only fails on Mondays, always fails after task X), and file issues with analysis attached. Instead of engineers manually tracking "I think this test is flaky," the agent quantifies it across the last N runs and recommends specific fixes.

### Pipeline Optimization
Pipeline execution data sits in Tekton Results — task durations, ordering, resource usage. An AgentRun can analyze the execution graph, identify serialized tasks that could run in parallel, flag redundant work, and estimate the speedup from restructuring. "Your 30-minute pipeline could be 12 minutes if you parallelize these 5 independent test tasks."

### AAC
Gitops based AgentRun of source code.

## Why Work on This

### Tekton has data but lacks the interpretation layer
Tekton pipelines already produce rich structured data: logs, metrics, provenance, etc. That data is consumed by dashboards or sits in storage. An AgentRun integrates into the existing ecosystem to turn passive data into active intelligence without requiring changes to the pipelines themselves.

### Agents belong at the Task level, not bolted on from outside
Running an agent as just another container in a pipeline misses the point. Agents need controlled tool access, network isolation, token budgets, and auditable output — none of which a generic container provides. By making AgentRun a peer of Task in the Tekton model, we get proper lifecycle management, limit enforcement, and integration with Chains/Results for free.

### Kubernetes is the right place to run agents
The industry is converging on purpose-built, hardened runtimes for AI agents — immutable base images, scoped API access, secrets injected at runtime rather than baked in, fine-grained network policies. Kubernetes already provides these primitives: pod security standards, network policies, admission controllers, image-based node management. Tekton, as a proven Kubernetes-native workflow manager, is the natural orchestration layer. Organizations get to run agents in their existing k8s infrastructure, managed by a workflow engine they already trust, without routing sensitive pipeline data to external services.

### The ecosystem is converging on this
Tekton joined CNCF as an incubating project. Pipelines-as-Code moved into the tektoncd org. The `tektoncd/mcp-server` repo already exists. The building blocks are in place, what's missing is the orchestration layer that connects LLM capabilities to pipeline context with the right guardrails.

### Every team is going to build this anyway
Teams are already duct-taping LLM calls into their pipelines with curl steps and custom scripts (Looking at Apple :eyes:). Without a standard, each team reinvents network isolation, prompt management, cost controls, and output handling. AgentRun provides the opinionated, secure-by-default path so teams don't have to solve these problems independently.

## Agentic PipelineRun Watcher

The use cases above focus on AgentRun as a unit of work inside a pipeline — triggered, scoped, and completed like a Task. But there's a complementary pattern within this system: a long-lived agentic watcher that sits outside individual pipeline runs and monitors them continuously.

Where an AgentRun is a discrete step in a pipeline, the watcher is a persistent deployment — part of the same AI-enabled Tekton system, running alongside the agent controller. It follows the observe-diagnose-remediate-verify loop that's emerging in the broader Kubernetes operations space:

1. **Observe** — Continuously monitor PipelineRun completions and failures. Read-only, no execution.
2. **Diagnose** — Correlate signals across tasks, classify failures (flake vs. real bug vs. infra issue), pull historical context from Results.
3. **Remediate** — Stage corrective actions within policy guardrails: comment `/retest` for known flakes, open a PR with a dependency bump for security scan failures, flag persistent regressions for human attention.
4. **Verify** — Confirm the remediation worked. If a retested pipeline passes, close the loop. If it fails again, escalate.

The adoption path should be incremental. Start read-only, move to approval-gated actions, then expand to autonomous handling of validated low-risk categories (retriggering known flakes). Full autonomous coverage comes last, gated by team confidence and policy governance.

The watcher and AgentRun are complementary pieces of the same system. The watcher can spawn AgentRuns for deeper analysis, and AgentRuns produce the structured output the watcher needs to make decisions. Both live under the agent controller's umbrella — one reactive (AgentRun, triggered within a pipeline), one proactive (the watcher, continuously monitoring across pipelines).
