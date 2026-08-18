package types

import "time"

const ConversationSyncSourceHermes = "hermes"

// ConversationSyncRequest appends one QA/conversation fragment to the
// external user's Markdown document for the fragment's local calendar day.
type ConversationSyncRequest struct {
	UserID         string     `json:"user_id" binding:"required"`
	QAContent      string     `json:"qa_content" binding:"required"`
	ConversationAt *time.Time `json:"conversation_at,omitempty"`
	EventID        string     `json:"event_id,omitempty"`
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
