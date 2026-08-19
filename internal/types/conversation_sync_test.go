package types

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestConversationSyncRequestAcceptsDateOnlyConversationAt(t *testing.T) {
	var request ConversationSyncRequest
	require.NoError(t, json.Unmarshal([]byte(`{
		"user_id":"hermes-user",
		"title":"每日对话",
		"text":"完整内容",
		"conversation_at":"2026-08-18"
	}`), &request))
	require.NotNil(t, request.ConversationAt)
	require.Equal(t, "2026-08-18", request.ConversationAt.In(time.Local).Format("2006-01-02"))
}

func TestConversationSyncRequestKeepsRFC3339Compatibility(t *testing.T) {
	var request ConversationSyncRequest
	require.NoError(t, json.Unmarshal([]byte(`{
		"user_id":"hermes-user",
		"title":"每日对话",
		"text":"完整内容",
		"conversation_at":"2026-08-18T14:35:00+08:00"
	}`), &request))
	require.NotNil(t, request.ConversationAt)
	require.Equal(t, "2026-08-18", request.ConversationAt.In(time.Local).Format("2006-01-02"))
}

func TestConversationSyncRequestRejectsInvalidConversationAt(t *testing.T) {
	var request ConversationSyncRequest
	err := json.Unmarshal([]byte(`{
		"user_id":"hermes-user",
		"title":"每日对话",
		"text":"完整内容",
		"conversation_at":"2026/08/18"
	}`), &request)
	require.ErrorContains(t, err, "YYYY-MM-DD")
}
