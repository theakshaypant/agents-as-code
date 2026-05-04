package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	duckv1 "knative.dev/pkg/apis/duck/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Repository is the representation of a Git repository and its associated
// knowledge graph configuration.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=repo
// +kubebuilder:printcolumn:name="URL",type=string,JSONPath=`.spec.url`
// +kubebuilder:printcolumn:name="KG",type=boolean,JSONPath=`.spec.knowledge_graph.enabled`
type Repository struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   RepositorySpec   `json:"spec"`
	Status RepositoryStatus `json:"status,omitempty"`
}

type RepositorySpec struct {
	// URL of the Git repository.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^https?://`
	URL string `json:"url"`

	// GitProvider contains authentication configuration for the Git provider.
	// +optional
	GitProvider *GitProvider `json:"git_provider,omitempty"`

	// KnowledgeGraph configures the repository's knowledge graph.
	// +optional
	KnowledgeGraph *KnowledgeGraphSpec `json:"knowledge_graph,omitempty"`

	// Settings contains optional configuration for the repository.
	// +optional
	Settings *Settings `json:"settings,omitempty"`
}

type Settings struct {
	// AI configures the LLM provider used by agents for this repository.
	// +optional
	AI *AIConfig `json:"ai,omitempty"`
}

type AIConfig struct {
	// Enabled controls whether AI agents can run against this repository.
	// +kubebuilder:validation:Required
	Enabled bool `json:"enabled"`

	// Provider is the LLM provider name (e.g. openai, anthropic, gemini).
	// +kubebuilder:validation:Required
	Provider string `json:"provider"`

	// SecretRef references the Kubernetes Secret containing the LLM API key.
	// +kubebuilder:validation:Required
	SecretRef Secret `json:"secret_ref"`

	// Model overrides the default LLM model for this provider.
	// +optional
	Model string `json:"model,omitempty"`

	// BaseURL overrides the LLM API endpoint for custom or self-hosted providers.
	// +optional
	BaseURL string `json:"base_url,omitempty"`

	// MaxCostPerRun is the per-run budget cap in USD (e.g. "1.50").
	// The controller terminates runs that exceed this amount.
	// +optional
	// +kubebuilder:validation:Pattern=`^[0-9]+(\.[0-9]+)?$`
	MaxCostPerRun string `json:"max_cost_per_run,omitempty"`

	// MaxTokensPerRun is the default token limit per run.
	// Agents can set lower limits but not higher.
	// +optional
	// +kubebuilder:validation:Minimum=1
	MaxTokensPerRun int `json:"max_tokens_per_run,omitempty"`

	// MaxTimeoutSeconds is the default execution timeout per run.
	// Agents can set lower timeouts but not higher.
	// +optional
	// +kubebuilder:validation:Minimum=1
	MaxTimeoutSeconds int `json:"max_timeout_seconds,omitempty"`

	// ModelConfig holds model-specific tuning parameters.
	// +optional
	ModelConfig *ModelConfig `json:"model_config,omitempty"`
}

type ModelConfig struct {
	// Temperature controls randomness (0.0 = deterministic, 1.0+ = creative).
	// Serialized as a string to satisfy CRD schema constraints (e.g. "0.2").
	// +optional
	Temperature string `json:"temperature,omitempty"`

	// MaxOutputTokens limits the length of each model response.
	// +optional
	// +kubebuilder:validation:Minimum=1
	MaxOutputTokens int `json:"max_output_tokens,omitempty"`

	// MaxContextTokens limits the total context window usage.
	// The controller truncates instruction content to fit within this budget.
	// +optional
	// +kubebuilder:validation:Minimum=1
	MaxContextTokens int `json:"max_context_tokens,omitempty"`

	// Thinking configures extended thinking / chain-of-thought.
	// +optional
	Thinking *ThinkingConfig `json:"thinking,omitempty"`

	// Parameters is a provider-specific key-value map for settings
	// not covered by the common fields (e.g. top_p, frequency_penalty).
	// +optional
	Parameters map[string]string `json:"parameters,omitempty"`
}

type ThinkingConfig struct {
	// Enabled controls whether extended thinking is active.
	// +kubebuilder:validation:Required
	Enabled bool `json:"enabled"`

	// BudgetTokens is the max tokens the model can use for thinking.
	// Maps to Claude's budget_tokens or OpenAI's max_completion_tokens for reasoning.
	// +optional
	// +kubebuilder:validation:Minimum=1
	BudgetTokens int `json:"budget_tokens,omitempty"`
}

type GitProvider struct {
	// Secret references the Kubernetes Secret containing the Git provider token.
	// +kubebuilder:validation:Required
	Secret Secret `json:"secret"`

	// WebhookSecret references the Kubernetes Secret containing the webhook
	// shared secret for payload validation. Used for webhook/PAT auth.
	// For GitHub App auth, the webhook secret comes from the global
	// controller secret instead.
	// +optional
	WebhookSecret *Secret `json:"webhook_secret,omitempty"`
}

type Secret struct {
	// Name of the Kubernetes Secret.
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Key within the Secret.
	// +optional
	Key string `json:"key,omitempty"`
}

type KnowledgeGraphSpec struct {
	// Enabled controls whether the knowledge graph is maintained for this repository.
	// +kubebuilder:validation:Required
	Enabled bool `json:"enabled"`

	// StorageClassName is the StorageClass used for provisioning KG PersistentVolumes.
	// +optional
	StorageClassName string `json:"storageClassName,omitempty"`

	// Branches defines per-branch KG configuration. Each branch gets its own PV.
	// +optional
	Branches []KGBranchConfig `json:"branches,omitempty"`
}

type KGBranchConfig struct {
	// Name of the branch. Supports glob patterns (e.g. "release-*").
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Storage is the size of the PV for this branch's knowledge graph.
	// +optional
	Storage resource.Quantity `json:"storage,omitempty"`

	// Scope limits which paths are included in the knowledge graph.
	// +optional
	Scope *KGScope `json:"scope,omitempty"`
}

type KGScope struct {
	// Paths limits the graph to these directories.
	// +optional
	Paths []string `json:"paths,omitempty"`

	// Ignore excludes files matching these patterns, on top of graphify defaults.
	// +optional
	Ignore []string `json:"ignore,omitempty"`
}

type RepositoryStatus struct {
	// KnowledgeGraph tracks the status of the knowledge graph per branch.
	// +optional
	KnowledgeGraph *KGStatus `json:"knowledge_graph,omitempty"`
}

type KGStatus struct {
	// Branches contains per-branch KG status.
	// +optional
	Branches []KGBranchStatus `json:"branches,omitempty"`
}

type KGBranchStatus struct {
	duckv1.Status `json:",inline"`

	// Name of the branch.
	Name string `json:"name"`

	// LastUpdated is the last time the KG was updated.
	// +optional
	LastUpdated *metav1.Time `json:"lastUpdated,omitempty"`

	// LastCommitSHA is the commit SHA the KG was last built from.
	// +optional
	LastCommitSHA string `json:"lastCommitSHA,omitempty"`

	// NodeCount is the number of nodes in the graph.
	// +optional
	NodeCount int `json:"nodeCount,omitempty"`

	// EdgeCount is the number of edges in the graph.
	// +optional
	EdgeCount int `json:"edgeCount,omitempty"`

	// CommunityCount is the number of detected communities.
	// +optional
	CommunityCount int `json:"communityCount,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// RepositoryList is a list of Repository resources.
// +kubebuilder:object:root=true
type RepositoryList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []Repository `json:"items"`
}
