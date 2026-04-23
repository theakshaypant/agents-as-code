package github

import (
	"context"
	"fmt"

	gh "github.com/google/go-github/v84/github"
	"github.com/theakshaypant/yeet/pkg/provider"
	"go.uber.org/zap"
)

var _ provider.Interface = (*Provider)(nil)

type Provider struct {
	logger        *zap.SugaredLogger
	webhookSecret string
}

func New(logger *zap.SugaredLogger) *Provider {
	return &Provider{logger: logger}
}

func (p *Provider) SetClient(_ context.Context, evt *provider.Event, token, webhookSecret string) error {
	evt.Provider.Token = token
	evt.Provider.WebhookSecret = webhookSecret
	p.webhookSecret = webhookSecret
	// TODO: initialize go-github client with oauth2 token for API calls
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
