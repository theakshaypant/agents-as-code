package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	duckv1 "knative.dev/pkg/apis/duck/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// AgentRun is an execution instance spawned when a git event matches an
// Agent definition. Created automatically by the webhook handler, reconciled
// by the AgentRun controller which provisions a sandbox and runs the agent.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=ar
// +kubebuilder:printcolumn:name="Agent",type=string,JSONPath=`.spec.agent_ref`
// +kubebuilder:printcolumn:name="Repository",type=string,JSONPath=`.spec.repository_ref`
// +kubebuilder:printcolumn:name="Status",type=string,JSONPath=`.status.conditions[-1:].type`
type AgentRun struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   AgentRunSpec   `json:"spec"`
	Status AgentRunStatus `json:"status,omitempty"`
}

type AgentRunSpec struct {
	// AgentRef is the name of the matched Agent definition.
	// +kubebuilder:validation:Required
	AgentRef string `json:"agent_ref"`

	// RepositoryRef is the name of the Repository CR in the same namespace.
	// +kubebuilder:validation:Required
	RepositoryRef string `json:"repository_ref"`

	// SystemPrompt is the agent's core instructions, copied from the Agent
	// definition at creation time so the reconciler can construct the task
	// without re-fetching agent YAML from the git provider.
	// +kubebuilder:validation:Required
	SystemPrompt string `json:"system_prompt"`

	// Instructions are resolved instruction file references.
	// Repo paths and remote URLs from Agent annotations are resolved at
	// AgentRun creation time.
	// +optional
	Instructions []InstructionRef `json:"instructions,omitempty"`

	// Tools is the resolved tool configuration, copied from the Agent definition.
	// MCPServer names reference entries in the Repository catalog.
	// +optional
	Tools *AgentToolsSpec `json:"tools,omitempty"`

	// Event describes the git event that triggered this run.
	// +kubebuilder:validation:Required
	Event AgentRunEventInfo `json:"event"`

	// Limits are the resolved execution constraints.
	// Computed as min(Agent.limits, Repository.settings.ai.max*).
	// +kubebuilder:validation:Required
	Limits AgentLimits `json:"limits"`
}

type InstructionRef struct {
	// Name is a human-readable identifier for this instruction source.
	// +optional
	Name string `json:"name,omitempty"`

	// Path is a repo-relative file path.
	// Mutually exclusive with URL.
	// +optional
	Path string `json:"path,omitempty"`

	// URL is a remote HTTP(S) URL.
	// Mutually exclusive with Path.
	// +optional
	URL string `json:"url,omitempty"`

	// Content is the resolved instruction text.
	// Populated at AgentRun creation time by the adapter.
	// +optional
	Content string `json:"content,omitempty"`
}

type AgentRunEventInfo struct {
	// Type is the git event type (e.g. "issue_comment", "pull_request", "push").
	// +kubebuilder:validation:Required
	Type string `json:"type"`

	// Action is the event action (e.g. "opened", "synchronize", "created").
	// +optional
	Action string `json:"action,omitempty"`

	// SHA is the commit SHA associated with the event.
	// +optional
	SHA string `json:"sha,omitempty"`

	// Branch is the target branch.
	// +optional
	Branch string `json:"branch,omitempty"`

	// Sender is the user who triggered the event.
	// +kubebuilder:validation:Required
	Sender string `json:"sender"`

	// URL is a deep link to the triggering event (comment, PR, commit).
	// +optional
	URL string `json:"url,omitempty"`

	// IssueNumber is the issue number (for issue_comment and issues_labeled events).
	// +optional
	IssueNumber int `json:"issue_number,omitempty"`

	// PullRequest contains PR-specific details.
	// +optional
	PullRequest *PullRequestInfo `json:"pull_request,omitempty"`

	// Comment contains the comment body for comment events.
	// +optional
	Comment *CommentInfo `json:"comment,omitempty"`

	// Labels on the issue/PR at event time.
	// +optional
	Labels []string `json:"labels,omitempty"`

	// ChangedFiles lists files affected by this event.
	// +optional
	ChangedFiles []string `json:"changed_files,omitempty"`
}

type PullRequestInfo struct {
	// Number is the PR number.
	// +kubebuilder:validation:Required
	Number int `json:"number"`

	// Title is the PR title.
	// +optional
	Title string `json:"title,omitempty"`

	// HeadBranch is the source branch of the PR.
	// +optional
	HeadBranch string `json:"head_branch,omitempty"`

	// BaseBranch is the target branch of the PR.
	// +optional
	BaseBranch string `json:"base_branch,omitempty"`
}

type CommentInfo struct {
	// Body is the comment text.
	// +kubebuilder:validation:Required
	Body string `json:"body"`
}

type AgentRunStatus struct {
	duckv1.Status `json:",inline"`

	// StartTime is when the AgentRun began executing.
	// +optional
	StartTime *metav1.Time `json:"start_time,omitempty"`

	// CompletionTime is when the AgentRun finished.
	// +optional
	CompletionTime *metav1.Time `json:"completion_time,omitempty"`

	// TokensUsed is the total LLM tokens consumed during execution.
	// +optional
	TokensUsed int `json:"tokens_used,omitempty"`

	// CostUSD is the total cost of the run in USD (e.g. "0.42").
	// +optional
	CostUSD string `json:"cost_usd,omitempty"`

	// Actions records what the agent did for audit.
	// +optional
	Actions []AgentAction `json:"actions,omitempty"`

	// SandboxName tracks which sandbox instance executed this run.
	// +optional
	SandboxName string `json:"sandbox_name,omitempty"`

	// ExecID is the execution identifier within the sandbox.
	// +optional
	ExecID string `json:"exec_id,omitempty"`
}

type AgentAction struct {
	// Type of action taken (pr-comment, commit, status-check, create-pr, label).
	Type string `json:"type"`

	// URL of the resulting resource (e.g. comment URL, PR URL).
	// +optional
	URL string `json:"url,omitempty"`

	// SHA of the resulting commit, if applicable.
	// +optional
	SHA string `json:"sha,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// AgentRunList is a list of AgentRun resources.
// +kubebuilder:object:root=true
type AgentRunList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []AgentRun `json:"items"`
}
