package result

import (
	"context"
	"fmt"
	"strings"

	"go.uber.org/zap"

	agentv1alpha1 "github.com/theakshaypant/agents-as-code/pkg/apis/agent/v1alpha1"
	"github.com/theakshaypant/agents-as-code/pkg/provider"
)

type Executor struct {
	provider provider.Interface
	logger   *zap.SugaredLogger
}

func NewExecutor(p provider.Interface, logger *zap.SugaredLogger) *Executor {
	return &Executor{
		provider: p,
		logger:   logger,
	}
}

// Execute runs all actions in a Result via the provider.
// Returns audit records and any errors. Continues executing remaining
// actions even if one fails (partial success).
func (e *Executor) Execute(ctx context.Context, r *Result, event *provider.Event) ([]agentv1alpha1.AgentAction, error) {
	var actions []agentv1alpha1.AgentAction
	var errs []string

	for i, action := range r.Actions {
		e.logger.Infow("executing action", "index", i, "type", action.Type)

		audit, err := e.executeAction(ctx, &action, event)
		if err != nil {
			e.logger.Errorw("action failed", "index", i, "type", action.Type, "error", err)
			errs = append(errs, fmt.Sprintf("action[%d] (%s): %v", i, action.Type, err))
			continue
		}

		actions = append(actions, audit)
		e.logger.Infow("action succeeded", "index", i, "type", action.Type)
	}

	if len(errs) > 0 {
		return actions, fmt.Errorf("execution errors: %s", strings.Join(errs, "; "))
	}
	return actions, nil
}

func (e *Executor) executeAction(ctx context.Context, a *Action, event *provider.Event) (agentv1alpha1.AgentAction, error) {
	switch a.Type {
	case "comment":
		return e.executeComment(ctx, a, event)
	case "review":
		return e.executeReview(ctx, a, event)
	case "label":
		return e.executeLabel(ctx, a, event)
	case "create-pr":
		return e.executeCreatePR(ctx, a, event)
	case "status":
		return e.executeStatus(ctx, a, event)
	case "commit":
		return e.executeCommit(ctx, a, event)
	default:
		return agentv1alpha1.AgentAction{}, fmt.Errorf("unknown action type %q", a.Type)
	}
}

func (e *Executor) executeComment(ctx context.Context, a *Action, event *provider.Event) (agentv1alpha1.AgentAction, error) {
	url, err := e.provider.CreateComment(ctx, event, a.Body)
	if err != nil {
		return agentv1alpha1.AgentAction{}, fmt.Errorf("creating comment: %w", err)
	}
	return agentv1alpha1.AgentAction{Type: "pr-comment", URL: url}, nil
}

func (e *Executor) executeReview(ctx context.Context, a *Action, event *provider.Event) (agentv1alpha1.AgentAction, error) {
	provComments := make([]provider.ReviewComment, 0, len(a.Comments))
	for _, c := range a.Comments {
		provComments = append(provComments, provider.ReviewComment{
			Path: c.Path,
			Line: c.Line,
			Body: c.Body,
		})
	}

	url, err := e.provider.CreateReview(ctx, event, a.Event, a.Body, provComments)
	if err != nil {
		return agentv1alpha1.AgentAction{}, fmt.Errorf("creating review: %w", err)
	}
	return agentv1alpha1.AgentAction{Type: "pr-review", URL: url}, nil
}

func (e *Executor) executeLabel(ctx context.Context, a *Action, event *provider.Event) (agentv1alpha1.AgentAction, error) {
	if len(a.Add) > 0 {
		if err := e.provider.AddLabels(ctx, event, a.Add); err != nil {
			return agentv1alpha1.AgentAction{}, fmt.Errorf("adding labels: %w", err)
		}
	}
	if len(a.Remove) > 0 {
		if err := e.provider.RemoveLabels(ctx, event, a.Remove); err != nil {
			return agentv1alpha1.AgentAction{}, fmt.Errorf("removing labels: %w", err)
		}
	}
	return agentv1alpha1.AgentAction{Type: "label"}, nil
}

func (e *Executor) executeCreatePR(ctx context.Context, a *Action, event *provider.Event) (agentv1alpha1.AgentAction, error) {
	url, err := e.provider.CreatePullRequest(ctx, event, a.Title, a.Body, a.Head, a.Base)
	if err != nil {
		return agentv1alpha1.AgentAction{}, fmt.Errorf("creating pull request: %w", err)
	}
	return agentv1alpha1.AgentAction{Type: "create-pr", URL: url}, nil
}

func (e *Executor) executeStatus(ctx context.Context, a *Action, event *provider.Event) (agentv1alpha1.AgentAction, error) {
	if err := e.provider.SetCommitStatus(ctx, event, a.Context, a.State, a.Description, a.TargetURL); err != nil {
		return agentv1alpha1.AgentAction{}, fmt.Errorf("setting commit status: %w", err)
	}
	return agentv1alpha1.AgentAction{Type: "status-check"}, nil
}

func (e *Executor) executeCommit(ctx context.Context, a *Action, event *provider.Event) (agentv1alpha1.AgentAction, error) {
	sha, err := e.provider.CreateCommit(ctx, event, a.Message, a.Files)
	if err != nil {
		return agentv1alpha1.AgentAction{}, fmt.Errorf("creating commit: %w", err)
	}
	return agentv1alpha1.AgentAction{
		Type: "commit",
		SHA:  sha,
		URL:  fmt.Sprintf("https://github.com/%s/%s/commit/%s", event.Organization, event.Repository, sha),
	}, nil
}
