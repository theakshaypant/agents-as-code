package agentrun

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/apis"

	agentv1alpha1 "github.com/theakshaypant/agents-as-code/pkg/apis/agent/v1alpha1"
)

func newTestRun() *agentv1alpha1.AgentRun {
	return &agentv1alpha1.AgentRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-run",
			Namespace: "default",
		},
		Spec: agentv1alpha1.AgentRunSpec{
			AgentRef:      "test-agent",
			RepositoryRef: "test-repo",
			SystemPrompt:  "You are a test agent.",
			Limits: agentv1alpha1.AgentLimits{
				MaxTokens:      100000,
				TimeoutSeconds: 300,
			},
		},
	}
}

func TestIsTerminal(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(*agentv1alpha1.AgentRun)
		want   bool
	}{
		{
			name:  "no conditions",
			setup: func(_ *agentv1alpha1.AgentRun) {},
			want:  false,
		},
		{
			name: "initialized",
			setup: func(run *agentv1alpha1.AgentRun) {
				conditionManager(run).InitializeConditions()
			},
			want: false,
		},
		{
			name: "succeeded",
			setup: func(run *agentv1alpha1.AgentRun) {
				conditionManager(run).InitializeConditions()
				mgr := conditionManager(run)
				mgr.MarkTrue(ConditionSandboxReady)
				mgr.MarkTrue(ConditionAgentExecuted)
				mgr.MarkTrue(ConditionResultsCollected)
				mgr.MarkTrue(ConditionActionsExecuted)
			},
			want: true,
		},
		{
			name: "failed",
			setup: func(run *agentv1alpha1.AgentRun) {
				conditionManager(run).InitializeConditions()
				conditionManager(run).MarkFalse(ConditionSandboxReady, "CreateFailed", "sandbox creation failed")
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := newTestRun()
			tt.setup(run)
			if got := IsTerminal(run); got != tt.want {
				t.Errorf("IsTerminal() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetCurrentPhase(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*agentv1alpha1.AgentRun)
		want  apis.ConditionType
	}{
		{
			name:  "no conditions — starts at SandboxReady",
			setup: func(_ *agentv1alpha1.AgentRun) {},
			want:  ConditionSandboxReady,
		},
		{
			name: "sandbox ready — next is AgentExecuted",
			setup: func(run *agentv1alpha1.AgentRun) {
				conditionManager(run).InitializeConditions()
				conditionManager(run).MarkTrue(ConditionSandboxReady)
			},
			want: ConditionAgentExecuted,
		},
		{
			name: "agent executed — next is ResultsCollected",
			setup: func(run *agentv1alpha1.AgentRun) {
				conditionManager(run).InitializeConditions()
				mgr := conditionManager(run)
				mgr.MarkTrue(ConditionSandboxReady)
				mgr.MarkTrue(ConditionAgentExecuted)
			},
			want: ConditionResultsCollected,
		},
		{
			name: "results collected — next is ActionsExecuted",
			setup: func(run *agentv1alpha1.AgentRun) {
				conditionManager(run).InitializeConditions()
				mgr := conditionManager(run)
				mgr.MarkTrue(ConditionSandboxReady)
				mgr.MarkTrue(ConditionAgentExecuted)
				mgr.MarkTrue(ConditionResultsCollected)
			},
			want: ConditionActionsExecuted,
		},
		{
			name: "all done — Succeeded",
			setup: func(run *agentv1alpha1.AgentRun) {
				conditionManager(run).InitializeConditions()
				mgr := conditionManager(run)
				mgr.MarkTrue(ConditionSandboxReady)
				mgr.MarkTrue(ConditionAgentExecuted)
				mgr.MarkTrue(ConditionResultsCollected)
				mgr.MarkTrue(ConditionActionsExecuted)
			},
			want: apis.ConditionSucceeded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := newTestRun()
			tt.setup(run)
			if got := GetCurrentPhase(run); got != tt.want {
				t.Errorf("GetCurrentPhase() = %q, want %q", got, tt.want)
			}
		})
	}
}
