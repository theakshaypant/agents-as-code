package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentv1alpha1 "github.com/theakshaypant/yeet/pkg/apis/agent/v1alpha1"
	"github.com/theakshaypant/yeet/pkg/provider"
)

const (
	DefaultGlobalSecretName = "yeet-github-app"
	GlobalWebhookSecretKey  = "webhook.secret"
)

type Adapter struct {
	client           client.Client
	providers        []provider.Interface
	logger           *zap.SugaredLogger
	globalSecretName string
	globalSecretNS   string
}

func New(c client.Client, logger *zap.SugaredLogger, globalSecretNS string, providers ...provider.Interface) *Adapter {
	return &Adapter{
		client:           c,
		providers:        providers,
		logger:           logger,
		globalSecretName: DefaultGlobalSecretName,
		globalSecretNS:   globalSecretNS,
	}
}

func (a *Adapter) HandleEvent(ctx context.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeResponse(w, http.StatusOK, "ok")
			return
		}

		payload, err := io.ReadAll(r.Body)
		if err != nil {
			a.logger.Errorf("failed to read body: %v", err)
			writeResponse(w, http.StatusInternalServerError, "body read error")
			return
		}

		prov, logger, err := a.detectProvider(r, string(payload))
		if err != nil || prov == nil {
			msg := "no supported git provider detected"
			if err != nil {
				msg = err.Error()
			}
			a.logger.Debugf("skipping event: %s", msg)
			writeResponse(w, http.StatusOK, msg)
			return
		}

		evt, err := prov.ParsePayload(ctx, r, string(payload))
		if err != nil {
			logger.Errorf("failed to parse payload: %v", err)
			writeResponse(w, http.StatusBadRequest, "invalid payload")
			return
		}

		repo, err := a.matchRepository(ctx, evt.URL)
		if err != nil || repo == nil {
			logger.Infof("no matching repository CR for %s", evt.URL)
			writeResponse(w, http.StatusOK, "repository not configured")
			return
		}

		secret, err := a.getWebhookSecret(ctx, evt, repo)
		if err != nil {
			logger.Errorf("failed to get webhook secret: %v", err)
			writeResponse(w, http.StatusInternalServerError, "credential error")
			return
		}

		if err := prov.SetClient(ctx, evt, "", secret); err != nil {
			logger.Errorf("failed to set up provider client: %v", err)
			writeResponse(w, http.StatusInternalServerError, "client setup error")
			return
		}

		if err := prov.Validate(ctx, evt); err != nil {
			logger.Errorf("webhook signature validation failed: %v", err)
			writeResponse(w, http.StatusUnauthorized, "invalid signature")
			return
		}

		go func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Errorf("panic processing event: %v", r)
				}
			}()
			processEvent(evt, repo, logger)
		}()

		writeResponse(w, http.StatusAccepted, "accepted")
	}
}

func (a *Adapter) detectProvider(req *http.Request, payload string) (provider.Interface, *zap.SugaredLogger, error) {
	for _, p := range a.providers {
		isProvider, shouldProcess, logger, reason, err := p.Detect(req, payload, a.logger)
		if !isProvider {
			continue
		}
		if err != nil {
			return nil, logger, fmt.Errorf("provider detection error: %w", err)
		}
		if !shouldProcess {
			return nil, logger, fmt.Errorf("skipping event: %s", reason)
		}
		return p, logger, nil
	}
	return nil, a.logger, fmt.Errorf("no supported git provider detected")
}

func (a *Adapter) matchRepository(ctx context.Context, eventURL string) (*agentv1alpha1.Repository, error) {
	var repos agentv1alpha1.RepositoryList
	if err := a.client.List(ctx, &repos); err != nil {
		return nil, fmt.Errorf("listing repositories: %w", err)
	}
	for i := range repos.Items {
		repoURL := strings.TrimSuffix(repos.Items[i].Spec.URL, "/")
		if repoURL == strings.TrimSuffix(eventURL, "/") {
			return &repos.Items[i], nil
		}
	}
	return nil, nil
}

func (a *Adapter) getWebhookSecret(ctx context.Context, evt *provider.Event, repo *agentv1alpha1.Repository) (string, error) {
	if evt.InstallationID > 0 {
		return a.getGlobalWebhookSecret(ctx)
	}
	return a.getRepoWebhookSecret(ctx, repo)
}

