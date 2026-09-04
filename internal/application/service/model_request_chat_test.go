package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/models/provider"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestScopedChatModelBypassesRepository(t *testing.T) {
	modelConfig := &types.RequestChatModel{
		BaseURL:   "http://ai-gateway:4000/v1/",
		APIKey:    "agentos-key",
		ModelName: "current-agentos-model",
	}
	ctx := types.WithRequestChatModel(context.Background(), modelConfig)
	svc := &modelService{}

	model, err := svc.GetModelByID(ctx, types.RequestChatModelID)
	require.NoError(t, err)
	require.NotNil(t, model)
	assert.Equal(t, "current-agentos-model", model.Name)
	assert.Equal(t, "http://ai-gateway:4000/v1", model.Parameters.BaseURL)
	assert.Equal(t, "agentos-key", model.Parameters.APIKey)
	assert.Equal(t, string(provider.ProviderLiteLLM), model.Parameters.Provider)
}

func TestRequestScopedChatModelIDFailsWithoutRequestContext(t *testing.T) {
	svc := &modelService{}

	_, err := svc.GetModelByID(context.Background(), types.RequestChatModelID)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing from context")
}
