package access

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// Context grants contain values, not mutable KB objects. Deriving a context
// copies the grant slice so parallel search branches cannot widen each other.
type kbGrant struct {
	caller           types.Caller
	kbID             string
	tenantID         uint64
	permission       types.OrgMemberRole
	task             bool
	workspaceVisible bool
}

type agentGrant struct {
	caller types.Caller
	scope  types.SharedAgentKBScope
}

// WithSharedAgent scopes execution after the caller has authorized this exact
// agent. Only its configured KBs receive read access; other source-tenant KBs
// and the source tenant's organization memberships are not inherited.
func WithSharedAgent(ctx context.Context, agent *types.CustomAgent) context.Context {
	grant := agentGrant{caller: types.CallerFromContext(ctx), scope: types.NewSharedAgentKBScope(agent)}
	ctx = context.WithValue(ctx, types.SharedAgentGrantContextKey, grant)
	if agent == nil {
		return ctx
	}
	return types.WithExecutionTenant(ctx, agent.TenantID)
}

// HasKBGrant checks only previously resolved resource access. It never infers
// ownership from the execution tenant, and always reapplies API-key scope.
func HasKBGrant(ctx context.Context, kbID string, tenantID uint64, required types.OrgMemberRole) bool {
	if !required.IsValid() {
		return false
	}
	outsideScope := types.AuthorizeTenantAPIKeyKnowledgeBases(ctx, kbID) != nil
	if outsideScope && (required != types.OrgRoleViewer || !types.TenantAPIKeyAllowsWorkspaceVisibleRead(ctx)) {
		return false
	}
	caller := types.CallerFromContext(ctx)
	if kbID == "" || tenantID == 0 {
		return false
	}
	grants, _ := ctx.Value(types.KBGrantsContextKey).([]kbGrant)
	for _, grant := range grants {
		if outsideScope && (!grant.workspaceVisible || grant.tenantID != caller.TenantID) {
			continue
		}
		if (caller.TenantID != 0 || grant.task) && grant.caller == caller && grant.kbID == kbID &&
			grant.tenantID == tenantID &&
			grant.permission.HasPermission(required) {
			return true
		}
	}
	grant, ok := ctx.Value(types.SharedAgentGrantContextKey).(agentGrant)
	return !outsideScope && ok && caller.TenantID != 0 && required == types.OrgRoleViewer && grant.caller == caller &&
		grant.scope.Allows(kbID, tenantID)
}

// KBPermissions combines caller ownership, exact context grants and organization
// shares for one service operation. Pass nil shares when that entry point does
// not permit organization expansion (for example, a caller without a user).
type KBPermissions struct {
	ctx    context.Context
	caller types.Caller
	shares *KBSharePermissions
	lookup func(context.Context, string) (*types.KnowledgeBase, error)
	kbs    map[string]*types.KnowledgeBase
}

// NewKBPermissions resolves reads for one caller and operation.
func NewKBPermissions(ctx context.Context, shares KBShareLookup, lookup ...func(context.Context, string) (*types.KnowledgeBase, error)) *KBPermissions {
	caller := types.CallerFromContext(ctx)
	p := &KBPermissions{
		ctx:    ctx,
		caller: caller,
		shares: NewKBSharePermissions(ctx, shares, caller.TenantID, caller.Role),
		kbs:    make(map[string]*types.KnowledgeBase),
	}
	if len(lookup) > 0 {
		p.lookup = lookup[0]
	}
	return p
}

// CheckKnowledgeBase evaluates workspace visibility on the persisted object.
func (p *KBPermissions) CheckKnowledgeBase(kb *types.KnowledgeBase, required types.OrgMemberRole) (bool, error) {
	if kb == nil {
		return false, nil
	}
	if kb.TenantID == p.caller.TenantID && required == types.OrgRoleViewer {
		if err := types.AuthorizeTenantAPIKeyKnowledgeBaseRead(p.ctx, kb); err != nil {
			return false, err
		}
		return types.CanReadKnowledgeBaseInWorkspace(p.ctx, kb), nil
	}
	return p.checkReference(kb.ID, kb.TenantID, required)
}

// Check combines caller ownership, exact grants and organization sharing.
func (p *KBPermissions) Check(kbID string, ownerTenantID uint64, required types.OrgMemberRole) (bool, error) {
	if p.lookup != nil && ownerTenantID == p.caller.TenantID {
		kb := p.kbs[kbID]
		if kb == nil {
			var err error
			kb, err = p.lookup(p.ctx, kbID)
			if err != nil {
				return false, err
			}
			if kb == nil || kb.ID != kbID || kb.TenantID != ownerTenantID {
				return false, nil
			}
			p.kbs[kbID] = kb
		}
		return p.CheckKnowledgeBase(kb, required)
	}
	return p.checkReference(kbID, ownerTenantID, required)
}

func (p *KBPermissions) checkReference(kbID string, ownerTenantID uint64, required types.OrgMemberRole) (bool, error) {
	if kbID == "" || ownerTenantID == 0 || !required.IsValid() {
		return false, nil
	}
	if err := types.AuthorizeTenantAPIKeyKnowledgeBases(p.ctx, kbID); err != nil {
		return false, err
	}
	if (p.caller.TenantID == ownerTenantID && required == types.OrgRoleViewer) ||
		HasKBGrant(p.ctx, kbID, ownerTenantID, required) {
		return true, nil
	}
	if p.caller.TenantID == 0 || p.caller.TenantID == ownerTenantID {
		return false, nil
	}
	return p.shares.Check(kbID, required)
}
