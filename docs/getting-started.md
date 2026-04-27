# Getting Started with Agents as Code (AAC)

This guide walks you through deploying AAC on a local kind cluster, pointing it at a GitHub repository, and watching it receive webhook events and match agent definitions.

## Prerequisites

Install the following tools:

| Tool | Install |
|------|---------|
| [kind](https://kind.sigs.k8s.io/docs/user/quick-start/#installation) | `go install sigs.k8s.io/kind@latest` |
| [ko](https://ko.build/install/) | `go install github.com/ko-build/ko@latest` |
| [gosmee](https://github.com/chmouel/gosmee) | `go install github.com/chmouel/gosmee@latest` |
| [kubectl](https://kubernetes.io/docs/tasks/tools/) | See docs |
| Docker | Running and accessible |

You also need a GitHub repository where you can configure a webhook.

## 1. Create a Smee Channel

Smee proxies GitHub webhook deliveries to your local machine. Create a channel:

```bash
curl -s https://hook.pipelinesascode.com -o /dev/null -w '%{redirect_url}'
```

Save the URL it prints — you'll use it as the webhook payload URL and in your `.env`.

## 2. Create a GitHub Webhook

Go to your repository's **Settings > Webhooks > Add webhook** and configure:

| Field | Value |
|-------|-------|
| Payload URL | The smee URL from step 1 |
| Content type | `application/json` |
| Secret | A strong random string (save it for `.env`) |

Under **"Which events would you like to trigger this webhook?"**, select **"Let me select individual events"** and check:

- **Pushes**
- **Pull requests**
- **Pull request reviews**
- **Issue comments**
- **Issues**

These map to AAC's supported trigger types:

| GitHub event | Supported actions | AAC trigger |
|---|---|---|
| Pushes | all | `push` |
| Pull requests | opened, synchronize, reopened | `pull_request` |
| Pull requests | labeled | `pull_request_labeled` |
| Pull request reviews | all | `pull_request_review` |
| Issue comments | created | `issue_comment` |
| Issues | labeled | `issues_labeled` |

## 3. Create a GitHub Personal Access Token

The token is used by AAC to fetch agent definitions from the repository and check commenter permissions.

**Classic token** — create at **Settings > Developer settings > Personal access tokens > Tokens (classic)**:

| Scope | Why |
|-------|-----|
| `repo` | Read contents, create branches and PRs, check collaborator permissions |

**Fine-grained token** — create at **Settings > Developer settings > Personal access tokens > Fine-grained tokens**, scoped to your repository:

| Permission | Access | Why |
|------------|--------|-----|
| Contents | Read & Write | Fetch `.tekton/agents/` YAML files, create branches and push commits |
| Pull requests | Read & Write | Create and update pull requests |
| Issues | Read & Write | Post comments and add labels on issues and PRs |
| Metadata | Read | Required by all fine-grained tokens |
| Administration | Read | Check collaborator permission level for comment-triggered agents |

## 4. Configure .env

```bash
cp .env.example .env
```

Fill in the values:

```bash
# The smee URL from step 1
AAC_SMEEURL=https://hook.pipelinesascode.com/aBcDeF

# The webhook secret you set in step 2
GITHUB_WEBHOOK_SECRET=your-webhook-secret

# Your repository
REPO_URL=https://github.com/your-org/your-repo

# A GitHub PAT with repo scope
GITHUB_TOKEN=ghp_xxxxxxxxxxxxxxxxxxxx
```

The `GITHUB_APP_ID` and `GITHUB_PRIVATE_KEY_PATH` fields are only needed if you're using a GitHub App instead of a PAT. Leave them empty for webhook+PAT setups.

To enable AI agents, also set:

```bash
AI_ENABLED=true
AI_PROVIDER=gemini
AI_API_KEY=your-api-key
```

## 5. Deploy

```bash
# Full setup: kind cluster + nginx + AAC + secrets
make kind-setup

# Create the Repository CR
make setup-repo
```

This creates:
- A kind cluster named `aac`
- nginx ingress controller
- AAC controller and webhook deployments in `agents-as-code-system`
- An `agents-as-code-github-app` secret with your webhook secret
- A Repository CR pointing at your repo with git token and webhook secrets
- An AI API key secret (if `AI_ENABLED=true`)

## 6. Start the Webhook Forwarder

In a separate terminal:

```bash
gosmee client --saveDir /tmp/replays $AAC_SMEEURL http://webhook.aac-127-0-0-1.nip.io
```

This forwards GitHub webhook deliveries from your smee channel to the AAC webhook running in kind. The `--saveDir` flag saves replays so you can re-send events later without triggering them on GitHub again.

## 7. Add Agent Definitions to Your Repository

Create `.tekton/agents/` in your repository and add agent definition YAML files. Here's a triaging agent that fires on new pull requests and issue comments:

```yaml
# .tekton/agents/triage.yaml
apiVersion: agent.tekton.dev/v1alpha1
kind: Agent
metadata:
  name: triage
spec:
  purpose: >
    Triage incoming pull requests and issues. Label PRs by area
    (bug, feature, docs, tests), assess complexity, identify reviewers
    based on changed files, and post a summary comment.
  triggers:
    - event: pull_request
      branches:
        - main
        - "release-*"
    - event: issue_comment
      match: "/triage"
  limits:
    maxTokens: 8000
    timeoutSeconds: 120
```

This agent triggers on:
- **Pull requests** targeting `main` or `release-*` branches
- **Issue comments** containing `/triage`

You can add more agents in separate files. Each `.yaml` or `.yml` file in `.tekton/agents/` is discovered independently.

### More Examples

A review agent that runs on PR review events:

```yaml
# .tekton/agents/reviewer.yaml
apiVersion: agent.tekton.dev/v1alpha1
kind: Agent
metadata:
  name: reviewer
spec:
  purpose: >
    Review pull request code changes for bugs, security issues,
    and adherence to project conventions.
  triggers:
    - event: pull_request
      branches:
        - main
    - event: issue_comment
      match: "/review"
  limits:
    maxTokens: 16000
    timeoutSeconds: 300
```

A labeling agent that reacts to a specific issue label:

```yaml
# .tekton/agents/labeler.yaml
apiVersion: agent.tekton.dev/v1alpha1
kind: Agent
metadata:
  name: labeler
spec:
  purpose: >
    When an issue is labeled 'needs-investigation', analyze the issue
    description, find related code, and post an initial investigation comment.
  triggers:
    - event: issues_labeled
      match: needs-investigation
  limits:
    maxTokens: 8000
    timeoutSeconds: 120
```

An agent that triggers when a PR is labeled for auto-merge:

```yaml
# .tekton/agents/auto-merge.yaml
apiVersion: agent.tekton.dev/v1alpha1
kind: Agent
metadata:
  name: auto-merge
spec:
  purpose: >
    When a PR is labeled 'ready-to-merge', verify all checks passed,
    confirm approvals, and post a merge-readiness summary.
  triggers:
    - event: pull_request_labeled
      match: ready-to-merge
  limits:
    maxTokens: 4000
    timeoutSeconds: 60
```

## 8. Test It

Trigger an event on your repository (open a PR, push a commit, or comment `/triage` on an issue) and watch the webhook logs:

```bash
make logs-webhook
```

You should see the event being received, parsed, the Repository CR matched, and agent definitions discovered and matched:

```
processing event  trigger=pull_request org=your-org repo=your-repo sha=abc123 sender=you
discovered agent definitions  count=1
agent matched  agent=triage purpose="Triage incoming pull requests..."  trigger_event=pull_request
```

## 9. Replaying Events

gosmee saves webhook payloads to `/tmp/replays`. To replay a saved event:

```bash
gosmee replay --targetURL http://webhook.aac-127-0-0-1.nip.io /tmp/replays/<event-file>.json
```

This is useful for iterating on agent definitions without triggering new events on GitHub.

## Day-to-Day Workflow

After the initial setup, you typically only need:

```bash
# Rebuild and redeploy after code changes
make kind-redeploy

# Tail logs
make logs-webhook
make logs-controller

# Restart pods without rebuilding
make kind-restart

# Reconfigure secrets/ingress without rebuilding
make kind-configure

# Tear down
make kind-delete
```

Run `make help` to see all available targets.

## Trigger Fields Reference

| Field | Description | Example |
|-------|-------------|---------|
| `event` | Required. One of: `push`, `pull_request`, `pull_request_review`, `issue_comment`, `issues_labeled`, `pull_request_labeled`. | `pull_request` |
| `branches` | Optional. Glob patterns for target branch filtering. | `["main", "release-*"]` |
| `match` | Optional. For `issue_comment`: matches if the comment body contains this string. For `issues_labeled` / `pull_request_labeled`: matches if the label name equals this string exactly. | `"/triage"`, `"bug"` |
