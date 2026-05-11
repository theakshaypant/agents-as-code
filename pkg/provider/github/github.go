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

func (p *Provider) CreateComment(ctx context.Context, evt *provider.Event, body string) (string, error) {
	if p.client == nil {
		return "", fmt.Errorf("github client not initialized")
	}
	num := issueNumber(evt)
	if num == 0 {
		return "", fmt.Errorf("no issue or pull request number in event")
	}

	comment, _, err := p.client.Issues.CreateComment(
		ctx, evt.Organization, evt.Repository, num,
		&gh.IssueComment{Body: gh.Ptr(body)},
	)
	if err != nil {
		return "", fmt.Errorf("creating comment: %w", err)
	}
	return comment.GetHTMLURL(), nil
}

func (p *Provider) CreateReview(ctx context.Context, evt *provider.Event, reviewEvent, body string, comments []provider.ReviewComment) (string, error) {
	if p.client == nil {
		return "", fmt.Errorf("github client not initialized")
	}
	if evt.PullRequestNumber == 0 {
		return "", fmt.Errorf("no pull request number in event")
	}

	req := &gh.PullRequestReviewRequest{
		Event: gh.Ptr(reviewEvent),
	}
	if body != "" {
		req.Body = gh.Ptr(body)
	}
	if len(comments) > 0 {
		drafts := make([]*gh.DraftReviewComment, 0, len(comments))
		for _, c := range comments {
			drafts = append(drafts, &gh.DraftReviewComment{
				Path: gh.Ptr(c.Path),
				Line: gh.Ptr(c.Line),
				Side: gh.Ptr("RIGHT"),
				Body: gh.Ptr(c.Body),
			})
		}
		req.Comments = drafts
	}

	review, _, err := p.client.PullRequests.CreateReview(
		ctx, evt.Organization, evt.Repository, evt.PullRequestNumber, req,
	)
	if err != nil {
		return "", fmt.Errorf("creating review: %w", err)
	}
	return review.GetHTMLURL(), nil
}

func (p *Provider) AddLabels(ctx context.Context, evt *provider.Event, labels []string) error {
	if p.client == nil {
		return fmt.Errorf("github client not initialized")
	}
	num := issueNumber(evt)
	if num == 0 {
		return fmt.Errorf("no issue or pull request number in event")
	}

	_, _, err := p.client.Issues.AddLabelsToIssue(
		ctx, evt.Organization, evt.Repository, num, labels,
	)
	if err != nil {
		return fmt.Errorf("adding labels: %w", err)
	}
	return nil
}

func (p *Provider) RemoveLabels(ctx context.Context, evt *provider.Event, labels []string) error {
	if p.client == nil {
		return fmt.Errorf("github client not initialized")
	}
	num := issueNumber(evt)
	if num == 0 {
		return fmt.Errorf("no issue or pull request number in event")
	}

	for _, label := range labels {
		_, err := p.client.Issues.RemoveLabelForIssue(
			ctx, evt.Organization, evt.Repository, num, label,
		)
		if err != nil {
			p.logger.Warnf("failed to remove label %q: %v", label, err)
		}
	}
	return nil
}

func (p *Provider) CreatePullRequest(ctx context.Context, evt *provider.Event, title, body, head, base string) (string, error) {
	if p.client == nil {
		return "", fmt.Errorf("github client not initialized")
	}

	newPR := &gh.NewPullRequest{
		Title: gh.Ptr(title),
		Head:  gh.Ptr(head),
		Base:  gh.Ptr(base),
	}
	if body != "" {
		newPR.Body = gh.Ptr(body)
	}

	pr, _, err := p.client.PullRequests.Create(
		ctx, evt.Organization, evt.Repository, newPR,
	)
	if err != nil {
		return "", fmt.Errorf("creating pull request: %w", err)
	}
	return pr.GetHTMLURL(), nil
}

