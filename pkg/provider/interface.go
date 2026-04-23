package provider

import (
	"context"
	"net/http"

	"go.uber.org/zap"
)

type Interface interface {
	Detect(req *http.Request, payload string, logger *zap.SugaredLogger) (bool, bool, *zap.SugaredLogger, string, error)
	ParsePayload(ctx context.Context, req *http.Request, payload string) (*Event, error)
	Validate(ctx context.Context, event *Event) error
	SetClient(ctx context.Context, event *Event, token, webhookSecret string) error
	CreateComment(ctx context.Context, event *Event, body string) error
	GetFiles(ctx context.Context, event *Event) ([]string, error)
	GetAgentDir(ctx context.Context, event *Event, path string) (string, error)
	CheckPermission(ctx context.Context, event *Event) (bool, error)
}
