package provider

import (
	"context"
	"net/http"

	"go.uber.org/zap"
)

// ReviewComment represents an inline code review comment on a specific file and line.
type ReviewComment struct {
	Path string
	Line int
	Body string
}

type Interface interface {
	Detect(req *http.Request, payload string, logger *zap.SugaredLogger) (bool, bool, *zap.SugaredLogger, string, error)
	ParsePayload(ctx context.Context, req *http.Request, payload string) (*Event, error)
	Validate(ctx context.Context, event *Event) error
	SetClient(ctx context.Context, event *Event, token, webhookSecret string) error
	GetFiles(ctx context.Context, event *Event) ([]string, error)
	GetAgentDir(ctx context.Context, event *Event, path string) (string, error)
	CheckPermission(ctx context.Context, event *Event) (bool, error)

	GetPullRequestDescription(ctx context.Context, event *Event) (string, error)
	GetPullRequestDiff(ctx context.Context, event *Event) (string, error)
	GetPullRequestReviews(ctx context.Context, event *Event) (string, error)
	GetPullRequestComments(ctx context.Context, event *Event) (string, error)

	GetIssueTitle(ctx context.Context, event *Event) (string, error)
	GetIssueBody(ctx context.Context, event *Event) (string, error)
	GetIssueComments(ctx context.Context, event *Event) (string, error)
	GetIssueLabels(ctx context.Context, event *Event) (string, error)

	CreateComment(ctx context.Context, event *Event, body string) (string, error)
	CreateReview(ctx context.Context, event *Event, reviewEvent, body string, comments []ReviewComment) (string, error)
	AddLabels(ctx context.Context, event *Event, labels []string) error
	RemoveLabels(ctx context.Context, event *Event, labels []string) error
	CreatePullRequest(ctx context.Context, event *Event, title, body, head, base string) (string, error)
	SetCommitStatus(ctx context.Context, event *Event, statusContext, state, description, targetURL string) error
	CreateCommit(ctx context.Context, event *Event, message string, files map[string]string) (string, error)
}