func (p *Provider) SetCommitStatus(ctx context.Context, evt *provider.Event, statusContext, state, description, targetURL string) error {
	if p.client == nil {
		return fmt.Errorf("github client not initialized")
	}
	if evt.SHA == "" {
		return fmt.Errorf("no commit SHA in event")
	}

	repoStatus := gh.RepoStatus{
		Context: gh.Ptr(statusContext),
		State:   gh.Ptr(state),
	}
	if description != "" {
		repoStatus.Description = gh.Ptr(description)
	}
	if targetURL != "" {
		repoStatus.TargetURL = gh.Ptr(targetURL)
	}

	_, _, err := p.client.Repositories.CreateStatus(
		ctx, evt.Organization, evt.Repository, evt.SHA, repoStatus,
	)
	if err != nil {
		return fmt.Errorf("setting commit status: %w", err)
	}
	return nil
}

func (p *Provider) CreateCommit(ctx context.Context, evt *provider.Event, message string, files map[string]string) (string, error) {
	if p.client == nil {
		return "", fmt.Errorf("github client not initialized")
	}

	branch := evt.HeadBranch
	if branch == "" {
		branch, _ = p.createIssueBranch(ctx, evt)
		if branch == "" {
			return "", fmt.Errorf("no head branch in event and could not create one")
		}
		evt.HeadBranch = branch
	}

	ref, _, err := p.client.Git.GetRef(ctx, evt.Organization, evt.Repository, "refs/heads/"+branch)
	if err != nil {
		return "", fmt.Errorf("getting ref for branch %q: %w", branch, err)
	}
	baseCommitSHA := ref.GetObject().GetSHA()

	baseCommit, _, err := p.client.Git.GetCommit(ctx, evt.Organization, evt.Repository, baseCommitSHA)
	if err != nil {
		return "", fmt.Errorf("getting base commit: %w", err)
	}
	baseTreeSHA := baseCommit.GetTree().GetSHA()

	var entries []*gh.TreeEntry
	for path, content := range files {
		blob, _, err := p.client.Git.CreateBlob(ctx, evt.Organization, evt.Repository, gh.Blob{
			Content:  gh.Ptr(content),
			Encoding: gh.Ptr("utf-8"),
		})
		if err != nil {
			return "", fmt.Errorf("creating blob for %q: %w", path, err)
		}
		entries = append(entries, &gh.TreeEntry{
			Path: gh.Ptr(path),
			Mode: gh.Ptr("100644"),
			Type: gh.Ptr("blob"),
			SHA:  blob.SHA,
		})
	}

	tree, _, err := p.client.Git.CreateTree(ctx, evt.Organization, evt.Repository, baseTreeSHA, entries)
	if err != nil {
		return "", fmt.Errorf("creating tree: %w", err)
	}

	commit, _, err := p.client.Git.CreateCommit(ctx, evt.Organization, evt.Repository, gh.Commit{
		Message: gh.Ptr(message),
		Tree:    tree,
		Parents: []*gh.Commit{{SHA: gh.Ptr(baseCommitSHA)}},
	}, nil)
	if err != nil {
		return "", fmt.Errorf("creating commit: %w", err)
	}

	_, _, err = p.client.Git.UpdateRef(ctx, evt.Organization, evt.Repository,
		"refs/heads/"+evt.HeadBranch, gh.UpdateRef{SHA: commit.GetSHA(), Force: gh.Ptr(false)})
	if err != nil {
		return "", fmt.Errorf("updating ref for branch %q: %w", evt.HeadBranch, err)
	}

	return commit.GetSHA(), nil
}

