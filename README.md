# Agents as Code (AAC)

> **Experimental / Proof of Concept** — this project is a design exploration and not intended for production use.

Run AI agents on git events with repo-aware context.

## Why

Pipelines-as-Code (PaC) introduced AI/LLM-powered pipeline analysis as a tech preview feature — it can analyze pipeline failures using LLM providers and post root-cause analysis as PR comments. Products like Qodo Merge have shown that AI-powered developer workflows go far beyond failure analysis: automated PR descriptions, code review, inline suggestions, interactive Q&A, label generation — all driven by LLM analysis of code diffs and CI context.

PaC proposed an "AI Skills redesign" to move from a single-purpose failure analyzer to a skill-based platform. Skills are Markdown files in `.tekton/ai/` that define when to trigger, what context to assemble, what prompt to send, and where to post results. [Fullsend](https://github.com/fullsend-ai/fullsend) explored fully autonomous agentic development — agents with per-role GitHub App identities that handle triage, implementation, review, and merge, coordinating exclusively through GitHub primitives. Fullsend uses curated, human-written context (per-repo `CLAUDE.md`, bookmarks, org-level architecture docs) to give agents the understanding they need.

Each approach brings a different context model. PaC skills let users specify exactly which diffs, logs, and files to include. Fullsend relies on carefully curated documentation that humans write and maintain to reflect the codebase. AAC adds a complementary layer: a knowledge graph of the entire repository — automatically derived from the code itself — that captures structural relationships (call graphs, type hierarchies, package boundaries, community clusters) and updates incrementally on every push.

AAC is designed to integrate with PaC, reusing its adapter, provider, and Repository CR infrastructure. PaC skills and AAC agents can coexist on the same repository — skills handle lightweight prompt-driven tasks (formatting pipeline output, generating descriptions), while AAC agents handle tasks that benefit from deeper structural context. AAC also builds on PaC's multi-provider foundation, supporting GitHub, GitLab, Bitbucket, and Gitea/Forgejo with multiple LLM providers — configured once on the Repository CR, shared by all agents.

## Comparison

| | Qodo Merge | PaC AI Skills | Fullsend | AAC |
|---|---|---|---|---|
| Context model | Code diff | Code diff + pipeline logs | Human-curated docs | Knowledge graph (auto-derived) |
| Definition | SaaS config | Markdown in `.tekton/ai/` | Org-level config repo | YAML in `.tekton/agents/` |
| Git providers | GitHub | GitHub, GitLab, Bitbucket, Gitea | GitHub | GitHub, GitLab, Bitbucket, Gitea |
| LLM providers | Built-in | Configurable | Claude | Configurable |
| Execution | SaaS | Inline in PaC process | Per-role GitHub App | Isolated sandbox per agent |
| Audit | PR comments | PR comments | Git actions | AgentRun CR |
| Strength | Zero setup, polished UX | Pipeline-aware, multi-provider | Full autonomy, security-first | Structural context, declarative |

## How It Works

1. Define agents as YAML files in `.tekton/agents/` in your repository
2. Configure a Repository CR pointing at your repo
3. When a git event matches an agent's triggers, AAC spawns a sandboxed execution with filtered knowledge graph context

```yaml
# .tekton/agents/triage.yaml
apiVersion: agent.tekton.dev/v1alpha1
kind: Agent
metadata:
  name: triage
spec:
  purpose: >
    Triage incoming pull requests. Label by area,
    assess complexity, and identify reviewers.
  triggers:
    - event: pull_request
      branches:
        - main
    - event: issue_comment
      match: "/triage"
  limits:
    maxTokens: 8000
    timeoutSeconds: 120
```

Agents describe their purpose in natural language. AAC infers the right knowledge graph filtering strategy — no manual context configuration needed.

## Supported Triggers

| Trigger | Description |
|---------|-------------|
| `push` | Code pushed to a branch |
| `pull_request` | PR opened, synchronized, or reopened |
| `pull_request_review` | PR review submitted |
| `issue_comment` | Comment on an issue or PR |
| `issues_labeled` | Label added to an issue |
| `pull_request_labeled` | Label added to a PR |

## Documentation

- [Getting Started](docs/getting-started.md) — deploy AAC locally with kind and test it end-to-end
- [Design](docs/design.md) — architecture, CRDs, knowledge graph, and context filtering
