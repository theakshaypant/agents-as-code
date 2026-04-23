package matcher

import (
	"path/filepath"
	"strings"

	agentv1alpha1 "github.com/theakshaypant/yeet/pkg/apis/agent/v1alpha1"
	"github.com/theakshaypant/yeet/pkg/provider"
	"go.uber.org/zap"
)

type AgentMatch struct {
	Agent   *agentv1alpha1.Agent
	Trigger *agentv1alpha1.AgentTrigger
}

func MatchAgentsToEvent(agents []agentv1alpha1.Agent, evt *provider.Event, logger *zap.SugaredLogger) []AgentMatch {
	var matches []AgentMatch

	for i := range agents {
		agent := &agents[i]
		for j := range agent.Spec.On {
			trigger := &agent.Spec.On[j]

			if !matchEvent(trigger, evt) {
				continue
			}

			if !matchBranches(trigger, evt) {
				continue
			}

			if !matchComment(trigger, evt) {
				continue
			}

			logger.Infow("matched agent to event",
				"agent", agent.Name,
				"trigger_event", trigger.Event,
				"event_type", evt.TriggerType,
			)
			matches = append(matches, AgentMatch{
				Agent:   agent,
				Trigger: trigger,
			})
			break
		}
	}

	return matches
}

func matchEvent(trigger *agentv1alpha1.AgentTrigger, evt *provider.Event) bool {
	return trigger.Event == string(evt.TriggerType)
}

func matchBranches(trigger *agentv1alpha1.AgentTrigger, evt *provider.Event) bool {
	if len(trigger.Branches) == 0 {
		return true
	}

	branch := evt.BaseBranch
	for _, pattern := range trigger.Branches {
		if branchMatch(pattern, branch) {
			return true
		}
	}
	return false
}

func matchComment(trigger *agentv1alpha1.AgentTrigger, evt *provider.Event) bool {
	if trigger.Match == "" {
		return true
	}
	return strings.Contains(evt.CommentBody, trigger.Match)
}

func branchMatch(pattern, branch string) bool {
	pattern = strings.TrimPrefix(pattern, "refs/heads/")
	branch = strings.TrimPrefix(branch, "refs/heads/")

	matched, err := filepath.Match(pattern, branch)
	if err != nil {
		return false
	}
	return matched
}
