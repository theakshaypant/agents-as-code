package agentrun

import (
	"context"
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	agentv1alpha1 "github.com/theakshaypant/agents-as-code/pkg/apis/agent/v1alpha1"
)

func testScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)
	_ = agentv1alpha1.AddToScheme(s)
	return s
}

func TestBuild_BasicConfig(t *testing.T) {
	run := &agentv1alpha1.AgentRun{
		Spec: agentv1alpha1.AgentRunSpec{
			SystemPrompt: "You are a reviewer.",
			Instructions: []agentv1alpha1.InstructionRef{
				{Name: "guide.md", Content: "Review carefully."},
			},
			Limits: agentv1alpha1.AgentLimits{
				MaxTokens:      100000,
				TimeoutSeconds: 300,
			},
		},
	}

	repo := &agentv1alpha1.Repository{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
		Spec: agentv1alpha1.RepositorySpec{
			Settings: &agentv1alpha1.Settings{
				AI: &agentv1alpha1.AIConfig{
					Provider: "anthropic",
					Model:    "claude-sonnet-4-20250514",
					ModelConfig: &agentv1alpha1.ModelConfig{
						Temperature:     "0.2",
						MaxOutputTokens: 8192,
						Thinking: &agentv1alpha1.ThinkingConfig{
							Enabled:      true,
							BudgetTokens: 10000,
						},
					},
				},
			},
		},
	}

	c := fake.NewClientBuilder().WithScheme(testScheme()).Build()
	builder := NewRuntimeConfigBuilder(c)

	data, err := builder.Build(context.Background(), run, repo)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	var cfg RuntimeConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if cfg.SystemPrompt != "You are a reviewer." {
		t.Errorf("SystemPrompt = %q", cfg.SystemPrompt)
	}
	if len(cfg.Instructions) != 1 || cfg.Instructions[0].Name != "guide.md" {
		t.Errorf("Instructions = %+v", cfg.Instructions)
	}
	if cfg.Model.Provider != "anthropic" || cfg.Model.Model != "claude-sonnet-4-20250514" {
		t.Errorf("Model = %+v", cfg.Model)
	}
	if cfg.Model.APIKeyEnv != "LLM_API_KEY" {
		t.Errorf("APIKeyEnv = %q", cfg.Model.APIKeyEnv)
	}
	if cfg.Model.Config == nil || cfg.Model.Config.Temperature != "0.2" {
		t.Errorf("ModelConfig = %+v", cfg.Model.Config)
	}
	if cfg.Model.Config.Thinking == nil || !cfg.Model.Config.Thinking.Enabled || cfg.Model.Config.Thinking.BudgetTokens != 10000 {
		t.Errorf("Thinking = %+v", cfg.Model.Config.Thinking)
	}
	if cfg.Limits.MaxTokens != 100000 || cfg.Limits.TimeoutSeconds != 300 {
		t.Errorf("Limits = %+v", cfg.Limits)
	}
	if cfg.Workspace != "/workspace" {
		t.Errorf("Workspace = %q", cfg.Workspace)
	}
}

func TestBuild_MCPServerWithSecretRef(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "github-token",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"token": []byte("ghp_test123"),
		},
	}

	run := &agentv1alpha1.AgentRun{
		Spec: agentv1alpha1.AgentRunSpec{
			SystemPrompt: "test",
			Tools: &agentv1alpha1.AgentToolsSpec{
				MCPServers: []string{"github"},
			},
			Limits: agentv1alpha1.AgentLimits{MaxTokens: 1000, TimeoutSeconds: 60},
		},
	}

	repo := &agentv1alpha1.Repository{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
		Spec: agentv1alpha1.RepositorySpec{
			Settings: &agentv1alpha1.Settings{
				MCPServers: []agentv1alpha1.MCPServerSpec{
					{
						Name:  "github",
						Image: "ghcr.io/mcp/github:latest",
						Env: []agentv1alpha1.EnvVar{
							{
								Name: "GITHUB_TOKEN",
								ValueFrom: &agentv1alpha1.EnvVarSource{
									SecretKeyRef: &agentv1alpha1.KeyRef{
										Name: "github-token",
										Key:  "token",
									},
								},
							},
						},
					},
				},
			},
		},
	}

	c := fake.NewClientBuilder().WithScheme(testScheme()).WithObjects(secret).Build()
	builder := NewRuntimeConfigBuilder(c)

	data, err := builder.Build(context.Background(), run, repo)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	var cfg RuntimeConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if len(cfg.MCPServers) != 1 {
		t.Fatalf("expected 1 MCP server, got %d", len(cfg.MCPServers))
	}
	srv := cfg.MCPServers[0]
	if srv.Name != "github" || srv.Image != "ghcr.io/mcp/github:latest" {
		t.Errorf("MCP server = %+v", srv)
	}
	if len(srv.Env) != 1 || srv.Env[0].Name != "GITHUB_TOKEN" || srv.Env[0].Value != "ghp_test123" {
		t.Errorf("Env = %+v", srv.Env)
	}
}

