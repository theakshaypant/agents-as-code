package result

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"go.uber.org/zap"

	"github.com/theakshaypant/agents-as-code/pkg/provider"
)

type mockProvider struct {
	createCommentFn     func(ctx context.Context, event *provider.Event, body string) (string, error)
	createReviewFn      func(ctx context.Context, event *provider.Event, reviewEvent, body string, comments []provider.ReviewComment) (string, error)
	addLabelsFn         func(ctx context.Context, event *provider.Event, labels []string) error
	removeLabelsFn      func(ctx context.Context, event *provider.Event, labels []string) error
	createPullRequestFn func(ctx context.Context, event *provider.Event, title, body, head, base string) (string, error)
	setCommitStatusFn   func(ctx context.Context, event *provider.Event, statusContext, state, description, targetURL string) error
	createCommitFn      func(ctx context.Context, event *provider.Event, message string, files map[string]string) (string, error)
}

func (m *mockProvider) Detect(_ *http.Request, _ string, _ *zap.SugaredLogger) (bool, bool, *zap.SugaredLogger, string, error) {
	return false, false, nil, "", nil
}
func (m *mockProvider) ParsePayload(_ context.Context, _ *http.Request, _ string) (*provider.Event, error) {
	return nil, nil
}
func (m *mockProvider) Validate(_ context.Context, _ *provider.Event) error { return nil }
func (m *mockProvider) SetClient(_ context.Context, _ *provider.Event, _, _ string) error {
	return nil
}
func (m *mockProvider) GetFiles(_ context.Context, _ *provider.Event) ([]string, error) {
	return nil, nil
}
func (m *mockProvider) GetFile(_ context.Context, _ *provider.Event, _ string) (string, error) {
	return "", nil
}
func (m *mockProvider) GetAgentDir(_ context.Context, _ *provider.Event, _ string) (string, error) {
	return "", nil
}
func (m *mockProvider) CheckPermission(_ context.Context, _ *provider.Event) (bool, error) {
	return false, nil
}
func (m *mockProvider) GetPullRequestDescription(_ context.Context, _ *provider.Event) (string, error) {
	return "", nil
}
func (m *mockProvider) GetPullRequestDiff(_ context.Context, _ *provider.Event) (string, error) {
	return "", nil
}
func (m *mockProvider) GetPullRequestReviews(_ context.Context, _ *provider.Event) (string, error) {
	return "", nil
}
func (m *mockProvider) GetPullRequestComments(_ context.Context, _ *provider.Event) (string, error) {
	return "", nil
}
func (m *mockProvider) GetIssueTitle(_ context.Context, _ *provider.Event) (string, error) {
	return "", nil
}
func (m *mockProvider) GetIssueBody(_ context.Context, _ *provider.Event) (string, error) {
	return "", nil
}
func (m *mockProvider) GetIssueComments(_ context.Context, _ *provider.Event) (string, error) {
	return "", nil
}
func (m *mockProvider) GetIssueLabels(_ context.Context, _ *provider.Event) (string, error) {
	return "", nil
}

func (m *mockProvider) CreateComment(ctx context.Context, event *provider.Event, body string) (string, error) {
	if m.createCommentFn != nil {
		return m.createCommentFn(ctx, event, body)
	}
	return "https://github.com/org/repo/issues/1#issuecomment-1", nil
}

func (m *mockProvider) CreateReview(ctx context.Context, event *provider.Event, reviewEvent, body string, comments []provider.ReviewComment) (string, error) {
	if m.createReviewFn != nil {
		return m.createReviewFn(ctx, event, reviewEvent, body, comments)
	}
	return "https://github.com/org/repo/pull/1#pullrequestreview-1", nil
}

func (m *mockProvider) AddLabels(ctx context.Context, event *provider.Event, labels []string) error {
	if m.addLabelsFn != nil {
		return m.addLabelsFn(ctx, event, labels)
	}
	return nil
}

func (m *mockProvider) RemoveLabels(ctx context.Context, event *provider.Event, labels []string) error {
	if m.removeLabelsFn != nil {
		return m.removeLabelsFn(ctx, event, labels)
	}
	return nil
}

