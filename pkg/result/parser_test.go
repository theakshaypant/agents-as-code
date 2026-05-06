package result

import (
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		wantErr bool
		want    func(*Result) bool
	}{
		{
			name: "full result with all action types",
			data: `{
				"actions": [
					{"type": "comment", "body": "hello"},
					{"type": "review", "event": "APPROVE", "body": "lgtm"},
					{"type": "label", "add": ["bug"], "remove": ["triage"]},
					{"type": "create-pr", "title": "fix", "head": "fix/x", "base": "main", "body": "desc"},
					{"type": "status", "context": "ci/review", "state": "success", "description": "passed"},
					{"type": "commit", "message": "fix", "files": {"a.go": "content"}}
				],
				"tokens_used": 5000,
				"cost_usd": "0.10"
			}`,
			want: func(r *Result) bool {
				return len(r.Actions) == 6 && r.TokensUsed == 5000 && r.CostUSD == "0.10"
			},
		},
		{
			name: "empty actions",
			data: `{"actions": []}`,
			want: func(r *Result) bool { return len(r.Actions) == 0 },
		},
		{
			name: "review with inline comments",
			data: `{
				"actions": [{
					"type": "review",
					"event": "REQUEST_CHANGES",
					"body": "needs work",
					"comments": [
						{"path": "main.go", "line": 10, "body": "fix this"}
					]
				}]
			}`,
			want: func(r *Result) bool {
				return len(r.Actions) == 1 &&
					len(r.Actions[0].Comments) == 1 &&
					r.Actions[0].Comments[0].Path == "main.go"
			},
		},
		{
			name: "commit with files",
			data: `{
				"actions": [{
					"type": "commit",
					"message": "fix lint",
					"files": {"README.md": "updated", "main.go": "package main"}
				}]
			}`,
			want: func(r *Result) bool {
				return len(r.Actions) == 1 &&
					r.Actions[0].Message == "fix lint" &&
					len(r.Actions[0].Files) == 2
			},
		},
		{
			name:    "empty data",
			data:    "",
			wantErr: true,
		},
		{
			name:    "invalid json",
			data:    "{invalid",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := Parse([]byte(tt.data))
			if (err != nil) != tt.wantErr {
				t.Fatalf("Parse() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.want != nil && !tt.want(r) {
				t.Fatalf("Parse() result did not match expectations")
			}
		})
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		result  *Result
		wantErr bool
	}{
		{
			name:    "nil result",
			result:  nil,
			wantErr: true,
		},
		{
			name:   "valid comment",
			result: &Result{Actions: []Action{{Type: "comment", Body: "hello"}}},
		},
		{
			name:    "comment missing body",
			result:  &Result{Actions: []Action{{Type: "comment"}}},
			wantErr: true,
		},
		{
			name:   "valid review",
			result: &Result{Actions: []Action{{Type: "review", Event: "APPROVE"}}},
		},
		{
			name:    "review missing event",
			result:  &Result{Actions: []Action{{Type: "review"}}},
			wantErr: true,
		},
		{
			name:    "review invalid event",
			result:  &Result{Actions: []Action{{Type: "review", Event: "INVALID"}}},
			wantErr: true,
		},
		{
			name: "review with valid inline comments",
			result: &Result{Actions: []Action{{
				Type: "review", Event: "COMMENT",
				Comments: []ReviewComment{{Path: "a.go", Line: 1, Body: "fix"}},
			}}},
		},
		{
			name: "review with missing comment path",
			result: &Result{Actions: []Action{{
				Type: "review", Event: "COMMENT",
				Comments: []ReviewComment{{Line: 1, Body: "fix"}},
			}}},
			wantErr: true,
		},
		{
			name: "review with invalid comment line",
			result: &Result{Actions: []Action{{
				Type: "review", Event: "COMMENT",
				Comments: []ReviewComment{{Path: "a.go", Line: 0, Body: "fix"}},
			}}},
			wantErr: true,
		},
		{
			name: "review with missing comment body",
			result: &Result{Actions: []Action{{
				Type: "review", Event: "COMMENT",
				Comments: []ReviewComment{{Path: "a.go", Line: 1}},
			}}},
			wantErr: true,
		},
		{
			name:   "valid label add",
			result: &Result{Actions: []Action{{Type: "label", Add: []string{"bug"}}}},
		},
		{
			name:   "valid label remove",
			result: &Result{Actions: []Action{{Type: "label", Remove: []string{"triage"}}}},
		},
		{
			name:    "label with no add or remove",
			result:  &Result{Actions: []Action{{Type: "label"}}},
			wantErr: true,
		},
		{
			name:   "valid create-pr",
			result: &Result{Actions: []Action{{Type: "create-pr", Title: "fix", Head: "fix/x", Base: "main"}}},
		},
		{
			name:    "create-pr missing title",
			result:  &Result{Actions: []Action{{Type: "create-pr", Head: "fix/x", Base: "main"}}},
			wantErr: true,
		},
		{
			name:    "create-pr missing head",
			result:  &Result{Actions: []Action{{Type: "create-pr", Title: "fix", Base: "main"}}},
			wantErr: true,
		},
		{
			name:    "create-pr missing base",
			result:  &Result{Actions: []Action{{Type: "create-pr", Title: "fix", Head: "fix/x"}}},
			wantErr: true,
		},
		{
			name:   "valid status",
			result: &Result{Actions: []Action{{Type: "status", Context: "ci/test", State: "success"}}},
		},
		{
			name:    "status missing context",
			result:  &Result{Actions: []Action{{Type: "status", State: "success"}}},
			wantErr: true,
		},
		{
			name:    "status missing state",
			result:  &Result{Actions: []Action{{Type: "status", Context: "ci/test"}}},
			wantErr: true,
		},
		{
			name:    "status invalid state",
			result:  &Result{Actions: []Action{{Type: "status", Context: "ci/test", State: "invalid"}}},
			wantErr: true,
		},
		{
			name:   "status with all valid states",
			result: &Result{Actions: []Action{{Type: "status", Context: "a", State: "success"}}},
		},
		{
			name: "valid commit",
			result: &Result{Actions: []Action{{
				Type: "commit", Message: "fix lint",
				Files: map[string]string{"README.md": "updated"},
			}}},
		},
		{
			name:    "commit missing message",
			result:  &Result{Actions: []Action{{Type: "commit", Files: map[string]string{"a.go": "x"}}}},
			wantErr: true,
		},
		{
			name:    "commit missing files",
			result:  &Result{Actions: []Action{{Type: "commit", Message: "fix"}}},
			wantErr: true,
		},
		{
			name:    "commit empty files map",
			result:  &Result{Actions: []Action{{Type: "commit", Message: "fix", Files: map[string]string{}}}},
			wantErr: true,
		},
		{
			name: "commit absolute path",
			result: &Result{Actions: []Action{{
				Type: "commit", Message: "fix",
				Files: map[string]string{"/etc/passwd": "x"},
			}}},
			wantErr: true,
		},
		{
			name: "commit path traversal",
			result: &Result{Actions: []Action{{
				Type: "commit", Message: "fix",
				Files: map[string]string{"../secret.txt": "x"},
			}}},
			wantErr: true,
		},
		{
			name:    "unknown action type",
			result:  &Result{Actions: []Action{{Type: "unknown"}}},
			wantErr: true,
		},
		{
			name: "multiple errors aggregated",
			result: &Result{Actions: []Action{
				{Type: "comment"},
				{Type: "review"},
			}},
			wantErr: true,
		},
		{
			name:   "empty actions is valid",
			result: &Result{Actions: []Action{}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(tt.result)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
