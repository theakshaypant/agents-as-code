package agentrun

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	agentv1alpha1 "github.com/theakshaypant/agents-as-code/pkg/apis/agent/v1alpha1"
	"github.com/theakshaypant/agents-as-code/pkg/sandbox"
)

const clonePath = "/workspace/repo"

func (r *Reconciler) cloneRepo(ctx context.Context, handle *sandbox.Handle, run *agentv1alpha1.AgentRun, repo *agentv1alpha1.Repository) error {
	token, err := getProviderToken(ctx, r.client, repo)
	if err != nil {
		return fmt.Errorf("getting git token: %w", err)
	}

	authURL, err := authenticatedURL(repo.Spec.URL, token)
	if err != nil {
		return fmt.Errorf("building auth URL: %w", err)
	}

	branch := cloneBranch(run)
	cmd := cloneCommand(authURL, branch, clonePath)

	result, err := r.sandboxRuntime.Run(ctx, handle, cmd)
	if err != nil {
		return fmt.Errorf("running git clone: %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("git clone failed (exit %d): %s", result.ExitCode, result.Stderr)
	}
	return nil
}

func cloneBranch(run *agentv1alpha1.AgentRun) string {
	if pr := run.Spec.Event.PullRequest; pr != nil && pr.HeadBranch != "" {
		return pr.HeadBranch
	}
	if run.Spec.Event.Branch != "" {
		return run.Spec.Event.Branch
	}
	return ""
}

func cloneCommand(repoURL, branch, dest string) string {
	args := []string{"git", "clone", "--depth", "1"}
	if branch != "" {
		args = append(args, "--branch", branch)
	}
	args = append(args, repoURL, dest)
	return strings.Join(args, " ")
}

func authenticatedURL(repoURL, token string) (string, error) {
	u, err := url.Parse(repoURL)
	if err != nil {
		return "", fmt.Errorf("parsing repo URL %q: %w", repoURL, err)
	}
	u.User = url.UserPassword("x-access-token", token)
	return u.String(), nil
}
