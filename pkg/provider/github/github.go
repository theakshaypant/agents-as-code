package github

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	gh "github.com/google/go-github/v84/github"
	"github.com/theakshaypant/yeet/pkg/provider"
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

func (p *Provider) GetFiles(_ context.Context, _ *provider.Event) ([]string, error) {
	// TODO: list changed files from push or pull request via GitHub API
	return nil, nil
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

// TODO: this currently only checks write permission. May need richer policy
// support in the future (e.g. ok-to-test approval, org membership, CODEOWNERS).
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
