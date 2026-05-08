package agentrun

import (
	"context"
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentv1alpha1 "github.com/theakshaypant/agents-as-code/pkg/apis/agent/v1alpha1"
)

const (
	ConfigPath  = "/etc/aac/config.json"
	ResultPath  = "/output/result.json"
	APIKeyEnv   = "LLM_API_KEY"
	WorkspaceDir = "/workspace"
)

type RuntimeConfig struct {
	SystemPrompt string               `json:"system_prompt"`
	Instructions []RuntimeInstruction `json:"instructions,omitempty"`
	Model        RuntimeModel         `json:"model"`
	MCPServers   []RuntimeMCPServer   `json:"mcp_servers,omitempty"`
	Limits       RuntimeLimits        `json:"limits"`
	Workspace    string               `json:"workspace"`
}

type RuntimeInstruction struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

type RuntimeModel struct {
	Provider  string              `json:"provider"`
	Model     string              `json:"model"`
	APIKeyEnv string              `json:"api_key_env"`
	BaseURL   string              `json:"base_url,omitempty"`
	Config    *RuntimeModelConfig `json:"config,omitempty"`
}

type RuntimeModelConfig struct {
	Temperature     string                `json:"temperature,omitempty"`
	MaxOutputTokens int                   `json:"max_output_tokens,omitempty"`
	MaxContextTokens int                  `json:"max_context_tokens,omitempty"`
	Thinking        *RuntimeThinkingConfig `json:"thinking,omitempty"`
	Parameters      map[string]string     `json:"parameters,omitempty"`
}

type RuntimeThinkingConfig struct {
	Enabled      bool `json:"enabled"`
	BudgetTokens int  `json:"budget_tokens,omitempty"`
}

type RuntimeMCPServer struct {
	Name    string          `json:"name"`
	Image   string          `json:"image,omitempty"`
	Command []string        `json:"command,omitempty"`
	Args    []string        `json:"args,omitempty"`
	Env     []RuntimeEnvVar `json:"env,omitempty"`
}

type RuntimeEnvVar struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type RuntimeLimits struct {
	MaxTokens      int `json:"max_tokens"`
	TimeoutSeconds int `json:"timeout_seconds"`
}

type RuntimeConfigBuilder struct {
	client client.Client
}

func NewRuntimeConfigBuilder(c client.Client) *RuntimeConfigBuilder {
	return &RuntimeConfigBuilder{client: c}
}

func (b *RuntimeConfigBuilder) Build(ctx context.Context, run *agentv1alpha1.AgentRun, repo *agentv1alpha1.Repository) ([]byte, error) {
	cfg := RuntimeConfig{
		SystemPrompt: run.Spec.SystemPrompt,
		Instructions: b.buildInstructions(run),
		Model:        b.buildModel(repo),
		Limits:       b.buildLimits(run),
		Workspace:    WorkspaceDir,
	}

	mcpServers, err := b.buildMCPServers(ctx, run, repo)
	if err != nil {
		return nil, fmt.Errorf("building MCP server config: %w", err)
	}
	cfg.MCPServers = mcpServers

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshaling runtime config: %w", err)
	}
	return data, nil
}

func (b *RuntimeConfigBuilder) buildInstructions(run *agentv1alpha1.AgentRun) []RuntimeInstruction {
	var instructions []RuntimeInstruction
	for _, ref := range run.Spec.Instructions {
		if ref.Content == "" {
			continue
		}
		instructions = append(instructions, RuntimeInstruction{
			Name:    ref.Name,
			Content: ref.Content,
		})
	}
	return instructions
}

func (b *RuntimeConfigBuilder) buildModel(repo *agentv1alpha1.Repository) RuntimeModel {
	m := RuntimeModel{
		APIKeyEnv: APIKeyEnv,
	}
	if repo.Spec.Settings == nil || repo.Spec.Settings.AI == nil {
		return m
	}
	ai := repo.Spec.Settings.AI
	m.Provider = ai.Provider
	m.Model = ai.Model
	m.BaseURL = ai.BaseURL

	if ai.ModelConfig != nil {
		mc := &RuntimeModelConfig{
			Temperature:      ai.ModelConfig.Temperature,
			MaxOutputTokens:  ai.ModelConfig.MaxOutputTokens,
			MaxContextTokens: ai.ModelConfig.MaxContextTokens,
			Parameters:       ai.ModelConfig.Parameters,
		}
		if ai.ModelConfig.Thinking != nil {
			mc.Thinking = &RuntimeThinkingConfig{
				Enabled:      ai.ModelConfig.Thinking.Enabled,
				BudgetTokens: ai.ModelConfig.Thinking.BudgetTokens,
			}
		}
		m.Config = mc
	}
	return m
}

