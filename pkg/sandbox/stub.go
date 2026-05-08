package sandbox

import (
	"context"
	"encoding/json"
)

var _ Runtime = (*Stub)(nil)

// Stub implements Runtime for testing without a real sandbox controller.
// Each method delegates to its function field if set, otherwise returns defaults.
type Stub struct {
	CreateFunc    func(ctx context.Context, opts CreateOpts) (*Handle, error)
	GetFunc       func(ctx context.Context, claimName, namespace string) (*Handle, error)
	WriteFileFunc func(ctx context.Context, h *Handle, path string, content []byte) error
	ReadFileFunc  func(ctx context.Context, h *Handle, path string) ([]byte, error)
	RunFunc       func(ctx context.Context, h *Handle, command string) (*RunResult, error)
	DestroyFunc   func(ctx context.Context, h *Handle) error
}

func NewStub() *Stub {
	return &Stub{}
}

func (s *Stub) Create(ctx context.Context, opts CreateOpts) (*Handle, error) {
	if s.CreateFunc != nil {
		return s.CreateFunc(ctx, opts)
	}
	return &Handle{
		ClaimName:   "stub-claim",
		SandboxName: "stub-sandbox",
		Namespace:   opts.Namespace,
	}, nil
}

func (s *Stub) Get(ctx context.Context, claimName, namespace string) (*Handle, error) {
	if s.GetFunc != nil {
		return s.GetFunc(ctx, claimName, namespace)
	}
	return &Handle{
		ClaimName:   claimName,
		SandboxName: "stub-sandbox",
		Namespace:   namespace,
	}, nil
}

func (s *Stub) WriteFile(ctx context.Context, h *Handle, path string, content []byte) error {
	if s.WriteFileFunc != nil {
		return s.WriteFileFunc(ctx, h, path, content)
	}
	return nil
}

func (s *Stub) ReadFile(ctx context.Context, h *Handle, path string) ([]byte, error) {
	if s.ReadFileFunc != nil {
		return s.ReadFileFunc(ctx, h, path)
	}
	return defaultResult()
}

func (s *Stub) Run(ctx context.Context, h *Handle, command string) (*RunResult, error) {
	if s.RunFunc != nil {
		return s.RunFunc(ctx, h, command)
	}
	return &RunResult{ExitCode: 0}, nil
}

func (s *Stub) Destroy(ctx context.Context, h *Handle) error {
	if s.DestroyFunc != nil {
		return s.DestroyFunc(ctx, h)
	}
	return nil
}

func defaultResult() ([]byte, error) {
	r := struct {
		Actions    []struct{} `json:"actions"`
		TokensUsed int        `json:"tokens_used"`
		CostUSD    string     `json:"cost_usd"`
	}{
		Actions:    []struct{}{},
		TokensUsed: 0,
		CostUSD:    "0.00",
	}
	return json.Marshal(r)
}
