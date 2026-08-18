package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateExternalUser(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/external-users" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("X-API-Key") != "sk-admin" {
			t.Fatalf("X-API-Key = %q", r.Header.Get("X-API-Key"))
		}
		var request ExternalUserCreateRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.UserID != "hermes-42" || request.Email != "h42@example.com" {
			t.Fatalf("request = %+v", request)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"data": map[string]interface{}{
				"external_user_id": "hermes-42", "user_id": "internal-42",
				"username": "hermes_42", "email": "h42@example.com",
				"space_id": 10001, "role": "contributor", "api_key_id": 91,
				"api_key": "sk-user", "full_access": false,
				"capabilities":       []string{"chat", "retrieve", "read_agents"},
				"knowledge_base_ids": []string{"kb-user"},
				"user_created":       true, "api_key_created": true,
			},
		})
	}))
	defer server.Close()

	api := NewClient(server.URL, WithAPIKey("sk-admin"))
	result, err := api.CreateExternalUser(context.Background(), &ExternalUserCreateRequest{
		UserID: "hermes-42", Username: "hermes_42", Email: "h42@example.com", Password: "Hermes1234",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.SpaceID != 10001 || result.Role != "contributor" || result.FullAccess || result.APIKey != "sk-user" ||
		len(result.Capabilities) != 3 || len(result.KnowledgeBaseIDs) != 1 {
		t.Fatalf("result = %+v", result)
	}
}
