package sandbox

import "context"

// Handle identifies a provisioned sandbox instance.
type Handle struct {
	ClaimName   string
	SandboxName string
	Namespace   string
}

// CreateOpts configures sandbox creation.
type CreateOpts struct {
	Namespace    string
	TemplateName string
}

// RunResult holds the output of a command executed in a sandbox.
type RunResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

// Runtime is a backend-agnostic interface for provisioning and interacting
// with sandboxed execution environments. Implementations may target the K8s
// SIG Agent Sandbox, NVIDIA OpenShell, Daytona, or any other isolation layer.
type Runtime interface {
	// Create provisions a sandbox and blocks until it is ready.
	Create(ctx context.Context, opts CreateOpts) (*Handle, error)

	// Get reconnects to an existing sandbox by claim name.
	Get(ctx context.Context, claimName, namespace string) (*Handle, error)

	// WriteFile writes content to a path inside the sandbox.
	WriteFile(ctx context.Context, h *Handle, path string, content []byte) error

	// ReadFile reads content from a path inside the sandbox.
	ReadFile(ctx context.Context, h *Handle, path string) ([]byte, error)

	// Run executes a command inside the sandbox.
	Run(ctx context.Context, h *Handle, command string) (*RunResult, error)

	// Destroy tears down the sandbox and releases resources.
	Destroy(ctx context.Context, h *Handle) error
}
