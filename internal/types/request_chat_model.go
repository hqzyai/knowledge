package types

import (
	"context"
	"strings"
)

// RequestChatModelID identifies a request-scoped chat model. It is never a
// persisted models.id; ModelService resolves it from RequestChatModelContextKey.
const RequestChatModelID = "request-chat-model"

// RequestChatModel carries an OpenAI-compatible model endpoint for one QA
// request. The API key is kept in request context only and is never persisted
// to the model repository, session state, or chat execution metadata.
type RequestChatModel struct {
	BaseURL   string `json:"base_url"`
	APIKey    string `json:"api_key"`
	ModelName string `json:"model_name"`
}

// Normalize trims transport whitespace without changing the credential.
func (m RequestChatModel) Normalize() RequestChatModel {
	m.BaseURL = strings.TrimRight(strings.TrimSpace(m.BaseURL), "/")
	m.APIKey = strings.TrimSpace(m.APIKey)
	m.ModelName = strings.TrimSpace(m.ModelName)
	return m
}

// WithRequestChatModel installs a defensive copy for this request's model
// resolution. A nil model leaves the context unchanged.
func WithRequestChatModel(ctx context.Context, model *RequestChatModel) context.Context {
	if model == nil {
		return ctx
	}
	normalized := model.Normalize()
	return context.WithValue(ctx, RequestChatModelContextKey, normalized)
}

// RequestChatModelFromContext returns the request-scoped model configuration.
func RequestChatModelFromContext(ctx context.Context) (RequestChatModel, bool) {
	if ctx == nil {
		return RequestChatModel{}, false
	}
	model, ok := ctx.Value(RequestChatModelContextKey).(RequestChatModel)
	if !ok {
		return RequestChatModel{}, false
	}
	return model.Normalize(), true
}
