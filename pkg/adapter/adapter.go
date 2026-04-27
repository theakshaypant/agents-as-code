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

	agentv1alpha1 "github.com/theakshaypant/agents-as-code/pkg/apis/agent/v1alpha1"
	"github.com/theakshaypant/agents-as-code/pkg/matcher"
	"github.com/theakshaypant/agents-as-code/pkg/provider"
)

const (
	DefaultGlobalSecretName = "agents-as-code-github-app"
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

		token, err := a.getProviderToken(ctx, repo)
		if err != nil {
			logger.Errorf("failed to get provider token: %v", err)
			writeResponse(w, http.StatusInternalServerError, "credential error")
			return
		}

		webhookSecret, err := a.getWebhookSecret(ctx, evt, repo)
		if err != nil {
			logger.Errorf("failed to get webhook secret: %v", err)
			writeResponse(w, http.StatusInternalServerError, "credential error")
			return
		}

		if err := prov.SetClient(ctx, evt, token, webhookSecret); err != nil {
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
			a.processEvent(ctx, evt, prov, repo, logger)
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

func (a *Adapter) getProviderToken(ctx context.Context, repo *agentv1alpha1.Repository) (string, error) {
	if repo.Spec.GitProvider == nil {
		return "", fmt.Errorf("no git_provider configured on Repository %s/%s", repo.Namespace, repo.Name)
	}
	ref := repo.Spec.GitProvider.Secret
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
		secretKey = "token"
	}
	val, ok := secret.Data[secretKey]
	if !ok {
		return "", fmt.Errorf("key %q not found in secret %s/%s", secretKey, key.Namespace, key.Name)
	}
	return string(val), nil
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

func (a *Adapter) processEvent(ctx context.Context, evt *provider.Event, prov provider.Interface, repo *agentv1alpha1.Repository, logger *zap.SugaredLogger) {
	logger = logger.With(
		"trigger", evt.TriggerType,
		"org", evt.Organization,
		"repo", evt.Repository,
		"sha", evt.SHA,
		"sender", evt.Sender,
	)

	logger.Infow("processing event")

	if evt.TriggerType == provider.TriggerPush {
		// TODO: check if branch matches repo.Spec.KnowledgeGraph.Branches
		// TODO: run incremental knowledge graph update via graphify --update
		// TODO: update Repository status with new commit SHA and graph counts
	}

	if evt.TriggerType == provider.TriggerIssueComment {
		allowed, err := prov.CheckPermission(ctx, evt)
		if err != nil {
			logger.Errorf("failed to check permission for %s: %v", evt.Sender, err)
			return
		}
		if !allowed {
			logger.Infow("sender does not have write permission, skipping", "sender", evt.Sender)
			return
		}
	}

	matches := a.matchAgents(ctx, evt, prov, logger)
	if len(matches) == 0 {
		logger.Infow("no agents matched event")
		return
	}

	for _, m := range matches {
		logger.Infow("agent matched",
			"agent", m.Agent.Name,
			"purpose", m.Agent.Spec.Purpose,
			"trigger_event", m.Trigger.Event,
		)
		// TODO: create AgentRun CR for each matched agent
	}
}

func (a *Adapter) matchAgents(ctx context.Context, evt *provider.Event, prov provider.Interface, logger *zap.SugaredLogger) []matcher.AgentMatch {
	rawYAML, err := prov.GetAgentDir(ctx, evt, matcher.AgentDirPath())
	if err != nil {
		logger.Errorf("failed to fetch agent definitions: %v", err)
		return nil
	}
	if rawYAML == "" {
		logger.Debugf("no .tekton/agents/ directory found")
		return nil
	}

	agents, err := matcher.ParseAgentDefinitions(rawYAML)
	if err != nil {
		logger.Errorf("failed to parse agent definitions: %v", err)
		return nil
	}

	logger.Infow("discovered agent definitions", "count", len(agents))
	return matcher.MatchAgentsToEvent(agents, evt, logger)
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
