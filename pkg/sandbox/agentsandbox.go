package sandbox

import (
	"context"
	"fmt"

	"k8s.io/client-go/rest"
	sdk "sigs.k8s.io/agent-sandbox/clients/go/sandbox"
)

var _ Runtime = (*AgentSandboxRuntime)(nil)

// AgentSandboxRuntime implements Runtime using the K8s SIG Agent Sandbox SDK.
type AgentSandboxRuntime struct {
	client *sdk.Client
}

// NewAgentSandboxRuntime creates a runtime backed by the agent-sandbox controller.
// The defaultTemplate is the SandboxTemplate name used to initialise the SDK
// client; individual Create calls can override it via CreateOpts.TemplateName.
func NewAgentSandboxRuntime(ctx context.Context, cfg *rest.Config, defaultTemplate string) (*AgentSandboxRuntime, error) {
	c, err := sdk.NewClient(ctx, sdk.Options{
		TemplateName: defaultTemplate,
		RestConfig:   cfg,
		Quiet:        true,
	})
	if err != nil {
		return nil, fmt.Errorf("initializing agent-sandbox client: %w", err)
	}
	return &AgentSandboxRuntime{client: c}, nil
}

func (r *AgentSandboxRuntime) Create(ctx context.Context, opts CreateOpts) (*Handle, error) {
	sb, err := r.client.CreateSandbox(ctx, opts.TemplateName, opts.Namespace)
	if err != nil {
		return nil, fmt.Errorf("creating sandbox: %w", err)
	}
	return &Handle{
		ClaimName:   sb.ClaimName(),
		SandboxName: sb.SandboxName(),
		Namespace:   opts.Namespace,
	}, nil
}

func (r *AgentSandboxRuntime) Get(ctx context.Context, claimName, namespace string) (*Handle, error) {
	sb, err := r.client.GetSandbox(ctx, claimName, namespace)
	if err != nil {
		return nil, fmt.Errorf("getting sandbox %s/%s: %w", namespace, claimName, err)
	}
	return &Handle{
		ClaimName:   sb.ClaimName(),
		SandboxName: sb.SandboxName(),
		Namespace:   namespace,
	}, nil
}

func (r *AgentSandboxRuntime) WriteFile(ctx context.Context, h *Handle, path string, content []byte) error {
	sb, err := r.client.GetSandbox(ctx, h.ClaimName, h.Namespace)
	if err != nil {
		return fmt.Errorf("getting sandbox for write: %w", err)
	}
	if err := sb.Write(ctx, path, content); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

func (r *AgentSandboxRuntime) ReadFile(ctx context.Context, h *Handle, path string) ([]byte, error) {
	sb, err := r.client.GetSandbox(ctx, h.ClaimName, h.Namespace)
	if err != nil {
		return nil, fmt.Errorf("getting sandbox for read: %w", err)
	}
	data, err := sb.Read(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return data, nil
}

func (r *AgentSandboxRuntime) Run(ctx context.Context, h *Handle, command string) (*RunResult, error) {
	sb, err := r.client.GetSandbox(ctx, h.ClaimName, h.Namespace)
	if err != nil {
		return nil, fmt.Errorf("getting sandbox for run: %w", err)
	}
	result, err := sb.Run(ctx, command)
	if err != nil {
		return nil, fmt.Errorf("running command: %w", err)
	}
	return &RunResult{
		ExitCode: result.ExitCode,
		Stdout:   result.Stdout,
		Stderr:   result.Stderr,
	}, nil
}

func (r *AgentSandboxRuntime) Destroy(ctx context.Context, h *Handle) error {
	if err := r.client.DeleteSandbox(ctx, h.ClaimName, h.Namespace); err != nil {
		return fmt.Errorf("destroying sandbox %s/%s: %w", h.Namespace, h.ClaimName, err)
	}
	return nil
}
