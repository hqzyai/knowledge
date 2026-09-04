package session

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	secutils "github.com/Tencent/WeKnora/internal/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func chatAPIKeyContext() context.Context {
	return types.WithTenantAPIKeyScope(context.Background(), types.TenantAPIKeyScope{
		Capabilities: types.StringArray{string(types.APIKeyCapabilityChat)},
	})
}

func TestValidateRequestChatModelRequiresChatAPIKey(t *testing.T) {
	model := &types.RequestChatModel{
		BaseURL:   "https://8.8.8.8/v1",
		APIKey:    "agentos-key",
		ModelName: "current-model",
	}

	err := validateRequestChatModel(context.Background(), model)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "API keys authorized for chat")
}

func TestValidateRequestChatModelAcceptsAndNormalizesAuthorizedEndpoint(t *testing.T) {
	secutils.SetSSRFWhitelistFromRaw("ai-gateway")
	t.Cleanup(secutils.ResetSSRFWhitelistForTest)
	model := &types.RequestChatModel{
		BaseURL:   " http://ai-gateway:4000/v1/ ",
		APIKey:    " agentos-key ",
		ModelName: " current-model ",
	}

	err := validateRequestChatModel(chatAPIKeyContext(), model)

	require.NoError(t, err)
	assert.Equal(t, "http://ai-gateway:4000/v1", model.BaseURL)
	assert.Equal(t, "agentos-key", model.APIKey)
	assert.Equal(t, "current-model", model.ModelName)
}

func TestValidateRequestChatModelBlocksPrivateEndpointOutsideSSRFWhitelist(t *testing.T) {
	secutils.SetSSRFWhitelistFromRaw("")
	t.Cleanup(func() { secutils.ResetSSRFWhitelistForTest() })
	model := &types.RequestChatModel{
		BaseURL:   "http://127.0.0.1:4000/v1",
		APIKey:    "agentos-key",
		ModelName: "current-model",
	}

	err := validateRequestChatModel(chatAPIKeyContext(), model)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "chat_model.base_url")
}