func (m *mockProvider) CreatePullRequest(ctx context.Context, event *provider.Event, title, body, head, base string) (string, error) {
	if m.createPullRequestFn != nil {
		return m.createPullRequestFn(ctx, event, title, body, head, base)
	}
	return "https://github.com/org/repo/pull/2", nil
}

func (m *mockProvider) SetCommitStatus(ctx context.Context, event *provider.Event, statusContext, state, description, targetURL string) error {
	if m.setCommitStatusFn != nil {
		return m.setCommitStatusFn(ctx, event, statusContext, state, description, targetURL)
	}
	return nil
}

func (m *mockProvider) CreateCommit(ctx context.Context, event *provider.Event, message string, files map[string]string) (string, error) {
	if m.createCommitFn != nil {
		return m.createCommitFn(ctx, event, message, files)
	}
	return "abc123def456", nil
}

func testLogger() *zap.SugaredLogger {
	l, _ := zap.NewDevelopment()
	return l.Sugar()
}

func TestExecute(t *testing.T) {
	ctx := context.Background()
	evt := &provider.Event{
		Organization:      "org",
		Repository:        "repo",
		PullRequestNumber: 1,
		SHA:               "abc123",
		HeadBranch:        "feature/test",
	}

	t.Run("comment action", func(t *testing.T) {
		mock := &mockProvider{
			createCommentFn: func(_ context.Context, _ *provider.Event, body string) (string, error) {
				if body != "hello world" {
					t.Fatalf("expected body 'hello world', got %q", body)
				}
				return "https://example.com/comment/1", nil
			},
		}
		exec := NewExecutor(mock, testLogger())
		actions, err := exec.Execute(ctx, &Result{
			Actions: []Action{{Type: "comment", Body: "hello world"}},
		}, evt)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(actions) != 1 || actions[0].Type != "pr-comment" || actions[0].URL != "https://example.com/comment/1" {
			t.Fatalf("unexpected action: %+v", actions)
		}
	})

	t.Run("review action with inline comments", func(t *testing.T) {
		mock := &mockProvider{
			createReviewFn: func(_ context.Context, _ *provider.Event, event, body string, comments []provider.ReviewComment) (string, error) {
				if event != "REQUEST_CHANGES" {
					t.Fatalf("expected event REQUEST_CHANGES, got %q", event)
				}
				if len(comments) != 1 || comments[0].Path != "main.go" {
					t.Fatalf("unexpected comments: %+v", comments)
				}
				return "https://example.com/review/1", nil
			},
		}
		exec := NewExecutor(mock, testLogger())
		actions, err := exec.Execute(ctx, &Result{
			Actions: []Action{{
				Type: "review", Event: "REQUEST_CHANGES", Body: "needs work",
				Comments: []ReviewComment{{Path: "main.go", Line: 10, Body: "fix"}},
			}},
		}, evt)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(actions) != 1 || actions[0].Type != "pr-review" {
			t.Fatalf("unexpected action: %+v", actions)
		}
	})

	t.Run("label action", func(t *testing.T) {
		var addedLabels, removedLabels []string
		mock := &mockProvider{
			addLabelsFn: func(_ context.Context, _ *provider.Event, labels []string) error {
				addedLabels = labels
				return nil
			},
			removeLabelsFn: func(_ context.Context, _ *provider.Event, labels []string) error {
				removedLabels = labels
				return nil
			},
		}
		exec := NewExecutor(mock, testLogger())
		actions, err := exec.Execute(ctx, &Result{
			Actions: []Action{{Type: "label", Add: []string{"bug"}, Remove: []string{"triage"}}},
		}, evt)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(actions) != 1 || actions[0].Type != "label" {
			t.Fatalf("unexpected action: %+v", actions)
		}
		if len(addedLabels) != 1 || addedLabels[0] != "bug" {
			t.Fatalf("unexpected added labels: %v", addedLabels)
		}
		if len(removedLabels) != 1 || removedLabels[0] != "triage" {
			t.Fatalf("unexpected removed labels: %v", removedLabels)
		}
	})

	t.Run("create-pr action", func(t *testing.T) {
		mock := &mockProvider{
			createPullRequestFn: func(_ context.Context, _ *provider.Event, title, body, head, base string) (string, error) {
				if title != "fix bug" || head != "fix/x" || base != "main" {
					t.Fatalf("unexpected PR params: title=%q head=%q base=%q", title, head, base)
				}
				return "https://example.com/pull/2", nil
			},
		}
		exec := NewExecutor(mock, testLogger())
		actions, err := exec.Execute(ctx, &Result{
			Actions: []Action{{Type: "create-pr", Title: "fix bug", Head: "fix/x", Base: "main", Body: "desc"}},
		}, evt)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(actions) != 1 || actions[0].Type != "create-pr" || actions[0].URL != "https://example.com/pull/2" {
			t.Fatalf("unexpected action: %+v", actions)
		}
	})

	t.Run("status action", func(t *testing.T) {
		mock := &mockProvider{
			setCommitStatusFn: func(_ context.Context, _ *provider.Event, ctx, state, desc, url string) error {
				if ctx != "ci/review" || state != "success" {
					t.Fatalf("unexpected status params: ctx=%q state=%q", ctx, state)
				}
				return nil
			},
		}
		exec := NewExecutor(mock, testLogger())
		actions, err := exec.Execute(ctx, &Result{
			Actions: []Action{{Type: "status", Context: "ci/review", State: "success", Description: "passed"}},
		}, evt)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(actions) != 1 || actions[0].Type != "status-check" {
			t.Fatalf("unexpected action: %+v", actions)
		}
	})

	t.Run("commit action", func(t *testing.T) {
		mock := &mockProvider{
			createCommitFn: func(_ context.Context, _ *provider.Event, message string, files map[string]string) (string, error) {
				if message != "fix lint" {
					t.Fatalf("expected message 'fix lint', got %q", message)
				}
				if len(files) != 2 {
					t.Fatalf("expected 2 files, got %d", len(files))
				}
				if files["README.md"] != "updated" {
					t.Fatalf("unexpected README.md content: %q", files["README.md"])
				}
				return "deadbeef1234", nil
			},
		}
		exec := NewExecutor(mock, testLogger())
		actions, err := exec.Execute(ctx, &Result{
			Actions: []Action{{
				Type:    "commit",
				Message: "fix lint",
				Files:   map[string]string{"README.md": "updated", "main.go": "package main"},
			}},
		}, evt)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(actions) != 1 || actions[0].Type != "commit" || actions[0].SHA != "deadbeef1234" {
			t.Fatalf("unexpected action: %+v", actions)
		}
		if actions[0].URL != "https://github.com/org/repo/commit/deadbeef1234" {
			t.Fatalf("unexpected URL: %q", actions[0].URL)
		}
	})

	t.Run("partial failure continues execution", func(t *testing.T) {
		callCount := 0
		mock := &mockProvider{
			createCommentFn: func(_ context.Context, _ *provider.Event, body string) (string, error) {
				callCount++
				if body == "fail" {
					return "", fmt.Errorf("api error")
				}
				return "https://example.com/comment/1", nil
			},
		}
		exec := NewExecutor(mock, testLogger())
		actions, err := exec.Execute(ctx, &Result{
			Actions: []Action{
				{Type: "comment", Body: "first"},
				{Type: "comment", Body: "fail"},
				{Type: "comment", Body: "third"},
			},
		}, evt)
		if err == nil {
			t.Fatal("expected error for partial failure")
		}
		if callCount != 3 {
			t.Fatalf("expected 3 calls, got %d", callCount)
		}
		if len(actions) != 2 {
			t.Fatalf("expected 2 successful actions, got %d", len(actions))
		}
	})

	t.Run("empty result", func(t *testing.T) {
		mock := &mockProvider{}
		exec := NewExecutor(mock, testLogger())
		actions, err := exec.Execute(ctx, &Result{}, evt)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(actions) != 0 {
			t.Fatalf("expected 0 actions, got %d", len(actions))
		}
	})
}
