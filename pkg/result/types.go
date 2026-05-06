package result

// Result represents the complete output from an agent execution.
// Agents write this as JSON to /output/result.json inside the sandbox.
type Result struct {
	Actions    []Action `json:"actions"`
	TokensUsed int      `json:"tokens_used,omitempty"`
	CostUSD    string   `json:"cost_usd,omitempty"`
}

// Action represents a single operation the agent wants the controller to perform.
// The Type field determines which other fields are relevant.
type Action struct {
	// Type identifies the action: comment, review, label, create-pr, status.
	Type string `json:"type"`

	// Body is used by comment and review actions.
	Body string `json:"body,omitempty"`

	// Event is the review verdict: COMMENT, APPROVE, REQUEST_CHANGES.
	Event string `json:"event,omitempty"`

	// Comments are inline code review comments (review action).
	Comments []ReviewComment `json:"comments,omitempty"`

	// Add lists labels to add (label action).
	Add []string `json:"add,omitempty"`

	// Remove lists labels to remove (label action).
	Remove []string `json:"remove,omitempty"`

	// Title is the PR title (create-pr action).
	Title string `json:"title,omitempty"`

	// Head is the source branch (create-pr action).
	Head string `json:"head,omitempty"`

	// Base is the target branch (create-pr action).
	Base string `json:"base,omitempty"`

	// Context is the status check name (status action).
	Context string `json:"context,omitempty"`

	// State is the status check state: success, failure, error, pending (status action).
	State string `json:"state,omitempty"`

	// Description is a short status check description (status action).
	Description string `json:"description,omitempty"`

	// TargetURL links to details for the status check (status action).
	TargetURL string `json:"target_url,omitempty"`

	// Message is the commit message (commit action).
	Message string `json:"message,omitempty"`

	// Files is a map of repo-relative file paths to their full content (commit action).
	Files map[string]string `json:"files,omitempty"`
}

// ReviewComment is an inline code review comment on a specific file and line.
type ReviewComment struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Body string `json:"body"`
}
