package matcher

import (
	"testing"

	"github.com/theakshaypant/agents-as-code/pkg/apis/agent/keys"
	"github.com/theakshaypant/agents-as-code/pkg/provider"
)

func TestGetAnnotationValues(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []string
		wantErr bool
	}{
		{"single value", "push", []string{"push"}, false},
		{"bracket array", "[push, pull_request]", []string{"push", "pull_request"}, false},
		{"whitespace", "  push  ", []string{"push"}, false},
		{"bracket whitespace", "[ push , pull_request ]", []string{"push", "pull_request"}, false},
		{"empty string", "", nil, false},
		{"empty brackets", "[]", nil, true},
		{"html comma", "doc/gen&#44;*", []string{"doc/gen,*"}, false},
		{"bracket html comma", "[doc/gen&#44;*, cmd/*]", []string{"doc/gen,*", "cmd/*"}, false},
		{"invalid format", "[push", nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := getAnnotationValues(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("getAnnotationValues(%q) error = %v, wantErr = %v", tt.input, err, tt.wantErr)
			}
			if !tt.wantErr {
				if len(got) != len(tt.want) {
					t.Fatalf("getAnnotationValues(%q) = %v, want %v", tt.input, got, tt.want)
				}
				for i := range got {
					if got[i] != tt.want[i] {
						t.Errorf("getAnnotationValues(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
					}
				}
			}
		})
	}
}

