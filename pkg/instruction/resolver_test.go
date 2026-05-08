package instruction

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"

	agentv1alpha1 "github.com/theakshaypant/agents-as-code/pkg/apis/agent/v1alpha1"
	"github.com/theakshaypant/agents-as-code/pkg/apis/agent/keys"
	"github.com/theakshaypant/agents-as-code/pkg/provider"
	"github.com/theakshaypant/agents-as-code/pkg/template"
)

type mockProvider struct {
	getFileFn func(ctx context.Context, event *provider.Event, path string) (string, error)
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
func (m *mockProvider) GetFile(ctx context.Context, event *provider.Event, path string) (string, error) {
	if m.getFileFn != nil {
		return m.getFileFn(ctx, event, path)
	}
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
func (m *mockProvider) CreateComment(_ context.Context, _ *provider.Event, _ string) (string, error) {
	return "", nil
}
func (m *mockProvider) CreateReview(_ context.Context, _ *provider.Event, _, _ string, _ []provider.ReviewComment) (string, error) {
	return "", nil
}
func (m *mockProvider) AddLabels(_ context.Context, _ *provider.Event, _ []string) error {
	return nil
}
func (m *mockProvider) RemoveLabels(_ context.Context, _ *provider.Event, _ []string) error {
	return nil
}
func (m *mockProvider) CreatePullRequest(_ context.Context, _ *provider.Event, _, _, _, _ string) (string, error) {
	return "", nil
}
func (m *mockProvider) SetCommitStatus(_ context.Context, _ *provider.Event, _, _, _, _ string) error {
	return nil
}
func (m *mockProvider) CreateCommit(_ context.Context, _ *provider.Event, _ string, _ map[string]string) (string, error) {
	return "", nil
}

func testLogger() *zap.SugaredLogger {
	l, _ := zap.NewDevelopment()
	return l.Sugar()
}

func TestParseAnnotations(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		want        []string
	}{
		{
			name:        "no annotations",
			annotations: map[string]string{},
			want:        nil,
		},
		{
			name: "single instruction",
			annotations: map[string]string{
				keys.Instruction: "CLAUDE.md",
			},
			want: []string{"CLAUDE.md"},
		},
		{
			name: "numbered only",
			annotations: map[string]string{
				keys.InstructionBase + "1": "first.md",
				keys.InstructionBase + "2": "second.md",
			},
			want: []string{"first.md", "second.md"},
		},
		{
			name: "base plus numbered",
			annotations: map[string]string{
				keys.Instruction:          "CLAUDE.md",
				keys.InstructionBase + "1": "review-guide.md",
				keys.InstructionBase + "2": "https://example.com/checklist.md",
			},
			want: []string{"CLAUDE.md", "review-guide.md", "https://example.com/checklist.md"},
		},
		{
			name: "numbered with gap",
			annotations: map[string]string{
				keys.InstructionBase + "1": "first.md",
				keys.InstructionBase + "5": "fifth.md",
			},
			want: []string{"first.md", "fifth.md"},
		},
		{
			name: "non-numeric suffix ignored",
			annotations: map[string]string{
				keys.Instruction:            "base.md",
				keys.InstructionBase + "abc": "bad.md",
				keys.InstructionBase + "1":   "good.md",
			},
			want: []string{"base.md", "good.md"},
		},
		{
			name: "empty values skipped",
			annotations: map[string]string{
				keys.Instruction:          "",
				keys.InstructionBase + "1": "only.md",
			},
			want: []string{"only.md"},
		},
		{
			name: "unrelated annotations ignored",
			annotations: map[string]string{
				keys.OnEvent:     "pull_request",
				keys.Instruction: "CLAUDE.md",
			},
			want: []string{"CLAUDE.md"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseAnnotations(tt.annotations)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d refs, want %d: %v", len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("ref[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestResolveAll_RepoFile(t *testing.T) {
	mock := &mockProvider{
		getFileFn: func(_ context.Context, _ *provider.Event, path string) (string, error) {
			switch path {
			case "CLAUDE.md":
				return "# Claude instructions\nBe helpful.", nil
			case "review-guide.md":
				return "# Review Guide\nCheck for bugs.", nil
			default:
				return "", fmt.Errorf("file not found: %s", path)
			}
		},
	}

	evt := &provider.Event{Repository: "test-repo", Organization: "org"}
	repo := &agentv1alpha1.Repository{}
	resolver := NewResolver(mock, evt, testLogger())
	varResolver := template.NewVariableResolver(evt, repo, mock, testLogger(), "")

	refs := []string{"CLAUDE.md", "review-guide.md"}
	got := resolver.ResolveAll(context.Background(), refs, varResolver)

	if len(got) != 2 {
		t.Fatalf("got %d instructions, want 2", len(got))
	}

	if got[0].Name != "CLAUDE.md" || got[0].Path != "CLAUDE.md" || got[0].Content != "# Claude instructions\nBe helpful." {
		t.Errorf("instruction[0] = %+v", got[0])
	}
	if got[1].Name != "review-guide.md" || got[1].Path != "review-guide.md" {
		t.Errorf("instruction[1] = %+v", got[1])
	}
}

func TestResolveAll_RemoteURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/checklist.md" {
			w.Write([]byte("# Security Checklist\n- Check XSS"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	mock := &mockProvider{}
	evt := &provider.Event{Repository: "test-repo", Organization: "org"}
	repo := &agentv1alpha1.Repository{}
	resolver := NewResolver(mock, evt, testLogger())
	varResolver := template.NewVariableResolver(evt, repo, mock, testLogger(), "")

	refs := []string{server.URL + "/checklist.md"}
	got := resolver.ResolveAll(context.Background(), refs, varResolver)

	if len(got) != 1 {
		t.Fatalf("got %d instructions, want 1", len(got))
	}

	if got[0].URL != server.URL+"/checklist.md" {
		t.Errorf("URL = %q", got[0].URL)
	}
	if got[0].Content != "# Security Checklist\n- Check XSS" {
		t.Errorf("Content = %q", got[0].Content)
	}
	if got[0].Path != "" {
		t.Errorf("Path should be empty for URL refs, got %q", got[0].Path)
	}
}

func TestResolveAll_TemplateVariables(t *testing.T) {
	mock := &mockProvider{
		getFileFn: func(_ context.Context, _ *provider.Event, _ string) (string, error) {
			return "Review PR #{{ pull_request_number }} in {{ repo_name }}", nil
		},
	}

	evt := &provider.Event{
		Repository:        "yeet",
		Organization:      "org",
		PullRequestNumber: 42,
	}
	repo := &agentv1alpha1.Repository{}
	resolver := NewResolver(mock, evt, testLogger())
	varResolver := template.NewVariableResolver(evt, repo, mock, testLogger(), "")

	refs := []string{"guide.md"}
	got := resolver.ResolveAll(context.Background(), refs, varResolver)

	if len(got) != 1 {
		t.Fatalf("got %d instructions, want 1", len(got))
	}

	want := "Review PR #42 in yeet"
	if got[0].Content != want {
		t.Errorf("Content = %q, want %q", got[0].Content, want)
	}
}

func TestResolveAll_FetchFailureSkipsInstruction(t *testing.T) {
	mock := &mockProvider{
		getFileFn: func(_ context.Context, _ *provider.Event, path string) (string, error) {
			if path == "good.md" {
				return "good content", nil
			}
			return "", fmt.Errorf("file not found: %s", path)
		},
	}

	evt := &provider.Event{Repository: "test-repo", Organization: "org"}
	repo := &agentv1alpha1.Repository{}
	resolver := NewResolver(mock, evt, testLogger())
	varResolver := template.NewVariableResolver(evt, repo, mock, testLogger(), "")

	refs := []string{"missing.md", "good.md", "also-missing.md"}
	got := resolver.ResolveAll(context.Background(), refs, varResolver)

	if len(got) != 1 {
		t.Fatalf("got %d instructions, want 1 (failures should be skipped)", len(got))
	}
	if got[0].Name != "good.md" {
		t.Errorf("expected good.md, got %q", got[0].Name)
	}
}

func TestResolveAll_URLFetchFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	mock := &mockProvider{}
	evt := &provider.Event{Repository: "test-repo", Organization: "org"}
	repo := &agentv1alpha1.Repository{}
	resolver := NewResolver(mock, evt, testLogger())
	varResolver := template.NewVariableResolver(evt, repo, mock, testLogger(), "")

	refs := []string{server.URL + "/missing.md"}
	got := resolver.ResolveAll(context.Background(), refs, varResolver)

	if len(got) != 0 {
		t.Fatalf("got %d instructions, want 0 (404 should be skipped)", len(got))
	}
}

func TestNameFromRef(t *testing.T) {
	tests := []struct {
		ref  string
		want string
	}{
		{"CLAUDE.md", "CLAUDE.md"},
		{".tekton/agents/review-guide.md", "review-guide.md"},
		{"https://example.com/path/to/checklist.md", "checklist.md"},
		{"http://example.com/file.txt", "file.txt"},
	}

	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			got := nameFromRef(tt.ref)
			if got != tt.want {
				t.Errorf("nameFromRef(%q) = %q, want %q", tt.ref, got, tt.want)
			}
		})
	}
}
