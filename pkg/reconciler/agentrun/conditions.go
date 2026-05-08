package agentrun

import (
	"knative.dev/pkg/apis"

	agentv1alpha1 "github.com/theakshaypant/agents-as-code/pkg/apis/agent/v1alpha1"
)

const (
	ConditionSandboxReady     apis.ConditionType = "SandboxReady"
	ConditionAgentExecuted    apis.ConditionType = "AgentExecuted"
	ConditionResultsCollected apis.ConditionType = "ResultsCollected"
	ConditionActionsExecuted  apis.ConditionType = "ActionsExecuted"
)

var conditionSet = apis.NewBatchConditionSet(
	ConditionSandboxReady,
	ConditionAgentExecuted,
	ConditionResultsCollected,
	ConditionActionsExecuted,
)

func conditionManager(run *agentv1alpha1.AgentRun) apis.ConditionManager {
	return conditionSet.Manage(&run.Status)
}

func IsTerminal(run *agentv1alpha1.AgentRun) bool {
	c := conditionManager(run).GetTopLevelCondition()
	if c == nil {
		return false
	}
	return c.IsTrue() || c.IsFalse()
}

// GetCurrentPhase returns the condition type representing the current phase.
// It finds the first sub-condition that is not yet True.
func GetCurrentPhase(run *agentv1alpha1.AgentRun) apis.ConditionType {
	mgr := conditionManager(run)

	phases := []apis.ConditionType{
		ConditionSandboxReady,
		ConditionAgentExecuted,
		ConditionResultsCollected,
		ConditionActionsExecuted,
	}

	for _, phase := range phases {
		c := mgr.GetCondition(phase)
		if c == nil || !c.IsTrue() {
			return phase
		}
	}

	return apis.ConditionSucceeded
}
