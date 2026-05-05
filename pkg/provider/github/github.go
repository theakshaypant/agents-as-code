package github

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	gh "github.com/google/go-github/v84/github"
	"github.com/theakshaypant/agents-as-code/pkg/provider"
	"go.uber.org/zap"
	"golang.org/x/oauth2"
)

var _ provider.Interface = (*Provider)(nil)

type Provider struct {
	logger        *zap.SugaredLogger
	client        *gh.Client
	webhookSecret string
}

func New(logger *zap.SugaredLogger) *Provider {
	return &Provider{logger: logger}
}

func (p *Provider) SetClient(ctx context.Context, evt *provider.Event, token, webhookSecret string) error {
	evt.Provider.Token = token
	evt.Provider.WebhookSecret = webhookSecret
	p.webhookSecret = webhookSecret

	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	p.client = gh.NewClient(oauth2.NewClient(ctx, ts))
	return nil
}

func (p *Provider) Validate(_ context.Context, evt *provider.Event) error {
	signature := evt.Request.Header.Get(gh.SHA256SignatureHeader)
	if signature == "" {
		signature = evt.Request.Header.Get(gh.SHA1SignatureHeader)
	}
	if signature == "" || signature == "sha1=" {
		return fmt.Errorf("no webhook signature found on request")
	}
	if p.webhookSecret == "" {
		return fmt.Errorf("no webhook secret configured for repository")
	}
	return gh.ValidateSignature(signature, evt.Request.Payload, []byte(p.webhookSecret))
}

func (p *Provider) CreateComment(_ context.Context, _ *provider.Event, _ string) error {
	// TODO: create or update PR comment via GitHub API
	return nil
}

func (p *Provider) GetAgentDir(ctx context.Context, evt *provider.Event, path string) (string, error) {
	if p.client == nil {
		return "", fmt.Errorf("github client not initialized")
	}

	// TODO: support provenance modes (source vs default_branch) like PaC's
	// pipelinerun_provenance setting. Currently hardcoded to default_branch
	// so only merged agent definitions run.
	revision := evt.DefaultBranch

	rootTree, _, err := p.client.Git.GetTree(ctx, evt.Organization, evt.Repository, revision, true)
	if err != nil {
		return "", fmt.Errorf("fetching root tree: %w", err)
	}

	var dirSHA string
	for _, entry := range rootTree.Entries {
		if entry.GetPath() == path {
			if entry.GetType() != "tree" {
				return "", fmt.Errorf("%s exists but is not a directory", path)
			}
			dirSHA = entry.GetSHA()
			break
		}
	}

	if dirSHA == "" {
		return "", nil
	}

	subtree, _, err := p.client.Git.GetTree(ctx, evt.Organization, evt.Repository, dirSHA, true)
	if err != nil {
		return "", fmt.Errorf("fetching agent dir tree: %w", err)
	}

	var docs []string
	for _, entry := range subtree.Entries {
		if entry.GetType() != "blob" {
			continue
		}
		if !strings.HasSuffix(entry.GetPath(), ".yaml") && !strings.HasSuffix(entry.GetPath(), ".yml") {
			continue
		}

		blob, _, err := p.client.Git.GetBlob(ctx, evt.Organization, evt.Repository, entry.GetSHA())
		if err != nil {
			p.logger.Warnf("failed to fetch blob %s: %v", entry.GetPath(), err)
			continue
		}

		content := blob.GetContent()
		if blob.GetEncoding() == "base64" {
			decoded, err := base64.StdEncoding.DecodeString(content)
			if err != nil {
				p.logger.Warnf("failed to decode blob %s: %v", entry.GetPath(), err)
				continue
			}
			content = string(decoded)
		}

		docs = append(docs, content)
	}

	return strings.Join(docs, "\n---\n"), nil
}

