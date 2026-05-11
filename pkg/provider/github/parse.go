package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	gh "github.com/google/go-github/v84/github"
	"github.com/theakshaypant/agents-as-code/pkg/provider"
)

func (p *Provider) ParsePayload(_ context.Context, req *http.Request, payload string) (*provider.Event, error) {
	evt := provider.NewEvent()
	evt.Request = &provider.Request{
		Header:  req.Header,
		Payload: []byte(payload),
	}
	evt.EventType = req.Header.Get("X-GitHub-Event")

	installationID, _ := extractInstallationID(payload)
	evt.InstallationID = installationID

	eventInt, err := gh.ParseWebHook(evt.EventType, []byte(payload))
	if err != nil {
		return nil, fmt.Errorf("parsing webhook: %w", err)
	}

	switch e := eventInt.(type) {
	case *gh.PushEvent:
		return parsePushEvent(evt, e), nil
	case *gh.PullRequestEvent:
		return parsePullRequestEvent(evt, e), nil
	case *gh.PullRequestReviewEvent:
		return parsePullRequestReviewEvent(evt, e), nil
	case *gh.IssueCommentEvent:
		return parseIssueCommentEvent(evt, e), nil
	case *gh.IssuesEvent:
		return parseIssuesEvent(evt, e), nil
	default:
		return nil, fmt.Errorf("unsupported event type %q", evt.EventType)
	}
}

func parsePushEvent(evt *provider.Event, e *gh.PushEvent) *provider.Event {
	evt.TriggerType = provider.TriggerPush
	evt.Organization = e.GetRepo().GetOwner().GetLogin()
	evt.Repository = e.GetRepo().GetName()
	evt.URL = e.GetRepo().GetHTMLURL()
	evt.DefaultBranch = e.GetRepo().GetDefaultBranch()
	evt.SHA = e.GetHeadCommit().GetID()
	evt.SHATitle = e.GetHeadCommit().GetMessage()
	evt.SHAURL = e.GetHeadCommit().GetURL()
	evt.Sender = e.GetSender().GetLogin()
	evt.BaseBranch = stripRefsPrefix(e.GetRef())
	evt.HeadBranch = evt.BaseBranch

	return evt
}

func parsePullRequestEvent(evt *provider.Event, e *gh.PullRequestEvent) *provider.Event {
	evt.TriggerType = provider.TriggerPullRequest
	if e.GetAction() == "labeled" {
		evt.TriggerType = provider.TriggerPRLabeled
		evt.Label = e.GetLabel().GetName()
	}
	evt.Organization = e.GetRepo().GetOwner().GetLogin()
	evt.Repository = e.GetRepo().GetName()
	evt.URL = e.GetRepo().GetHTMLURL()
	evt.DefaultBranch = e.GetRepo().GetDefaultBranch()
	evt.SHA = e.GetPullRequest().GetHead().GetSHA()
	evt.BaseBranch = e.GetPullRequest().GetBase().GetRef()
	evt.HeadBranch = e.GetPullRequest().GetHead().GetRef()
	evt.Sender = e.GetPullRequest().GetUser().GetLogin()
	evt.PullRequestNumber = e.GetPullRequest().GetNumber()
	evt.PullRequestTitle = e.GetPullRequest().GetTitle()
	evt.PullRequestAuthor = e.GetPullRequest().GetUser().GetLogin()
	evt.PullRequestURL = e.GetPullRequest().GetHTMLURL()

	return evt
}

func parsePullRequestReviewEvent(evt *provider.Event, e *gh.PullRequestReviewEvent) *provider.Event {
	evt.TriggerType = provider.TriggerPullRequestReview
	evt.Organization = e.GetRepo().GetOwner().GetLogin()
	evt.Repository = e.GetRepo().GetName()
	evt.URL = e.GetRepo().GetHTMLURL()
	evt.DefaultBranch = e.GetRepo().GetDefaultBranch()
	evt.SHA = e.GetPullRequest().GetHead().GetSHA()
	evt.BaseBranch = e.GetPullRequest().GetBase().GetRef()
	evt.HeadBranch = e.GetPullRequest().GetHead().GetRef()
	evt.Sender = e.GetSender().GetLogin()
	evt.PullRequestNumber = e.GetPullRequest().GetNumber()
	evt.PullRequestTitle = e.GetPullRequest().GetTitle()
	evt.PullRequestAuthor = e.GetPullRequest().GetUser().GetLogin()
	evt.PullRequestURL = e.GetPullRequest().GetHTMLURL()

	// TODO: extract review body, state (approved/changes_requested/commented)
	return evt
}

func parseIssueCommentEvent(evt *provider.Event, e *gh.IssueCommentEvent) *provider.Event {
	evt.TriggerType = provider.TriggerIssueComment
	evt.Organization = e.GetRepo().GetOwner().GetLogin()
	evt.Repository = e.GetRepo().GetName()
	evt.URL = e.GetRepo().GetHTMLURL()
	evt.DefaultBranch = e.GetRepo().GetDefaultBranch()
	evt.Sender = e.GetSender().GetLogin()
	evt.IssueNumber = e.GetIssue().GetNumber()
	if e.GetIssue().IsPullRequest() {
		evt.PullRequestNumber = e.GetIssue().GetNumber()
	}
	evt.CommentBody = e.GetComment().GetBody()

	// TODO: if PR comment, fetch full PR details to populate SHA, branches
	return evt
}

func parseIssuesEvent(evt *provider.Event, e *gh.IssuesEvent) *provider.Event {
	evt.TriggerType = provider.TriggerIssueLabeled
	evt.Organization = e.GetRepo().GetOwner().GetLogin()
	evt.Repository = e.GetRepo().GetName()
	evt.URL = e.GetRepo().GetHTMLURL()
	evt.DefaultBranch = e.GetRepo().GetDefaultBranch()
	evt.Sender = e.GetSender().GetLogin()
	evt.IssueNumber = e.GetIssue().GetNumber()
	evt.Label = e.GetLabel().GetName()

	return evt
}

type installationPayload struct {
	Installation struct {
		ID *int64 `json:"id"`
	} `json:"installation"`
}

func stripRefsPrefix(ref string) string {
	ref = strings.TrimPrefix(ref, "refs/heads/")
	ref = strings.TrimPrefix(ref, "refs/tags/")
	return ref
}

func extractInstallationID(payload string) (int64, error) {
	var data installationPayload
	if err := json.Unmarshal([]byte(payload), &data); err != nil {
		return -1, err
	}
	if data.Installation.ID != nil {
		return *data.Installation.ID, nil
	}
	return -1, nil
}
