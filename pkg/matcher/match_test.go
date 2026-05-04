package matcher

import (
	"testing"

	"github.com/theakshaypant/agents-as-code/pkg/apis/agent/keys"
	agentv1alpha1 "github.com/theakshaypant/agents-as-code/pkg/apis/agent/v1alpha1"
	"github.com/theakshaypant/agents-as-code/pkg/provider"
	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func testLogger() *zap.SugaredLogger {
	l, _ := zap.NewDevelopment()
	return l.Sugar()
}

func makeAgent(name string, annots map[string]string) agentv1alpha1.Agent {
	return agentv1alpha1.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Annotations: annots,
		},
		Spec: agentv1alpha1.AgentSpec{
			SystemPrompt: "test",
			Limits:       agentv1alpha1.AgentLimits{MaxTokens: 1000, TimeoutSeconds: 30},
		},
	}
}

func TestMatchAgentsToEvent_StandardMatching(t *testing.T) {
	agents := []agentv1alpha1.Agent{
		makeAgent("push-agent", map[string]string{
			keys.OnEvent:        "push",
			keys.OnTargetBranch: "main",
		}),
		makeAgent("pr-agent", map[string]string{
			keys.OnEvent: "pull_request",
		}),
		makeAgent("no-match", map[string]string{
			keys.OnEvent:        "push",
			keys.OnTargetBranch: "release-*",
		}),
	}

	evt := &provider.Event{
		TriggerType: provider.TriggerPush,
		BaseBranch:  "main",
	}

	matches := MatchAgentsToEvent(agents, evt, nil, testLogger())
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	if matches[0].Agent.Name != "push-agent" {
		t.Errorf("expected push-agent, got %s", matches[0].Agent.Name)
	}
}

func TestMatchAgentsToEvent_CommentSeparateTrack(t *testing.T) {
	agents := []agentv1alpha1.Agent{
		makeAgent("comment-agent", map[string]string{
			keys.OnEvent:   "issue_comment",
			keys.OnComment: "/hello",
		}),
		makeAgent("standard-agent", map[string]string{
			keys.OnEvent: "issue_comment",
		}),
	}

	evt := &provider.Event{
		TriggerType: provider.TriggerIssueComment,
		CommentBody: "/hello world",
	}

	matches := MatchAgentsToEvent(agents, evt, nil, testLogger())

	// comment-agent should match via on-comment track.
	// standard-agent has on-event: issue_comment but no on-comment,
	// so it goes through standard matching and also matches.
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(matches))
	}
}

func TestMatchAgentsToEvent_CommentNoMatch(t *testing.T) {
	agents := []agentv1alpha1.Agent{
		makeAgent("comment-agent", map[string]string{
			keys.OnEvent:   "issue_comment",
			keys.OnComment: "^/deploy$",
		}),
	}

	evt := &provider.Event{
		TriggerType: provider.TriggerIssueComment,
		CommentBody: "/hello",
	}

	matches := MatchAgentsToEvent(agents, evt, nil, testLogger())
	if len(matches) != 0 {
		t.Fatalf("expected 0 matches, got %d", len(matches))
	}
}

func TestMatchAgentsToEvent_PathChange(t *testing.T) {
	agents := []agentv1alpha1.Agent{
		makeAgent("path-agent", map[string]string{
			keys.OnEvent:      "push",
			keys.OnPathChange: "pkg/*",
		}),
	}

	t.Run("matching files", func(t *testing.T) {
		evt := &provider.Event{TriggerType: provider.TriggerPush, BaseBranch: "main"}
		matches := MatchAgentsToEvent(agents, evt, []string{"pkg/main.go"}, testLogger())
		if len(matches) != 1 {
			t.Fatalf("expected 1 match, got %d", len(matches))
		}
	})

	t.Run("no matching files", func(t *testing.T) {
		evt := &provider.Event{TriggerType: provider.TriggerPush, BaseBranch: "main"}
		matches := MatchAgentsToEvent(agents, evt, []string{"docs/readme.md"}, testLogger())
		if len(matches) != 0 {
			t.Fatalf("expected 0 matches, got %d", len(matches))
		}
	})
}

func TestMatchAgentsToEvent_LabelMatching(t *testing.T) {
	agents := []agentv1alpha1.Agent{
		makeAgent("label-agent", map[string]string{
			keys.OnEvent: "pull_request_labeled",
			keys.OnLabel: "[bug, security]",
		}),
	}

	t.Run("matching label", func(t *testing.T) {
		evt := &provider.Event{TriggerType: provider.TriggerPRLabeled, Label: "bug"}
		matches := MatchAgentsToEvent(agents, evt, nil, testLogger())
		if len(matches) != 1 {
			t.Fatalf("expected 1 match, got %d", len(matches))
		}
	})

	t.Run("non-matching label", func(t *testing.T) {
		evt := &provider.Event{TriggerType: provider.TriggerPRLabeled, Label: "docs"}
		matches := MatchAgentsToEvent(agents, evt, nil, testLogger())
		if len(matches) != 0 {
			t.Fatalf("expected 0 matches, got %d", len(matches))
		}
	})
}

func TestMatchAgentsToEvent_MultipleAnnotationsAND(t *testing.T) {
	agents := []agentv1alpha1.Agent{
		makeAgent("strict-agent", map[string]string{
			keys.OnEvent:        "push",
			keys.OnTargetBranch: "main",
			keys.OnPathChange:   "pkg/*",
		}),
	}

	evt := &provider.Event{
		TriggerType: provider.TriggerPush,
		BaseBranch:  "main",
	}

	t.Run("all conditions met", func(t *testing.T) {
		matches := MatchAgentsToEvent(agents, evt, []string{"pkg/foo.go"}, testLogger())
		if len(matches) != 1 {
			t.Fatalf("expected 1 match, got %d", len(matches))
		}
	})

	t.Run("path not met", func(t *testing.T) {
		matches := MatchAgentsToEvent(agents, evt, []string{"docs/readme.md"}, testLogger())
		if len(matches) != 0 {
			t.Fatalf("expected 0 matches, got %d", len(matches))
		}
	})

	t.Run("branch not met", func(t *testing.T) {
		evt2 := &provider.Event{TriggerType: provider.TriggerPush, BaseBranch: "develop"}
		matches := MatchAgentsToEvent(agents, evt2, []string{"pkg/foo.go"}, testLogger())
		if len(matches) != 0 {
			t.Fatalf("expected 0 matches, got %d", len(matches))
		}
	})
}

func TestMatchAgentsToEvent_LabelEventWithoutOnLabel(t *testing.T) {
	agents := []agentv1alpha1.Agent{
		makeAgent("push-agent", map[string]string{
			keys.OnEvent: "[push, pull_request_labeled]",
		}),
	}

	// Label events should NOT match agents without on-label (PaC behavior)
	evt := &provider.Event{TriggerType: provider.TriggerPRLabeled, Label: "bug"}
	matches := MatchAgentsToEvent(agents, evt, nil, testLogger())
	if len(matches) != 0 {
		t.Fatalf("expected 0 matches for label event without on-label, got %d", len(matches))
	}
}