func (p *Provider) GetFiles(ctx context.Context, evt *provider.Event) ([]string, error) {
	if p.client == nil {
		return nil, fmt.Errorf("github client not initialized")
	}
	if evt.PullRequestNumber == 0 {
		return nil, nil
	}

	var allFiles []string
	opts := &gh.ListOptions{PerPage: 100}
	for {
		files, resp, err := p.client.PullRequests.ListFiles(ctx, evt.Organization, evt.Repository, evt.PullRequestNumber, opts)
		if err != nil {
			return nil, fmt.Errorf("listing PR files: %w", err)
		}
		for _, f := range files {
			allFiles = append(allFiles, f.GetFilename())
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return allFiles, nil
}

func (p *Provider) GetPullRequestDescription(ctx context.Context, evt *provider.Event) (string, error) {
	if p.client == nil {
		return "", fmt.Errorf("github client not initialized")
	}
	if evt.PullRequestNumber == 0 {
		return "", nil
	}
	pr, _, err := p.client.PullRequests.Get(ctx, evt.Organization, evt.Repository, evt.PullRequestNumber)
	if err != nil {
		return "", fmt.Errorf("fetching PR description: %w", err)
	}
	return pr.GetBody(), nil
}

func (p *Provider) GetPullRequestDiff(ctx context.Context, evt *provider.Event) (string, error) {
	if p.client == nil {
		return "", fmt.Errorf("github client not initialized")
	}
	if evt.PullRequestNumber == 0 {
		return "", nil
	}
	diff, _, err := p.client.PullRequests.GetRaw(ctx, evt.Organization, evt.Repository, evt.PullRequestNumber, gh.RawOptions{Type: gh.Diff})
	if err != nil {
		return "", fmt.Errorf("fetching PR diff: %w", err)
	}
	return diff, nil
}

func (p *Provider) GetPullRequestReviews(ctx context.Context, evt *provider.Event) (string, error) {
	if p.client == nil {
		return "", fmt.Errorf("github client not initialized")
	}
	if evt.PullRequestNumber == 0 {
		return "", nil
	}

	var allReviews []*gh.PullRequestReview
	opts := &gh.ListOptions{PerPage: 100}
	for {
		reviews, resp, err := p.client.PullRequests.ListReviews(ctx, evt.Organization, evt.Repository, evt.PullRequestNumber, opts)
		if err != nil {
			return "", fmt.Errorf("listing PR reviews: %w", err)
		}
		allReviews = append(allReviews, reviews...)
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	if len(allReviews) == 0 {
		return "", nil
	}

	var b strings.Builder
	for i, r := range allReviews {
		if i > 0 {
			b.WriteString("\n---\n")
		}
		user := "unknown"
		if r.User != nil {
			user = r.GetUser().GetLogin()
		}
		fmt.Fprintf(&b, "@%s (%s)", user, r.GetState())
		if body := r.GetBody(); body != "" {
			fmt.Fprintf(&b, ": %s", body)
		}
	}
	return b.String(), nil
}

func (p *Provider) GetPullRequestComments(ctx context.Context, evt *provider.Event) (string, error) {
	if p.client == nil {
		return "", fmt.Errorf("github client not initialized")
	}
	if evt.PullRequestNumber == 0 {
		return "", nil
	}

	var allComments []*gh.IssueComment
	opts := &gh.IssueListCommentsOptions{ListOptions: gh.ListOptions{PerPage: 100}}
	for {
		comments, resp, err := p.client.Issues.ListComments(ctx, evt.Organization, evt.Repository, evt.PullRequestNumber, opts)
		if err != nil {
			return "", fmt.Errorf("listing PR comments: %w", err)
		}
		allComments = append(allComments, comments...)
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return formatComments(allComments), nil
}

func (p *Provider) GetIssueTitle(ctx context.Context, evt *provider.Event) (string, error) {
	if p.client == nil {
		return "", fmt.Errorf("github client not initialized")
	}
	if evt.PullRequestNumber == 0 {
		return "", nil
	}
	issue, _, err := p.client.Issues.Get(ctx, evt.Organization, evt.Repository, evt.PullRequestNumber)
	if err != nil {
		return "", fmt.Errorf("fetching issue title: %w", err)
	}
	return issue.GetTitle(), nil
}

func (p *Provider) GetIssueBody(ctx context.Context, evt *provider.Event) (string, error) {
	if p.client == nil {
		return "", fmt.Errorf("github client not initialized")
	}
	if evt.PullRequestNumber == 0 {
		return "", nil
	}
	issue, _, err := p.client.Issues.Get(ctx, evt.Organization, evt.Repository, evt.PullRequestNumber)
	if err != nil {
		return "", fmt.Errorf("fetching issue body: %w", err)
	}
	return issue.GetBody(), nil
}

func (p *Provider) GetIssueComments(ctx context.Context, evt *provider.Event) (string, error) {
	if p.client == nil {
		return "", fmt.Errorf("github client not initialized")
	}
	if evt.PullRequestNumber == 0 {
		return "", nil
	}

	var allComments []*gh.IssueComment
	opts := &gh.IssueListCommentsOptions{ListOptions: gh.ListOptions{PerPage: 100}}
	for {
		comments, resp, err := p.client.Issues.ListComments(ctx, evt.Organization, evt.Repository, evt.PullRequestNumber, opts)
		if err != nil {
			return "", fmt.Errorf("listing issue comments: %w", err)
		}
		allComments = append(allComments, comments...)
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return formatComments(allComments), nil
}

func (p *Provider) GetIssueLabels(ctx context.Context, evt *provider.Event) (string, error) {
	if p.client == nil {
		return "", fmt.Errorf("github client not initialized")
	}
	if evt.PullRequestNumber == 0 {
		return "", nil
	}
	issue, _, err := p.client.Issues.Get(ctx, evt.Organization, evt.Repository, evt.PullRequestNumber)
	if err != nil {
		return "", fmt.Errorf("fetching issue labels: %w", err)
	}
	var labels []string
	for _, l := range issue.Labels {
		labels = append(labels, l.GetName())
	}
	return strings.Join(labels, ", "), nil
}

func formatComments(comments []*gh.IssueComment) string {
	if len(comments) == 0 {
		return ""
	}
	var b strings.Builder
	for i, c := range comments {
		if i > 0 {
			b.WriteString("\n---\n")
		}
		user := "unknown"
		if c.User != nil {
			user = c.GetUser().GetLogin()
		}
		fmt.Fprintf(&b, "@%s: %s", user, c.GetBody())
	}
	return b.String()
}

func (p *Provider) CheckPermission(ctx context.Context, evt *provider.Event) (bool, error) {
	if p.client == nil {
		return false, fmt.Errorf("github client not initialized")
	}

	permLevel, _, err := p.client.Repositories.GetPermissionLevel(ctx, evt.Organization, evt.Repository, evt.Sender)
	if err != nil {
		return false, fmt.Errorf("checking permission for %s: %w", evt.Sender, err)
	}

	switch permLevel.GetPermission() {
	case "admin", "write":
		return true, nil
	default:
		return false, nil
	}
}
