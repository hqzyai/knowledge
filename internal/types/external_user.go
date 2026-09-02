package types

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// ExternalUserDefaultTenantID is the fixed system workspace that receives
// users provisioned by trusted external systems.
const ExternalUserDefaultTenantID uint64 = 10000

// ExternalUserAPIKeyNamePrefix reserves a deterministic API-key namespace for
// one conversation credential per provisioned external user. The internal
// user UUID in the persisted name provides stable idempotency and rotation.
const ExternalUserAPIKeyNamePrefix = "external-user/"

// ExternalUserCreateRequest creates or resumes one externally managed user.
// UserID is the stable ID from Hermes (and should be reused as user_id when
// calling /conversation-sync).
type ExternalUserCreateRequest struct {
	UserID   string `json:"user_id" binding:"required,max=128"`
	Username string `json:"username" binding:"required,min=2,max=50"`
	Email    string `json:"email" binding:"required,email,max=255"`
	Password string `json:"password" binding:"required,min=8,max=32"`
}

// ExternalUserCreateResult contains both the human account and its
// conversation-only machine credential. KnowledgeBaseIDs is the credential's
// stable personal allow-list; same-workspace KBs marked workspace-visible are
// additionally authorized at read time and are intentionally not copied into
// this array.
type ExternalUserCreateResult struct {
	ExternalUserID   string      `json:"external_user_id"`
	UserID           string      `json:"user_id"`
	Username         string      `json:"username"`
	Email            string      `json:"email"`
	SpaceID          uint64      `json:"space_id"`
	Role             TenantRole  `json:"role"`
	APIKeyID         uint64      `json:"api_key_id"`
	APIKey           string      `json:"api_key"`
	FullAccess       bool        `json:"full_access"`
	Capabilities     StringArray `json:"capabilities"`
	KnowledgeBaseIDs StringArray `json:"knowledge_base_ids"`
	UserCreated      bool        `json:"user_created"`
	APIKeyCreated    bool        `json:"api_key_created"`
}

type ExternalUserCreateResponse struct {
	Success bool                     `json:"success"`
	Data    ExternalUserCreateResult `json:"data"`
}

// ExternalUserInternalID maps a tenant-scoped external identifier to the UUID
// used by users.id. The same mapping is used by conversation-sync when it sets
// KnowledgeBase.CreatorID, so the provisioned user owns their synced KB.
func ExternalUserInternalID(tenantID uint64, externalUserID string) string {
	seed := fmt.Sprintf("weknora:external-user:v1:%d:%s", tenantID, strings.TrimSpace(externalUserID))
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(seed)).String()
}
