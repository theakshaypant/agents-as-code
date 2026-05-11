package template

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"go.uber.org/zap"

	agentv1alpha1 "github.com/theakshaypant/agents-as-code/pkg/apis/agent/v1alpha1"
	"github.com/theakshaypant/agents-as-code/pkg/provider"
)

const RepoClonePath = "/workspace/repo"

type VariableResolver struct {
	event          *provider.Event
	repo           *agentv1alpha1.Repository
	provider       provider.Interface
	logger         *zap.SugaredLogger
	cache          map[string]string
	commentPattern string
}

func NewVariableResolver(evt *provider.Event, repo *agentv1alpha1.Repository, prov provider.Interface, logger *zap.SugaredLogger, commentPattern string) *VariableResolver {
	return &VariableResolver{
		event:          evt,
		repo:           repo,
		provider:       prov,
		logger:         logger,
		cache:          make(map[string]string),
		commentPattern: commentPattern,
	}
}

// Resolve builds a variable map containing only the requested variables.
// Variables from event metadata are always cheap; provider API calls are made
// only for variables that need them.
func (r *VariableResolver) Resolve(ctx context.Context, needed []string) map[string]string {
	vars := make(map[string]string, len(needed))

	metadata := r.metadataVars()

	for _, name := range needed {
		if v, ok := metadata[name]; ok {
			vars[name] = v
			continue
		}
		if v, ok := r.fetchProviderVar(ctx, name); ok {
			vars[name] = v
		}
	}

	return vars
}

func (r *VariableResolver) metadataVars() map[string]string {
	m := map[string]string{
		"repo_url":        r.event.URL,
		"repo_name":       r.event.Repository,
		"repo_owner":      r.event.Organization,
		"default_branch":  r.event.DefaultBranch,
		"event_type":      string(r.event.TriggerType),
		"sender":          r.event.Sender,
		"sha":             r.event.SHA,
		"branch":          r.event.BaseBranch,
		"comment_body":    r.event.CommentBody,
		"repo_clone_path": RepoClonePath,
	}

	if r.event.IssueNumber > 0 {
		m["issue_number"] = fmt.Sprintf("%d", r.event.IssueNumber)
	}

	if r.event.PullRequestNumber > 0 {
		m["pull_request_number"] = fmt.Sprintf("%d", r.event.PullRequestNumber)
		m["pull_request_title"] = r.event.PullRequestTitle
		m["pull_request_author"] = r.event.PullRequestAuthor
		m["pull_request_url"] = r.event.PullRequestURL
		m["pull_request_head_sha"] = r.event.SHA
		if r.event.IssueNumber == 0 {
			m["issue_number"] = fmt.Sprintf("%d", r.event.PullRequestNumber)
		}
	}

	if r.event.Label != "" {
		m["issue_labels"] = r.event.Label
	}

	if r.commentPattern != "" && r.event.CommentBody != "" {
		m["trigger_comment_args"] = extractCommentArgs(r.event.CommentBody, r.commentPattern)
	}

	return m
}

// extractCommentArgs strips the matched command pattern from the comment body,
// returning only the user's additional context.
// e.g. comment="/review focus on security", pattern="/review" → "focus on security"
func extractCommentArgs(comment, pattern string) string {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return comment
	}
	loc := re.FindStringIndex(strings.TrimSpace(comment))
	if loc == nil {
		return comment
	}
	return strings.TrimSpace(strings.TrimSpace(comment)[loc[1]:])
}

// fetchProviderVar fetches a variable that requires a provider API call.
// Results are cached so repeated references don't cause repeated API calls.
func (r *VariableResolver) fetchProviderVar(ctx context.Context, name string) (string, bool) {
	if v, ok := r.cache[name]; ok {
		return v, true
	}

	var val string
	var err error
	fetched := true

	switch name {
	case "pull_request_description":
		val, err = r.provider.GetPullRequestDescription(ctx, r.event)
	case "pull_request_diff":
		val, err = r.provider.GetPullRequestDiff(ctx, r.event)
	case "pull_request_reviews":
		val, err = r.provider.GetPullRequestReviews(ctx, r.event)
	case "pull_request_comments":
		val, err = r.provider.GetPullRequestComments(ctx, r.event)
	case "pull_request_files":
		var files []string
		files, err = r.provider.GetFiles(ctx, r.event)
		if err == nil {
			val = strings.Join(files, "\n")
		}
	case "issue_title":
		val, err = r.provider.GetIssueTitle(ctx, r.event)
	case "issue_body":
		val, err = r.provider.GetIssueBody(ctx, r.event)
	case "issue_comments":
		val, err = r.provider.GetIssueComments(ctx, r.event)
	case "issue_labels":
		val, err = r.provider.GetIssueLabels(ctx, r.event)
	default:
		fetched = false
	}

	if err != nil {
		r.logger.Warnf("failed to fetch template variable %s: %v", name, err)
		r.cache[name] = ""
		return "", true
	}

	if fetched {
		r.cache[name] = val
		return val, true
	}

	return "", false
}
