package matcher

import (
	agentv1alpha1 "github.com/theakshaypant/agents-as-code/pkg/apis/agent/v1alpha1"
	"github.com/theakshaypant/agents-as-code/pkg/provider"
	"go.uber.org/zap"
)

type AgentMatch struct {
	Agent *agentv1alpha1.Agent
}

func MatchAgentsToEvent(agents []agentv1alpha1.Agent, evt *provider.Event, changedFiles []string, logger *zap.SugaredLogger) []AgentMatch {
	var matches []AgentMatch

	for i := range agents {
		agent := &agents[i]
		annots := agent.GetAnnotations()

		// on-comment is a separate matching track (PaC behavior):
		// if present and matches, the agent is selected immediately.
		// if the event is a comment but on-comment doesn't match, skip entirely.
		commentResult := matchComment(annots, evt)
		if commentResult == CommentMatched {
			logger.Infow("matched agent to event via on-comment",
				"agent", agent.Name,
				"event_type", evt.TriggerType,
			)
			matches = append(matches, AgentMatch{Agent: agent})
			continue
		}
		if commentResult == CommentNotMatched {
			continue
		}

		// Standard matching: all present annotations must pass.
		if !matchEvent(annots, evt) {
			continue
		}
		if !matchTargetBranch(annots, evt) {
			continue
		}
		if !matchLabel(annots, evt) {
			continue
		}
		if !matchPathChange(annots, changedFiles) {
			continue
		}

		logger.Infow("matched agent to event",
			"agent", agent.Name,
			"event_type", evt.TriggerType,
		)
		matches = append(matches, AgentMatch{Agent: agent})
	}

	return matches
}
