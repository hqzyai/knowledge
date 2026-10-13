package access

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestHQZYPrivateKBPolicyUsesAuthenticatedCaller(t *testing.T) {
	kb := &types.KnowledgeBase{ID: "private", TenantID: 10000, CreatorID: "alice", Visibility: types.KnowledgeBaseVisibilityPersonal}
	for _, user := range []string{"alice", "bob"} {
		ctx := types.WithCaller(context.Background(), types.Caller{TenantID: 10000, UserID: user, Role: types.TenantRoleContributor})
		request := KBRequest{Caller: types.CallerFromContext(ctx)}
		grant, err := ResolveKB(ctx, request, kb, types.OrgRoleViewer, nil, nil)
		if user == "bob" {
			require.ErrorIs(t, err, ErrForbidden)
			require.Nil(t, grant)
		} else {
			require.NoError(t, err)
		}
		permissions := NewKBPermissions(types.WithExecutionTenant(ctx, 20000), nil,
			func(context.Context, string) (*types.KnowledgeBase, error) { return kb, nil })
		allowed, err := permissions.Check(kb.ID, kb.TenantID, types.OrgRoleViewer)
		require.NoError(t, err)
		require.Equal(t, user == "alice", allowed)
	}
}

func TestHQZYExternalKeyDynamicGrantRemainsReadOnly(t *testing.T) {
	ctx := types.WithCaller(context.Background(), types.Caller{TenantID: 10000, UserID: "employee", Role: types.TenantRoleViewer})
	ctx = types.WithTenantAPIKeyScope(ctx, types.TenantAPIKeyScope{KnowledgeBaseIDs: types.StringArray{"own"}, IncludeWorkspaceVisibleRead: true})
	kb := &types.KnowledgeBase{ID: "workspace", TenantID: 10000, CreatorID: "other", Visibility: types.KnowledgeBaseVisibilityWorkspace}
	request := KBRequest{Caller: types.CallerFromContext(ctx)}
	grant, err := ResolveKB(ctx, request, kb, types.OrgRoleViewer, nil, nil)
	require.NoError(t, err)
	scoped := grant.Context(ctx)
	require.True(t, HasKBGrant(scoped, kb.ID, kb.TenantID, types.OrgRoleViewer))
	require.False(t, HasKBGrant(scoped, kb.ID, kb.TenantID, types.OrgRoleEditor))
	_, err = ResolveKB(ctx, request, kb, types.OrgRoleEditor, nil, nil)
	require.Error(t, err)
	kb.Visibility = types.KnowledgeBaseVisibilityPersonal
	_, err = ResolveKB(ctx, request, kb, types.OrgRoleViewer, nil, nil)
	require.Error(t, err)
	kb.Visibility = types.KnowledgeBaseVisibilityWorkspace
	kb.TenantID = 20000
	_, err = ResolveKB(ctx, request, kb, types.OrgRoleViewer, nil, nil)
	require.Error(t, err)
}
