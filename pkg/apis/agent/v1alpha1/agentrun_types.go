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
// +kubebuilder:printcolumn:name="Agent",type=string,JSONPath=`.spec.agentRef`
// +kubebuilder:printcolumn:name="Repository",type=string,JSONPath=`.spec.repositoryRef`
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
	AgentRef string `json:"agentRef"`

	// RepositoryRef is the name of the Repository CR in the same namespace.
	// +kubebuilder:validation:Required
	RepositoryRef string `json:"repositoryRef"`

	// Purpose is the agent's purpose, copied from the Agent definition
	// at creation time so the reconciler can construct the task without
	// re-fetching agent YAML from the git provider.
	// +kubebuilder:validation:Required
	Purpose string `json:"purpose"`

	// Event describes the git event that triggered this run.
	// +kubebuilder:validation:Required
	Event AgentRunEventInfo `json:"event"`

	// Limits constrains execution, copied from the Agent definition.
	// +kubebuilder:validation:Required
	Limits AgentLimits `json:"limits"`

	// Context holds the filtered KG subgraph metadata.
	// Nil when context filtering is not yet performed.
	// +optional
	Context *AgentRunContext `json:"context,omitempty"`
}

type AgentRunEventInfo struct {
	// Type is the git event type (e.g. "issue_comment", "pull_request", "push").
	// +kubebuilder:validation:Required
	Type string `json:"type"`

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
}

type AgentRunContext struct {
	// SubgraphRef is the path to the filtered KG subgraph file.
	// +optional
	SubgraphRef string `json:"subgraphRef,omitempty"`

	// TokenCount is the token count of the filtered subgraph.
	// +optional
	TokenCount int `json:"tokenCount,omitempty"`

	// SeedNodes are the KG nodes used as traversal starting points.
	// +optional
	SeedNodes []string `json:"seedNodes,omitempty"`

	// Strategy is the graph traversal strategy used (bfs, dfs, community, impact).
	// +optional
	Strategy string `json:"strategy,omitempty"`

	// Depth is the max hops from seed nodes used during traversal.
	// +optional
	Depth int `json:"depth,omitempty"`
}

type AgentRunStatus struct {
	duckv1.Status `json:",inline"`

	// StartTime is when the AgentRun began executing.
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// CompletionTime is when the AgentRun finished.
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// TokensUsed is the total LLM tokens consumed during execution.
	// +optional
	TokensUsed int `json:"tokensUsed,omitempty"`

	// Actions records what the agent did for audit.
	// +optional
	Actions []AgentAction `json:"actions,omitempty"`

	// SandboxName tracks which sandbox instance executed this run.
	// +optional
	SandboxName string `json:"sandboxName,omitempty"`
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