func TestMatchEvent(t *testing.T) {
	tests := []struct {
		name   string
		annots map[string]string
		evt    *provider.Event
		want   bool
	}{
		{
			"matches single event",
			map[string]string{keys.OnEvent: "push"},
			&provider.Event{TriggerType: provider.TriggerPush},
			true,
		},
		{
			"matches from array",
			map[string]string{keys.OnEvent: "[push, pull_request]"},
			&provider.Event{TriggerType: provider.TriggerPullRequest},
			true,
		},
		{
			"no match",
			map[string]string{keys.OnEvent: "push"},
			&provider.Event{TriggerType: provider.TriggerPullRequest},
			false,
		},
		{
			"missing annotation",
			map[string]string{},
			&provider.Event{TriggerType: provider.TriggerPush},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchEvent(tt.annots, tt.evt); got != tt.want {
				t.Errorf("matchEvent() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMatchTargetBranch(t *testing.T) {
	tests := []struct {
		name   string
		annots map[string]string
		evt    *provider.Event
		want   bool
	}{
		{
			"absent matches all",
			map[string]string{},
			&provider.Event{BaseBranch: "main"},
			true,
		},
		{
			"exact match",
			map[string]string{keys.OnTargetBranch: "main"},
			&provider.Event{BaseBranch: "main"},
			true,
		},
		{
			"glob match",
			map[string]string{keys.OnTargetBranch: "release-*"},
			&provider.Event{BaseBranch: "release-1.0"},
			true,
		},
		{
			"no match",
			map[string]string{keys.OnTargetBranch: "main"},
			&provider.Event{BaseBranch: "develop"},
			false,
		},
		{
			"refs/heads normalization",
			map[string]string{keys.OnTargetBranch: "main"},
			&provider.Event{BaseBranch: "refs/heads/main"},
			true,
		},
		{
			"array match",
			map[string]string{keys.OnTargetBranch: "[main, release-*]"},
			&provider.Event{BaseBranch: "release-2.0"},
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchTargetBranch(tt.annots, tt.evt); got != tt.want {
				t.Errorf("matchTargetBranch() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMatchComment(t *testing.T) {
	tests := []struct {
		name   string
		annots map[string]string
		evt    *provider.Event
		want   CommentMatchResult
	}{
		{
			"not applicable when absent",
			map[string]string{},
			&provider.Event{TriggerType: provider.TriggerIssueComment, CommentBody: "/hello"},
			CommentNotApplicable,
		},
		{
			"not matched on non-comment event",
			map[string]string{keys.OnComment: "/hello"},
			&provider.Event{TriggerType: provider.TriggerPush},
			CommentNotMatched,
		},
		{
			"matched regex",
			map[string]string{keys.OnComment: "/hello"},
			&provider.Event{TriggerType: provider.TriggerIssueComment, CommentBody: "/hello world"},
			CommentMatched,
		},
		{
			"anchored regex no match",
			map[string]string{keys.OnComment: "^/hello$"},
			&provider.Event{TriggerType: provider.TriggerIssueComment, CommentBody: "/hello world"},
			CommentNotMatched,
		},
		{
			"anchored regex match",
			map[string]string{keys.OnComment: "^/hello$"},
			&provider.Event{TriggerType: provider.TriggerIssueComment, CommentBody: "/hello"},
			CommentMatched,
		},
		{
			"array any match",
			map[string]string{keys.OnComment: "[/hello, /assign]"},
			&provider.Event{TriggerType: provider.TriggerIssueComment, CommentBody: "/assign me"},
			CommentMatched,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchComment(tt.annots, tt.evt); got != tt.want {
				t.Errorf("matchComment() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMatchPathChange(t *testing.T) {
	tests := []struct {
		name         string
		annots       map[string]string
		changedFiles []string
		want         bool
	}{
		{
			"absent passes",
			map[string]string{},
			[]string{"pkg/main.go"},
			true,
		},
		{
			"nil files with annotation fails",
			map[string]string{keys.OnPathChange: "pkg/*"},
			nil,
			false,
		},
		{
			"glob match",
			map[string]string{keys.OnPathChange: "pkg/*"},
			[]string{"pkg/main.go", "cmd/root.go"},
			true,
		},
		{
			"no match",
			map[string]string{keys.OnPathChange: "docs/*"},
			[]string{"pkg/main.go"},
			false,
		},
		{
			"array any match",
			map[string]string{keys.OnPathChange: "[pkg/*, cmd/*]"},
			[]string{"cmd/root.go"},
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchPathChange(tt.annots, tt.changedFiles); got != tt.want {
				t.Errorf("matchPathChange() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMatchLabel(t *testing.T) {
	tests := []struct {
		name   string
		annots map[string]string
		evt    *provider.Event
		want   bool
	}{
		{
			"absent fails for label events",
			map[string]string{},
			&provider.Event{TriggerType: provider.TriggerPRLabeled, Label: "bug"},
			false,
		},
		{
			"absent passes for non-label events",
			map[string]string{},
			&provider.Event{TriggerType: provider.TriggerPush},
			true,
		},
		{
			"non-label event fails",
			map[string]string{keys.OnLabel: "bug"},
			&provider.Event{TriggerType: provider.TriggerPush},
			false,
		},
		{
			"exact match",
			map[string]string{keys.OnLabel: "bug"},
			&provider.Event{TriggerType: provider.TriggerPRLabeled, Label: "bug"},
			true,
		},
		{
			"no match",
			map[string]string{keys.OnLabel: "bug"},
			&provider.Event{TriggerType: provider.TriggerPRLabeled, Label: "feature"},
			false,
		},
		{
			"array match",
			map[string]string{keys.OnLabel: "[bug, enhancement]"},
			&provider.Event{TriggerType: provider.TriggerIssueLabeled, Label: "enhancement"},
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchLabel(tt.annots, tt.evt); got != tt.want {
				t.Errorf("matchLabel() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBranchMatch(t *testing.T) {
	tests := []struct {
		pattern string
		branch  string
		want    bool
	}{
		{"main", "main", true},
		{"main", "develop", false},
		{"release-*", "release-1.0", true},
		{"release-*", "main", false},
		{"refs/heads/main", "refs/heads/main", true},
		{"main", "refs/heads/main", true},
		{"refs/heads/main", "main", true},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_vs_"+tt.branch, func(t *testing.T) {
			if got := branchMatch(tt.pattern, tt.branch); got != tt.want {
				t.Errorf("branchMatch(%q, %q) = %v, want %v", tt.pattern, tt.branch, got, tt.want)
			}
		})
	}
}

func TestTriggerAnnotations(t *testing.T) {
	annots := map[string]string{
		keys.OnEvent:        "push",
		keys.OnTargetBranch: "main",
		"unrelated/key":     "value",
	}
	got := TriggerAnnotations(annots)
	if len(got) != 2 {
		t.Fatalf("TriggerAnnotations() returned %d keys, want 2", len(got))
	}
	if got[keys.OnEvent] != "push" {
		t.Errorf("missing on-event")
	}
	if got[keys.OnTargetBranch] != "main" {
		t.Errorf("missing on-target-branch")
	}
	if _, ok := got["unrelated/key"]; ok {
		t.Errorf("should not include unrelated keys")
	}

	if got := TriggerAnnotations(nil); got != nil {
		t.Errorf("TriggerAnnotations(nil) = %v, want nil", got)
	}
}
