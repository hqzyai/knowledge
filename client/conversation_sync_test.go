package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSyncConversation(t *testing.T) {
	at := time.Date(2026, 8, 17, 14, 35, 0, 0, time.FixedZone("CST", 8*60*60))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/conversation-sync" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("X-API-Key"); got != "sk-full" {
			t.Fatalf("X-API-Key = %q", got)
		}
		var request ConversationSyncRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.UserID != "hermes-user" || request.EventID != "evt-1" || request.QAContent != "Q/A" {
			t.Fatalf("request body = %+v", request)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"data": map[string]interface{}{
				"knowledge_base_id":      "kb-1",
				"knowledge_id":           "knowledge-1",
				"document_date":          "2026-08-17",
				"content_version":        1,
				"parse_status":           "pending",
				"knowledge_base_created": true,
				"document_created":       true,
				"idempotent_replay":      false,
			},
		})
	}))
	defer server.Close()

	api := NewClient(server.URL, WithAPIKey("sk-full"))
	result, err := api.SyncConversation(context.Background(), &ConversationSyncRequest{
		UserID: "hermes-user", QAContent: "Q/A", ConversationAt: &at, EventID: "evt-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.KnowledgeBaseID != "kb-1" || result.KnowledgeID != "knowledge-1" || !result.KnowledgeBaseCreated {
		t.Fatalf("result = %+v", result)
	}
}