func (p *Provider) createIssueBranch(ctx context.Context, evt *provider.Event) (string, error) {
	repo, _, err := p.client.Repositories.Get(ctx, evt.Organization, evt.Repository)
	if err != nil {
		return "", fmt.Errorf("getting repo: %w", err)
	}
	defaultBranch := repo.GetDefaultBranch()

	baseRef, _, err := p.client.Git.GetRef(ctx, evt.Organization, evt.Repository, "refs/heads/"+defaultBranch)
	if err != nil {
		return "", fmt.Errorf("getting default branch ref: %w", err)
	}

	branch := fmt.Sprintf("aac/issue-%d", evt.IssueNumber)
	_, _, err = p.client.Git.CreateRef(ctx, evt.Organization, evt.Repository, gh.CreateRef{
		Ref: "refs/heads/" + branch,
		SHA: baseRef.GetObject().GetSHA(),
	})
	if err != nil {
		// Branch may already exist from a previous run — try to use it
		if _, _, getErr := p.client.Git.GetRef(ctx, evt.Organization, evt.Repository, "refs/heads/"+branch); getErr != nil {
			return "", fmt.Errorf("creating branch %q: %w", branch, err)
		}
	}

	evt.DefaultBranch = defaultBranch
	return branch, nil
}

func (p *Provider) GetFile(ctx context.Context, evt *provider.Event, path string) (string, error) {
	if p.client == nil {
		return "", fmt.Errorf("github client not initialized")
	}

	revision := evt.DefaultBranch

	rootTree, _, err := p.client.Git.GetTree(ctx, evt.Organization, evt.Repository, revision, true)
	if err != nil {
		return "", fmt.Errorf("fetching root tree: %w", err)
	}

	var blobSHA string
	for _, entry := range rootTree.Entries {
		if entry.GetPath() == path && entry.GetType() == "blob" {
			blobSHA = entry.GetSHA()
			break
		}
	}
	if blobSHA == "" {
		return "", fmt.Errorf("file %q not found in %s", path, revision)
	}

	blob, _, err := p.client.Git.GetBlob(ctx, evt.Organization, evt.Repository, blobSHA)
	if err != nil {
		return "", fmt.Errorf("fetching blob for %q: %w", path, err)
	}

	content := blob.GetContent()
	if blob.GetEncoding() == "base64" {
		decoded, err := base64.StdEncoding.DecodeString(content)
		if err != nil {
			return "", fmt.Errorf("decoding blob for %q: %w", path, err)
		}
		content = string(decoded)
	}

	return content, nil
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

func issueNumber(evt *provider.Event) int {
	if evt.IssueNumber > 0 {
		return evt.IssueNumber
	}
	return evt.PullRequestNumber
}

func (p *Provider) GetIssueTitle(ctx context.Context, evt *provider.Event) (string, error) {
	if p.client == nil {
		return "", fmt.Errorf("github client not initialized")
	}
	num := issueNumber(evt)
	if num == 0 {
		return "", nil
	}
	issue, _, err := p.client.Issues.Get(ctx, evt.Organization, evt.Repository, num)
	if err != nil {
		return "", fmt.Errorf("fetching issue title: %w", err)
	}
	return issue.GetTitle(), nil
}

func (p *Provider) GetIssueBody(ctx context.Context, evt *provider.Event) (string, error) {
	if p.client == nil {
		return "", fmt.Errorf("github client not initialized")
	}
	num := issueNumber(evt)
	if num == 0 {
		return "", nil
	}
	issue, _, err := p.client.Issues.Get(ctx, evt.Organization, evt.Repository, num)
	if err != nil {
		return "", fmt.Errorf("fetching issue body: %w", err)
	}
	return issue.GetBody(), nil
}

func (p *Provider) GetIssueComments(ctx context.Context, evt *provider.Event) (string, error) {
	if p.client == nil {
		return "", fmt.Errorf("github client not initialized")
	}
	num := issueNumber(evt)
	if num == 0 {
		return "", nil
	}

	var allComments []*gh.IssueComment
	opts := &gh.IssueListCommentsOptions{ListOptions: gh.ListOptions{PerPage: 100}}
	for {
		comments, resp, err := p.client.Issues.ListComments(ctx, evt.Organization, evt.Repository, num, opts)
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
	num := issueNumber(evt)
	if num == 0 {
		return "", nil
	}
	issue, _, err := p.client.Issues.Get(ctx, evt.Organization, evt.Repository, num)
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
