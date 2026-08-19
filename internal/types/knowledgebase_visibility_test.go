package types

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func kbVisibilityContext(tenantID uint64, userID string, role TenantRole) context.Context {
	ctx := context.WithValue(context.Background(), TenantIDContextKey, tenantID)
	ctx = context.WithValue(ctx, UserIDContextKey, userID)
	ctx = context.WithValue(ctx, TenantRoleContextKey, role)
	return ctx
}

func TestCanReadKnowledgeBaseInWorkspace(t *testing.T) {
	personal := &KnowledgeBase{
		ID: "kb-personal", TenantID: 100, CreatorID: "creator",
		Visibility: KnowledgeBaseVisibilityPersonal,
	}
	workspace := &KnowledgeBase{
		ID: "kb-workspace", TenantID: 100, CreatorID: "creator",
		Visibility: KnowledgeBaseVisibilityWorkspace,
	}

	require.True(t, CanReadKnowledgeBaseInWorkspace(
		kbVisibilityContext(100, "creator", TenantRoleContributor), personal,
	))
	require.False(t, CanReadKnowledgeBaseInWorkspace(
		kbVisibilityContext(100, "other", TenantRoleContributor), personal,
	))
	require.True(t, CanReadKnowledgeBaseInWorkspace(
		kbVisibilityContext(100, "admin", TenantRoleAdmin), personal,
	))
	require.True(t, CanReadKnowledgeBaseInWorkspace(
		kbVisibilityContext(100, "other", TenantRoleViewer), workspace,
	))
	require.False(t, CanReadKnowledgeBaseInWorkspace(
		kbVisibilityContext(200, "creator", TenantRoleOwner), personal,
	), "workspace-local visibility never grants cross-workspace access")
}

func TestCanReadKnowledgeBaseInWorkspace_APIKeyKeepsAllowListScope(t *testing.T) {
	kb := &KnowledgeBase{
		ID: "kb-personal", TenantID: 100, CreatorID: "creator",
		Visibility: KnowledgeBaseVisibilityPersonal,
	}
	ctx := kbVisibilityContext(100, "system-100", TenantRoleOwner)
	ctx = context.WithValue(ctx, TenantAPIKeyScopeContextKey, TenantAPIKeyScope{
		KnowledgeBaseIDs: StringArray{"kb-personal"},
	})
	require.True(t, CanReadKnowledgeBaseInWorkspace(ctx, kb))

	ctx = context.WithValue(ctx, TenantAPIKeyScopeContextKey, TenantAPIKeyScope{
		KnowledgeBaseIDs: StringArray{"kb-other"},
	})
	require.False(t, CanReadKnowledgeBaseInWorkspace(ctx, kb))
}
