package provider

import "net/http"

type TriggerType string

const (
	TriggerPush              TriggerType = "push"
	TriggerPullRequest       TriggerType = "pull_request"
	TriggerPullRequestReview TriggerType = "pull_request_review"
	TriggerIssueComment      TriggerType = "issue_comment"
	TriggerIssueLabeled      TriggerType = "issues_labeled"
	TriggerPRLabeled         TriggerType = "pull_request_labeled"
)

type Event struct {
	EventType   string
	TriggerType TriggerType

	Organization  string
	Repository    string
	URL           string
	DefaultBranch string

	BaseBranch string
	HeadBranch string
	SHA        string
	SHATitle   string
	SHAURL     string

	PullRequestNumber int
	PullRequestTitle  string

	CommentBody string

	Sender string

	Provider *ProviderInfo
	Request  *Request

	InstallationID int64
}

type ProviderInfo struct {
	Token         string
	WebhookSecret string
}

type Request struct {
	Header  http.Header
	Payload []byte
}

func NewEvent() *Event {
	return &Event{
		Provider: &ProviderInfo{},
		Request:  &Request{},
	}
}
