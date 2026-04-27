package graphify

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/theakshaypant/agents-as-code/pkg/knowledgegraph"
)

type Builder struct {
	BinaryPath string
}

func New(binaryPath string) *Builder {
	if binaryPath == "" {
		binaryPath = "graphify"
	}
	return &Builder{BinaryPath: binaryPath}
}

func (b *Builder) Build(ctx context.Context, opts knowledgegraph.BuildOpts) (*knowledgegraph.Result, error) {
	args := []string{"build", "--output", opts.OutputDir}

	if opts.Scope != nil {
		for _, p := range opts.Scope.Paths {
			args = append(args, "--path", p)
		}
		for _, ig := range opts.Scope.Ignore {
			args = append(args, "--ignore", ig)
		}
	}

	args = append(args, opts.RepoDir)

	if err := b.run(ctx, args); err != nil {
		return nil, fmt.Errorf("graphify build: %w", err)
	}

	// TODO: parse graphify output to populate Result (node/edge/community counts)
	return &knowledgegraph.Result{}, nil
}

func (b *Builder) Update(ctx context.Context, opts knowledgegraph.UpdateOpts) (*knowledgegraph.Result, error) {
	args := []string{"build", "--update", "--output", opts.OutputDir}

	if opts.Scope != nil {
		for _, p := range opts.Scope.Paths {
			args = append(args, "--path", p)
		}
		for _, ig := range opts.Scope.Ignore {
			args = append(args, "--ignore", ig)
		}
	}

	args = append(args, opts.RepoDir)

	if err := b.run(ctx, args); err != nil {
		return nil, fmt.Errorf("graphify update: %w", err)
	}

	// TODO: parse graphify output to populate Result (node/edge/community counts)
	return &knowledgegraph.Result{}, nil
}

func (b *Builder) run(ctx context.Context, args []string) error {
	cmd := exec.CommandContext(ctx, b.BinaryPath, args...)
	// TODO: capture stdout/stderr for logging and result parsing
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run()
}
