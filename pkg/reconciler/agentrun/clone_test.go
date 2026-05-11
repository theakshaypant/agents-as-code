package agentrun

import (
	"testing"

	agentv1alpha1 "github.com/theakshaypant/agents-as-code/pkg/apis/agent/v1alpha1"
)

func TestCloneBranch_PRHeadBranch(t *testing.T) {
	run := &agentv1alpha1.AgentRun{
		Spec: agentv1alpha1.AgentRunSpec{
			Event: agentv1alpha1.AgentRunEventInfo{
				Branch: "main",
				PullRequest: &agentv1alpha1.PullRequestInfo{
					HeadBranch: "feature/foo",
					BaseBranch: "main",
				},
			},
		},
	}
	if got := cloneBranch(run); got != "feature/foo" {
		t.Errorf("cloneBranch() = %q, want feature/foo", got)
	}
}

func TestCloneBranch_EventBranch(t *testing.T) {
	run := &agentv1alpha1.AgentRun{
		Spec: agentv1alpha1.AgentRunSpec{
			Event: agentv1alpha1.AgentRunEventInfo{
				Branch: "develop",
			},
		},
	}
	if got := cloneBranch(run); got != "develop" {
		t.Errorf("cloneBranch() = %q, want develop", got)
	}
}

func TestCloneBranch_NoBranch(t *testing.T) {
	run := &agentv1alpha1.AgentRun{
		Spec: agentv1alpha1.AgentRunSpec{
			Event: agentv1alpha1.AgentRunEventInfo{},
		},
	}
	if got := cloneBranch(run); got != "" {
		t.Errorf("cloneBranch() = %q, want empty", got)
	}
}

func TestCloneCommand_WithBranch(t *testing.T) {
	got := cloneCommand("https://github.com/org/repo.git", "main", "/workspace/repo")
	want := "git clone --depth 1 --branch main https://github.com/org/repo.git /workspace/repo"
	if got != want {
		t.Errorf("cloneCommand() = %q, want %q", got, want)
	}
}

func TestCloneCommand_NoBranch(t *testing.T) {
	got := cloneCommand("https://github.com/org/repo.git", "", "/workspace/repo")
	want := "git clone --depth 1 https://github.com/org/repo.git /workspace/repo"
	if got != want {
		t.Errorf("cloneCommand() = %q, want %q", got, want)
	}
}

func TestAuthenticatedURL(t *testing.T) {
	got, err := authenticatedURL("https://github.com/org/repo", "ghp_abc123")
	if err != nil {
		t.Fatalf("authenticatedURL() error: %v", err)
	}
	want := "https://x-access-token:ghp_abc123@github.com/org/repo"
	if got != want {
		t.Errorf("authenticatedURL() = %q, want %q", got, want)
	}
}

func TestAuthenticatedURL_WithExistingPath(t *testing.T) {
	got, err := authenticatedURL("https://github.com/org/repo.git", "tok")
	if err != nil {
		t.Fatalf("authenticatedURL() error: %v", err)
	}
	want := "https://x-access-token:tok@github.com/org/repo.git"
	if got != want {
		t.Errorf("authenticatedURL() = %q, want %q", got, want)
	}
}
