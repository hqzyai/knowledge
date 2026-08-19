package types

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const ConversationSyncSourceHermes = "hermes"

// ConversationSyncRequest replaces the external user's Markdown conversation
// snapshot for the request's local calendar day.
type ConversationSyncRequest struct {
	UserID         string     `json:"user_id" binding:"required"`
	Title          string     `json:"title" binding:"required"`
	Text           string     `json:"text" binding:"required"`
	ConversationAt *time.Time `json:"conversation_at,omitempty"`
	EventID        string     `json:"event_id,omitempty"`
}

// UnmarshalJSON accepts a date-only conversation_at while preserving RFC3339
// compatibility for callers that already send a complete timestamp.
func (r *ConversationSyncRequest) UnmarshalJSON(data []byte) error {
	var payload struct {
		UserID         string          `json:"user_id"`
		Title          string          `json:"title"`
		Text           string          `json:"text"`
		ConversationAt json.RawMessage `json:"conversation_at"`
		EventID        string          `json:"event_id"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}

	var conversationAt *time.Time
	if len(payload.ConversationAt) > 0 && string(payload.ConversationAt) != "null" {
		var value string
		if err := json.Unmarshal(payload.ConversationAt, &value); err != nil {
			return fmt.Errorf("conversation_at 必须是字符串")
		}
		value = strings.TrimSpace(value)
		if value != "" {
			parsed, err := time.ParseInLocation("2006-01-02", value, time.Local)
			if err != nil {
				parsed, err = time.Parse(time.RFC3339, value)
			}
			if err != nil {
				return fmt.Errorf("conversation_at 仅支持 YYYY-MM-DD 或 RFC3339 格式")
			}
			conversationAt = &parsed
		}
	}

	*r = ConversationSyncRequest{
		UserID:         payload.UserID,
		Title:          payload.Title,
		Text:           payload.Text,
		ConversationAt: conversationAt,
		EventID:        payload.EventID,
	}
	return nil
}

// ConversationSyncResult identifies the durable resources touched by a sync
// call. Indexing is asynchronous, so ParseStatus normally returns "pending".
type ConversationSyncResult struct {
	KnowledgeBaseID      string `json:"knowledge_base_id"`
	KnowledgeID          string `json:"knowledge_id"`
	DocumentDate         string `json:"document_date"`
	ContentVersion       int    `json:"content_version"`
	ParseStatus          string `json:"parse_status"`
	KnowledgeBaseCreated bool   `json:"knowledge_base_created"`
	DocumentCreated      bool   `json:"document_created"`
	IdempotentReplay     bool   `json:"idempotent_replay"`
}

// ConversationSyncResponse is the accepted response envelope returned by the
// Hermes conversation ingestion endpoint.
type ConversationSyncResponse struct {
	Success bool                   `json:"success"`
	Data    ConversationSyncResult `json:"data"`
}