func (b *RuntimeConfigBuilder) buildMCPServers(ctx context.Context, run *agentv1alpha1.AgentRun, repo *agentv1alpha1.Repository) ([]RuntimeMCPServer, error) {
	if run.Spec.Tools == nil || len(run.Spec.Tools.MCPServers) == 0 {
		return nil, nil
	}
	if repo.Spec.Settings == nil {
		return nil, nil
	}

	catalog := make(map[string]agentv1alpha1.MCPServerSpec, len(repo.Spec.Settings.MCPServers))
	for _, s := range repo.Spec.Settings.MCPServers {
		catalog[s.Name] = s
	}

	var servers []RuntimeMCPServer
	for _, name := range run.Spec.Tools.MCPServers {
		spec, ok := catalog[name]
		if !ok {
			return nil, fmt.Errorf("MCP server %q not found in repository catalog", name)
		}

		resolved, err := b.resolveEnvVars(ctx, spec.Env, repo.Namespace)
		if err != nil {
			return nil, fmt.Errorf("resolving env vars for MCP server %q: %w", name, err)
		}

		servers = append(servers, RuntimeMCPServer{
			Name:    spec.Name,
			Image:   spec.Image,
			Command: spec.Command,
			Args:    spec.Args,
			Env:     resolved,
		})
	}
	return servers, nil
}

func (b *RuntimeConfigBuilder) resolveEnvVars(ctx context.Context, envVars []agentv1alpha1.EnvVar, namespace string) ([]RuntimeEnvVar, error) {
	var resolved []RuntimeEnvVar
	for _, ev := range envVars {
		val, err := b.resolveEnvVar(ctx, ev, namespace)
		if err != nil {
			return nil, fmt.Errorf("env var %q: %w", ev.Name, err)
		}
		resolved = append(resolved, RuntimeEnvVar{
			Name:  ev.Name,
			Value: val,
		})
	}
	return resolved, nil
}

func (b *RuntimeConfigBuilder) resolveEnvVar(ctx context.Context, ev agentv1alpha1.EnvVar, namespace string) (string, error) {
	if ev.Value != "" {
		return ev.Value, nil
	}
	if ev.ValueFrom == nil {
		return "", nil
	}

	if ev.ValueFrom.SecretKeyRef != nil {
		ref := ev.ValueFrom.SecretKeyRef
		var secret corev1.Secret
		if err := b.client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: ref.Name}, &secret); err != nil {
			return "", fmt.Errorf("fetching secret %s/%s: %w", namespace, ref.Name, err)
		}
		val, ok := secret.Data[ref.Key]
		if !ok {
			return "", fmt.Errorf("key %q not found in secret %s/%s", ref.Key, namespace, ref.Name)
		}
		return string(val), nil
	}

	if ev.ValueFrom.ConfigMapKeyRef != nil {
		ref := ev.ValueFrom.ConfigMapKeyRef
		var cm corev1.ConfigMap
		if err := b.client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: ref.Name}, &cm); err != nil {
			return "", fmt.Errorf("fetching configmap %s/%s: %w", namespace, ref.Name, err)
		}
		val, ok := cm.Data[ref.Key]
		if !ok {
			return "", fmt.Errorf("key %q not found in configmap %s/%s", ref.Key, namespace, ref.Name)
		}
		return val, nil
	}

	return "", nil
}

func (b *RuntimeConfigBuilder) buildLimits(run *agentv1alpha1.AgentRun) RuntimeLimits {
	return RuntimeLimits{
		MaxTokens:      run.Spec.Limits.MaxTokens,
		TimeoutSeconds: run.Spec.Limits.TimeoutSeconds,
	}
}
