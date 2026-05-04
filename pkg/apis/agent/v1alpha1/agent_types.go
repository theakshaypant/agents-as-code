package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Agent is a user-defined agent template describing what the agent does,
// which git events trigger it, and which tools it uses.
// Agent definitions live in .tekton/agents/ in the repository and are
// discovered by the controller on each git event.
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,shortName=agt
// +kubebuilder:printcolumn:name="SystemPrompt",type=string,JSONPath=`.spec.system_prompt`
type Agent struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec AgentSpec `json:"spec"`
}

type AgentSpec struct {
	// SystemPrompt is the agent's core identity and behavioral instructions.
	// +kubebuilder:validation:Required
	SystemPrompt string `json:"system_prompt"`

	// Tools selects which MCP servers (from the Repository catalog) to use
	// and which specific tools to enable.
	// +optional
	Tools *AgentToolsSpec `json:"tools,omitempty"`

	// Limits constrains agent execution.
	// Bounded by Repository settings — agent can set lower, not higher.
	// +kubebuilder:validation:Required
	Limits AgentLimits `json:"limits"`
}

type AgentToolsSpec struct {
	// MCPServers references MCP servers by name from the Repository catalog.
	// +optional
	MCPServers []string `json:"mcp_servers,omitempty"`

	// Allowed is an allowlist of specific tools.
	// Format: "server:tool" or "server:*".
	// If empty, all tools from selected servers are available.
	// +optional
	Allowed []string `json:"allowed,omitempty"`
}

type AgentLimits struct {
	// MaxTokens is the maximum tokens per AgentRun.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=1
	MaxTokens int `json:"max_tokens"`

	// TimeoutSeconds is the maximum execution time.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=1
	TimeoutSeconds int `json:"timeout_seconds"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// AgentList is a list of Agent resources.
// +kubebuilder:object:root=true
type AgentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []Agent `json:"items"`
}
