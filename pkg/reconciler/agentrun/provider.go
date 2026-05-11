package agentrun

import (
	"context"
	"fmt"
	"strings"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentv1alpha1 "github.com/theakshaypant/agents-as-code/pkg/apis/agent/v1alpha1"
	"github.com/theakshaypant/agents-as-code/pkg/provider"
	gh "github.com/theakshaypant/agents-as-code/pkg/provider/github"
)

// ProviderFactory creates a configured provider.Interface from a Repository CR.
type ProviderFactory func(ctx context.Context, repo *agentv1alpha1.Repository, c client.Client, logger *zap.SugaredLogger) (provider.Interface, error)

// DefaultProviderFactory detects the provider type from the Repository URL
// and returns a configured provider with the git token set.
func DefaultProviderFactory(ctx context.Context, repo *agentv1alpha1.Repository, c client.Client, logger *zap.SugaredLogger) (provider.Interface, error) {
	token, err := getProviderToken(ctx, c, repo)
	if err != nil {
		return nil, err
	}

	var prov provider.Interface
	repoURL := repo.Spec.URL

	switch {
	case strings.Contains(repoURL, "github"):
		prov = gh.New(logger)
	default:
		return nil, fmt.Errorf("unsupported git provider for URL %q", repoURL)
	}

	evt := &provider.Event{Provider: &provider.ProviderInfo{}}
	if err := prov.SetClient(ctx, evt, token, ""); err != nil {
		return nil, fmt.Errorf("setting up provider client: %w", err)
	}

	return prov, nil
}

// CreateProviderEvent reconstructs a provider.Event from an AgentRun spec
// and Repository, providing enough context for result hook execution.
func CreateProviderEvent(run *agentv1alpha1.AgentRun, repo *agentv1alpha1.Repository) *provider.Event {
	evt := provider.NewEvent()

	parts := splitRepoURL(repo.Spec.URL)
	evt.Organization = parts[0]
	evt.Repository = parts[1]
	evt.URL = repo.Spec.URL

	evt.TriggerType = provider.TriggerType(run.Spec.Event.Type)
	evt.EventType = run.Spec.Event.Action
	evt.SHA = run.Spec.Event.SHA
	evt.BaseBranch = run.Spec.Event.Branch
	evt.Sender = run.Spec.Event.Sender
	evt.SHAURL = run.Spec.Event.URL
	evt.IssueNumber = run.Spec.Event.IssueNumber

	if pr := run.Spec.Event.PullRequest; pr != nil {
		evt.PullRequestNumber = pr.Number
		evt.PullRequestTitle = pr.Title
		evt.HeadBranch = pr.HeadBranch
		evt.BaseBranch = pr.BaseBranch
	}

	if c := run.Spec.Event.Comment; c != nil {
		evt.CommentBody = c.Body
	}

	if len(run.Spec.Event.Labels) > 0 {
		evt.Label = run.Spec.Event.Labels[0]
	}

	return evt
}

func getProviderToken(ctx context.Context, c client.Client, repo *agentv1alpha1.Repository) (string, error) {
	if repo.Spec.GitProvider == nil {
		return "", fmt.Errorf("no git_provider configured on Repository %s/%s", repo.Namespace, repo.Name)
	}
	ref := repo.Spec.GitProvider.Secret
	var secret corev1.Secret
	if err := c.Get(ctx, client.ObjectKey{Namespace: repo.Namespace, Name: ref.Name}, &secret); err != nil {
		return "", fmt.Errorf("fetching secret %s/%s: %w", repo.Namespace, ref.Name, err)
	}
	key := ref.Key
	if key == "" {
		key = "token"
	}
	val, ok := secret.Data[key]
	if !ok {
		return "", fmt.Errorf("key %q not found in secret %s/%s", key, repo.Namespace, ref.Name)
	}
	return string(val), nil
}

// splitRepoURL extracts org and repo name from a GitHub URL.
// e.g. "https://github.com/org/repo" → ["org", "repo"]
func splitRepoURL(url string) [2]string {
	url = strings.TrimSuffix(url, "/")
	url = strings.TrimSuffix(url, ".git")
	parts := strings.Split(url, "/")
	if len(parts) >= 2 {
		return [2]string{parts[len(parts)-2], parts[len(parts)-1]}
	}
	return [2]string{"", ""}
}
