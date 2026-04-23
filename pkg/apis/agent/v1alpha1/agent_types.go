package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Agent is a user-defined agent template describing what the agent does,
// which git events trigger it, and which LLM provider to use.
// Agent definitions live in .tekton/agents/ in the repository and are
// discovered by the controller on each git event.
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,shortName=agt
// +kubebuilder:printcolumn:name="Purpose",type=string,JSONPath=`.spec.purpose`
type Agent struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec AgentSpec `json:"spec"`
}

type AgentSpec struct {
	// Purpose is natural language describing what the agent does.
	// Drives automatic KG filtering strategy selection.
	// +kubebuilder:validation:Required
	Purpose string `json:"purpose"`

	// On defines which git events trigger this agent.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	On []AgentTrigger `json:"triggers"`

	// Limits constrains agent execution.
	// +kubebuilder:validation:Required
	Limits AgentLimits `json:"limits"`

	// TODO: Context filtering is not yet implemented. This field is reserved
	// for optional overrides of the purpose-inferred KG filtering strategy.
	// When present, its fields take precedence over the profile inferred
	// from Purpose. Partial overrides work (e.g. specifying only Depth
	// keeps the inferred strategy but increases traversal depth).
	// +optional
	Context *AgentContext `json:"context,omitempty"`
}

type AgentTrigger struct {
	// Event is the git event type that triggers this agent.
	// Supported: push, pull_request, pull_request_review, issue_comment,
	// issues_labeled, pull_request_labeled.
	// +kubebuilder:validation:Required
	Event string `json:"event"`

	// Match triggers only when the comment body contains this string.
	// Only applicable to issue_comment events.
	// +optional
	Match string `json:"match,omitempty"`

	// Branches filters by target branch. Supports glob patterns (e.g. "release-*").
	// +optional
	Branches []string `json:"branches,omitempty"`
}

type AgentLimits struct {
	// MaxTokens is the maximum tokens per AgentRun.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=1
	MaxTokens int `json:"maxTokens"`

	// TimeoutSeconds is the maximum execution time.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=1
	TimeoutSeconds int `json:"timeoutSeconds"`
}

type AgentContext struct {
	// Strategy overrides the inferred traversal strategy (bfs, dfs, community, impact).
	// +optional
	Strategy string `json:"strategy,omitempty"`

	// Depth overrides the max hops from seed nodes.
	// +optional
	Depth int `json:"depth,omitempty"`

	// TokenBudget caps the context size.
	// +optional
	TokenBudget int `json:"tokenBudget,omitempty"`

	// NodeFilter limits which node kinds to include.
	// +optional
	NodeFilter []string `json:"nodeFilter,omitempty"`

	// EdgeFilter limits which relationships to traverse.
	// +optional
	EdgeFilter []string `json:"edgeFilter,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// AgentList is a list of Agent resources.
// +kubebuilder:object:root=true
type AgentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []Agent `json:"items"`
}
