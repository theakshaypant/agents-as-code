package agentrun

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	agentv1alpha1 "github.com/theakshaypant/agents-as-code/pkg/apis/agent/v1alpha1"
	"github.com/theakshaypant/agents-as-code/pkg/provider"
)

func TestCreateProviderEvent(t *testing.T) {
	run := &agentv1alpha1.AgentRun{
		Spec: agentv1alpha1.AgentRunSpec{
			Event: agentv1alpha1.AgentRunEventInfo{
				Type:   "pull_request",
				Action: "opened",
				SHA:    "abc123",
				Branch: "main",
				Sender: "user1",
				URL:    "https://github.com/org/repo/pull/42",
				PullRequest: &agentv1alpha1.PullRequestInfo{
					Number:     42,
					Title:      "Fix bug",
					HeadBranch: "fix/bug",
					BaseBranch: "main",
				},
				Labels: []string{"bug"},
			},
		},
	}

	repo := &agentv1alpha1.Repository{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
		Spec: agentv1alpha1.RepositorySpec{
			URL: "https://github.com/org/repo",
		},
	}

	evt := CreateProviderEvent(run, repo)

	if evt.Organization != "org" || evt.Repository != "repo" {
		t.Errorf("org/repo = %s/%s", evt.Organization, evt.Repository)
	}
	if evt.TriggerType != provider.TriggerPullRequest {
		t.Errorf("TriggerType = %q", evt.TriggerType)
	}
	if evt.PullRequestNumber != 42 || evt.PullRequestTitle != "Fix bug" {
		t.Errorf("PR = %d %q", evt.PullRequestNumber, evt.PullRequestTitle)
	}
	if evt.HeadBranch != "fix/bug" || evt.BaseBranch != "main" {
		t.Errorf("branches = %s/%s", evt.HeadBranch, evt.BaseBranch)
	}
	if evt.SHA != "abc123" || evt.Sender != "user1" {
		t.Errorf("SHA=%q Sender=%q", evt.SHA, evt.Sender)
	}
	if evt.Label != "bug" {
		t.Errorf("Label = %q", evt.Label)
	}
}

func TestCreateProviderEvent_CommentEvent(t *testing.T) {
	run := &agentv1alpha1.AgentRun{
		Spec: agentv1alpha1.AgentRunSpec{
			Event: agentv1alpha1.AgentRunEventInfo{
				Type:   "issue_comment",
				Sender: "user2",
				Comment: &agentv1alpha1.CommentInfo{
					Body: "/review focus on security",
				},
				PullRequest: &agentv1alpha1.PullRequestInfo{
					Number: 10,
				},
			},
		},
	}

	repo := &agentv1alpha1.Repository{
		Spec: agentv1alpha1.RepositorySpec{
			URL: "https://github.com/myorg/myrepo.git",
		},
	}

	evt := CreateProviderEvent(run, repo)

	if evt.Organization != "myorg" || evt.Repository != "myrepo" {
		t.Errorf("org/repo = %s/%s", evt.Organization, evt.Repository)
	}
	if evt.CommentBody != "/review focus on security" {
		t.Errorf("CommentBody = %q", evt.CommentBody)
	}
	if evt.PullRequestNumber != 10 {
		t.Errorf("PullRequestNumber = %d", evt.PullRequestNumber)
	}
}

func TestSplitRepoURL(t *testing.T) {
	tests := []struct {
		url      string
		wantOrg  string
		wantRepo string
	}{
		{"https://github.com/org/repo", "org", "repo"},
		{"https://github.com/org/repo/", "org", "repo"},
		{"https://github.com/org/repo.git", "org", "repo"},
		{"https://gitlab.com/group/project", "group", "project"},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got := splitRepoURL(tt.url)
			if got[0] != tt.wantOrg || got[1] != tt.wantRepo {
				t.Errorf("splitRepoURL(%q) = %v, want [%s %s]", tt.url, got, tt.wantOrg, tt.wantRepo)
			}
		})
	}
}
