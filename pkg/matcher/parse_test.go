package matcher

import (
	"strings"
	"testing"
)

func TestParseAgentDefinitions_AnnotationFormat(t *testing.T) {
	yaml := `
apiVersion: agent.tekton.dev/v1alpha1
kind: Agent
metadata:
  name: test-agent
  annotations:
    agent.tekton.dev/on-event: "[push, pull_request]"
    agent.tekton.dev/on-target-branch: "main"
spec:
  purpose: Test agent
  limits:
    maxTokens: 1000
    timeoutSeconds: 30
`
	agents, err := ParseAgentDefinitions(yaml)
	if err != nil {
		t.Fatalf("ParseAgentDefinitions() error = %v", err)
	}
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(agents))
	}
	if agents[0].Name != "test-agent" {
		t.Errorf("expected name test-agent, got %s", agents[0].Name)
	}
	annots := agents[0].GetAnnotations()
	if annots["agent.tekton.dev/on-event"] != "[push, pull_request]" {
		t.Errorf("on-event annotation not preserved: %v", annots)
	}
}

func TestParseAgentDefinitions_MissingOnEvent(t *testing.T) {
	yaml := `
apiVersion: agent.tekton.dev/v1alpha1
kind: Agent
metadata:
  name: bad-agent
spec:
  purpose: Missing on-event
  limits:
    maxTokens: 1000
    timeoutSeconds: 30
`
	_, err := ParseAgentDefinitions(yaml)
	if err == nil {
		t.Fatal("expected error for missing on-event annotation")
	}
	if !strings.Contains(err.Error(), "on-event") {
		t.Errorf("error should mention on-event: %v", err)
	}
}

func TestParseAgentDefinitions_InvalidEventType(t *testing.T) {
	yaml := `
apiVersion: agent.tekton.dev/v1alpha1
kind: Agent
metadata:
  name: bad-agent
  annotations:
    agent.tekton.dev/on-event: "bogus_event"
spec:
  purpose: Invalid event type
  limits:
    maxTokens: 1000
    timeoutSeconds: 30
`
	_, err := ParseAgentDefinitions(yaml)
	if err == nil {
		t.Fatal("expected error for invalid event type")
	}
	if !strings.Contains(err.Error(), "bogus_event") {
		t.Errorf("error should mention bogus_event: %v", err)
	}
}

func TestParseAgentDefinitions_InvalidCommentRegex(t *testing.T) {
	yaml := `
apiVersion: agent.tekton.dev/v1alpha1
kind: Agent
metadata:
  name: bad-agent
  annotations:
    agent.tekton.dev/on-event: "issue_comment"
    agent.tekton.dev/on-comment: "[invalid"
spec:
  purpose: Invalid regex
  limits:
    maxTokens: 1000
    timeoutSeconds: 30
`
	_, err := ParseAgentDefinitions(yaml)
	if err == nil {
		t.Fatal("expected error for invalid regex")
	}
	if !strings.Contains(err.Error(), "on-comment") {
		t.Errorf("error should mention on-comment: %v", err)
	}
}

func TestParseAgentDefinitions_MultipleAgents(t *testing.T) {
	yaml := `
apiVersion: agent.tekton.dev/v1alpha1
kind: Agent
metadata:
  name: agent-a
  annotations:
    agent.tekton.dev/on-event: "push"
spec:
  purpose: Agent A
  limits:
    maxTokens: 1000
    timeoutSeconds: 30
---
apiVersion: agent.tekton.dev/v1alpha1
kind: Agent
metadata:
  name: agent-b
  annotations:
    agent.tekton.dev/on-event: "pull_request"
spec:
  purpose: Agent B
  limits:
    maxTokens: 2000
    timeoutSeconds: 60
`
	agents, err := ParseAgentDefinitions(yaml)
	if err != nil {
		t.Fatalf("ParseAgentDefinitions() error = %v", err)
	}
	if len(agents) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(agents))
	}
	if agents[0].Name != "agent-a" || agents[1].Name != "agent-b" {
		t.Errorf("agent names: %s, %s", agents[0].Name, agents[1].Name)
	}
}