func TestBuild_MCPServerWithConfigMapRef(t *testing.T) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mcp-config",
			Namespace: "default",
		},
		Data: map[string]string{
			"endpoint": "https://api.example.com",
		},
	}

	run := &agentv1alpha1.AgentRun{
		Spec: agentv1alpha1.AgentRunSpec{
			SystemPrompt: "test",
			Tools: &agentv1alpha1.AgentToolsSpec{
				MCPServers: []string{"custom"},
			},
			Limits: agentv1alpha1.AgentLimits{MaxTokens: 1000, TimeoutSeconds: 60},
		},
	}

	repo := &agentv1alpha1.Repository{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
		Spec: agentv1alpha1.RepositorySpec{
			Settings: &agentv1alpha1.Settings{
				MCPServers: []agentv1alpha1.MCPServerSpec{
					{
						Name:    "custom",
						Command: []string{"npx", "custom-mcp"},
						Env: []agentv1alpha1.EnvVar{
							{
								Name: "API_ENDPOINT",
								ValueFrom: &agentv1alpha1.EnvVarSource{
									ConfigMapKeyRef: &agentv1alpha1.KeyRef{
										Name: "mcp-config",
										Key:  "endpoint",
									},
								},
							},
						},
					},
				},
			},
		},
	}

	c := fake.NewClientBuilder().WithScheme(testScheme()).WithObjects(cm).Build()
	builder := NewRuntimeConfigBuilder(c)

	data, err := builder.Build(context.Background(), run, repo)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	var cfg RuntimeConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if len(cfg.MCPServers) != 1 {
		t.Fatalf("expected 1 MCP server, got %d", len(cfg.MCPServers))
	}
	if cfg.MCPServers[0].Env[0].Value != "https://api.example.com" {
		t.Errorf("expected resolved configmap value, got %q", cfg.MCPServers[0].Env[0].Value)
	}
}

func TestBuild_MCPServerNotInCatalog(t *testing.T) {
	run := &agentv1alpha1.AgentRun{
		Spec: agentv1alpha1.AgentRunSpec{
			SystemPrompt: "test",
			Tools: &agentv1alpha1.AgentToolsSpec{
				MCPServers: []string{"nonexistent"},
			},
			Limits: agentv1alpha1.AgentLimits{MaxTokens: 1000, TimeoutSeconds: 60},
		},
	}

	repo := &agentv1alpha1.Repository{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
		Spec: agentv1alpha1.RepositorySpec{
			Settings: &agentv1alpha1.Settings{
				MCPServers: []agentv1alpha1.MCPServerSpec{
					{Name: "github"},
				},
			},
		},
	}

	c := fake.NewClientBuilder().WithScheme(testScheme()).Build()
	builder := NewRuntimeConfigBuilder(c)

	_, err := builder.Build(context.Background(), run, repo)
	if err == nil {
		t.Fatal("expected error for missing MCP server")
	}
}

func TestBuild_NoTools(t *testing.T) {
	run := &agentv1alpha1.AgentRun{
		Spec: agentv1alpha1.AgentRunSpec{
			SystemPrompt: "test",
			Limits:       agentv1alpha1.AgentLimits{MaxTokens: 1000, TimeoutSeconds: 60},
		},
	}

	repo := &agentv1alpha1.Repository{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
		Spec:       agentv1alpha1.RepositorySpec{},
	}

	c := fake.NewClientBuilder().WithScheme(testScheme()).Build()
	builder := NewRuntimeConfigBuilder(c)

	data, err := builder.Build(context.Background(), run, repo)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	var cfg RuntimeConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if cfg.MCPServers != nil {
		t.Errorf("expected nil MCP servers, got %+v", cfg.MCPServers)
	}
}

func TestBuild_InstructionsSkipEmpty(t *testing.T) {
	run := &agentv1alpha1.AgentRun{
		Spec: agentv1alpha1.AgentRunSpec{
			SystemPrompt: "test",
			Instructions: []agentv1alpha1.InstructionRef{
				{Name: "has-content", Content: "real content"},
				{Name: "no-content"},
			},
			Limits: agentv1alpha1.AgentLimits{MaxTokens: 1000, TimeoutSeconds: 60},
		},
	}

	repo := &agentv1alpha1.Repository{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
		Spec:       agentv1alpha1.RepositorySpec{},
	}

	c := fake.NewClientBuilder().WithScheme(testScheme()).Build()
	builder := NewRuntimeConfigBuilder(c)

	data, err := builder.Build(context.Background(), run, repo)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	var cfg RuntimeConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if len(cfg.Instructions) != 1 || cfg.Instructions[0].Name != "has-content" {
		t.Errorf("Instructions = %+v", cfg.Instructions)
	}
}
