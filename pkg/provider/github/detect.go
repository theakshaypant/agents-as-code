package github

import (
	"fmt"
	"net/http"
	"slices"

	gh "github.com/google/go-github/v84/github"
	"github.com/theakshaypant/agents-as-code/pkg/provider"
	"go.uber.org/zap"
)

var supportedPRActions = []string{"opened", "synchronize", "synchronized", "reopened"}

func (p *Provider) Detect(req *http.Request, payload string, logger *zap.SugaredLogger) (bool, bool, *zap.SugaredLogger, string, error) {
	if h := req.Header.Get("X-Gitea-Event-Type"); h != "" {
		return false, false, logger, "", nil
	}

	eventType := req.Header.Get("X-Github-Event")
	if eventType == "" {
		return false, false, logger, "", nil
	}

	logger = logger.With("provider", "github", "event-id", req.Header.Get("X-GitHub-Delivery"))

	eventInt, err := gh.ParseWebHook(eventType, []byte(payload))
	if err != nil {
		return true, false, logger, "", err
	}

	triggerType, reason := detectTriggerType(eventType, eventInt)
	if triggerType == "" {
		return true, false, logger, reason, nil
	}

	return true, true, logger, "", nil
}

func detectTriggerType(ghEventType string, eventInt any) (provider.TriggerType, string) {
	switch event := eventInt.(type) {
	case *gh.PushEvent:
		if event.GetPusher() != nil {
			return provider.TriggerPush, ""
		}
		return "", "no pusher in payload"

	case *gh.PullRequestEvent:
		if event.GetAction() == "labeled" {
			return provider.TriggerPRLabeled, ""
		}
		if slices.Contains(supportedPRActions, event.GetAction()) {
			return provider.TriggerPullRequest, ""
		}
		return "", fmt.Sprintf("pull_request: unsupported action %q", event.GetAction())

	case *gh.PullRequestReviewEvent:
		if event.GetAction() == "submitted" {
			return provider.TriggerPullRequestReview, ""
		}
		return "", fmt.Sprintf("pull_request_review: unsupported action %q", event.GetAction())

	case *gh.IssueCommentEvent:
		if event.GetAction() != "created" {
			return "", fmt.Sprintf("issue_comment: unsupported action %q", event.GetAction())
		}
		return provider.TriggerIssueComment, ""

	case *gh.IssuesEvent:
		if event.GetAction() == "labeled" {
			return provider.TriggerIssueLabeled, ""
		}
		return "", fmt.Sprintf("issues: unsupported action %q", event.GetAction())
	}

	return "", fmt.Sprintf("unsupported event type %q", ghEventType)
}