func (a *Adapter) getGlobalWebhookSecret(ctx context.Context) (string, error) {
	key := client.ObjectKey{
		Namespace: a.globalSecretNS,
		Name:      a.globalSecretName,
	}
	var secret corev1.Secret
	if err := a.client.Get(ctx, key, &secret); err != nil {
		return "", fmt.Errorf("fetching global secret %s/%s: %w", key.Namespace, key.Name, err)
	}
	val, ok := secret.Data[GlobalWebhookSecretKey]
	if !ok {
		return "", fmt.Errorf("key %q not found in global secret %s/%s", GlobalWebhookSecretKey, key.Namespace, key.Name)
	}
	return string(val), nil
}

func (a *Adapter) getRepoWebhookSecret(ctx context.Context, repo *agentv1alpha1.Repository) (string, error) {
	if repo.Spec.GitProvider == nil || repo.Spec.GitProvider.WebhookSecret == nil {
		return "", fmt.Errorf("no webhook secret configured on Repository %s/%s", repo.Namespace, repo.Name)
	}
	ref := repo.Spec.GitProvider.WebhookSecret
	key := client.ObjectKey{
		Namespace: repo.Namespace,
		Name:      ref.Name,
	}
	var secret corev1.Secret
	if err := a.client.Get(ctx, key, &secret); err != nil {
		return "", fmt.Errorf("fetching secret %s/%s: %w", key.Namespace, key.Name, err)
	}
	secretKey := ref.Key
	if secretKey == "" {
		secretKey = "webhook.secret"
	}
	val, ok := secret.Data[secretKey]
	if !ok {
		return "", fmt.Errorf("key %q not found in secret %s/%s", secretKey, key.Namespace, key.Name)
	}
	return string(val), nil
}

func processEvent(evt *provider.Event, _ *agentv1alpha1.Repository, logger *zap.SugaredLogger) {
	logger = logger.With(
		"trigger", evt.TriggerType,
		"org", evt.Organization,
		"repo", evt.Repository,
		"sha", evt.SHA,
		"sender", evt.Sender,
	)

	switch evt.TriggerType {
	case provider.TriggerPush:
		logger.Infow("received push event",
			"branch", evt.BaseBranch,
		)
		// TODO: check if branch matches repo.Spec.KnowledgeGraph.Branches
		// TODO: run incremental knowledge graph update via graphify --update
		// TODO: update Repository status with new commit SHA and graph counts

	case provider.TriggerPullRequest:
		logger.Infow("received pull request event",
			"pr", evt.PullRequestNumber,
			"title", evt.PullRequestTitle,
			"base", evt.BaseBranch,
			"head", evt.HeadBranch,
		)
		// TODO: match event to agent definitions in .tekton/agents/
		// TODO: filter knowledge graph context for matched agents
		// TODO: create AgentRun CR

	case provider.TriggerPullRequestReview:
		logger.Infow("received pull request review event",
			"pr", evt.PullRequestNumber,
			"base", evt.BaseBranch,
			"head", evt.HeadBranch,
		)
		// TODO: extract review state (approved/changes_requested/commented)
		// TODO: enable reviewer agent to hold conversations via review threads
		// TODO: match to agent definitions with on.event=pull_request_review

	case provider.TriggerIssueComment:
		logger.Infow("received issue comment event",
			"pr", evt.PullRequestNumber,
		)
		// TODO: parse comment body for agent commands (/triage, /implement, /review)
		// TODO: match to agent definitions with on.event=issue_comment and on.match
		// TODO: create AgentRun CR

	case provider.TriggerIssueLabeled:
		logger.Infow("received issue labeled event")
		// TODO: extract label name from event
		// TODO: match to agent definitions with on.event=issues, on.action=labeled

	case provider.TriggerPRLabeled:
		logger.Infow("received pull request labeled event",
			"pr", evt.PullRequestNumber,
		)
		// TODO: extract label name from event
		// TODO: match to agent definitions with on.event=pull_request, on.action=labeled

	default:
		logger.Infow("received unhandled event type")
	}
}

type response struct {
	Status  int    `json:"status"`
	Message string `json:"message"`
}

func writeResponse(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(response{
		Status:  statusCode,
		Message: message,
	})
}
