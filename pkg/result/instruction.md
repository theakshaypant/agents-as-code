# Result Hooks — Structured Output Format

Your final response must be a structured Result object with an `actions` array. The controller will execute the actions you specify on your behalf. You do not need to call any APIs directly — just describe what you want to happen. You MUST include at least one action in your response.

## Format

```json
{
  "actions": [
    { ... action objects ... }
  ],
  "tokens_used": 0,
  "cost_usd": "0.00"
}
```

The `actions` array contains one or more action objects. Each action has a `type` field and type-specific fields.

## Action Types

### comment — Post a PR or issue comment

```json
{"type": "comment", "body": "Your comment text here."}
```

### review — Submit a pull request review

```json
{
  "type": "review",
  "event": "COMMENT",
  "body": "Overall review summary.",
  "comments": [
    {"path": "pkg/api/user.go", "line": 42, "body": "Potential nil dereference here."},
    {"path": "pkg/api/user.go", "line": 58, "body": "Missing error check."}
  ]
}
```

- `event` must be one of: `COMMENT`, `APPROVE`, `REQUEST_CHANGES`
- `body` is the overall review summary (optional for COMMENT)
- `comments` are inline comments on specific files and lines (optional)

### label — Add or remove labels

```json
{"type": "label", "add": ["bug", "needs-fix"], "remove": ["needs-triage"]}
```

- At least one of `add` or `remove` must be provided.

### create-pr — Create a new pull request

```json
{
  "type": "create-pr",
  "title": "Fix authentication bug",
  "body": "This PR fixes the login flow by...",
  "head": "fix/auth-bug",
  "base": "main"
}
```

- `title`, `head`, and `base` are required.
- `body` is optional.

### status — Set a commit status check

```json
{
  "type": "status",
  "context": "agent/code-review",
  "state": "success",
  "description": "Code review passed with no issues.",
  "target_url": "https://example.com/details"
}
```

- `context` and `state` are required.
- `state` must be one of: `success`, `failure`, `error`, `pending`
- `description` and `target_url` are optional.

### commit — Create a multi-file commit on the PR branch

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

- `message` is required and will be the commit message.
- `files` is a map of file paths (relative to repository root) to their complete content.
- Files are created if they don't exist, or updated if they do.
- The commit is always made on the PR's head branch.
- File paths must be relative (no leading `/`) and cannot contain `..`.

## Complete Example

```json
{
  "actions": [
    {
      "type": "review",
      "event": "REQUEST_CHANGES",
      "body": "Found 2 issues that need to be addressed.",
      "comments": [
        {"path": "pkg/api/handler.go", "line": 23, "body": "This endpoint is missing authentication."},
        {"path": "pkg/api/handler.go", "line": 45, "body": "SQL injection risk — use parameterized queries."}
      ]
    },
    {
      "type": "label",
      "add": ["needs-changes"],
      "remove": ["ready-for-review"]
    },
    {
      "type": "status",
      "context": "agent/security-review",
      "state": "failure",
      "description": "Security issues found."
    }
  ]
}
```

You can include multiple actions in a single result. All actions will be executed in order.
