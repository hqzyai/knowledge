package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
)

var errOIDCEmployeeNotProvisioned = errors.New("该飞书身份尚未绑定到企业员工账号，请先在 AgentOS 开通账号或联系管理员绑定")

func (s *userService) resolveOIDCAccount(
	ctx context.Context, cfg *config.OIDCAuthConfig, info *types.OIDCUserInfo,
	provisioning types.TenantProvisioningMode,
) (*types.User, bool, error) {
	if strings.TrimSpace(cfg.IssuerURL) == "" || strings.TrimSpace(info.Subject) == "" ||
		len(cfg.IssuerURL) > 512 || len(info.Subject) > 255 {
		return nil, false, errors.New("OIDC provider did not return a valid issuer and subject")
	}
	identity, err := s.userRepo.GetOIDCIdentity(ctx, cfg.IssuerURL, info.Subject)
	if err != nil {
		return nil, false, fmt.Errorf("failed to load OIDC identity: %w", err)
	}
	var user *types.User
	created := false
	if identity != nil {
		// Never fall back to email for an established binding, including when
		// the bound account has been disabled or deleted.
		user, err = s.userRepo.GetUserByID(ctx, identity.UserID)
		if err != nil {
			return nil, false, errOIDCEmployeeNotProvisioned
		}
	} else {
		email := strings.TrimSpace(info.Email)
		if email == "" {
			return nil, false, errors.New("OIDC provider did not return email")
		}
		user, err = s.userRepo.GetUserByEmail(ctx, email)
		if err != nil && !isUserLookupNotFound(err) {
			return nil, false, fmt.Errorf("failed to query OIDC user: %w", err)
		}
		if user == nil || isUserLookupNotFound(err) {
			if cfg.ExistingUserTenantID != 0 {
				return nil, false, errOIDCEmployeeNotProvisioned
			}
			user, err = s.provisionOIDCUser(ctx, info, provisioning)
			if err != nil {
				return nil, false, err
			}
			created = true
		}
	}
	if user == nil || !user.IsActive || user.DeletedAt.Valid {
		return nil, false, errors.New("Account is disabled")
	}
	if tenantID := cfg.ExistingUserTenantID; tenantID != 0 {
		if s.memberService == nil || s.tenantService == nil {
			return nil, false, errOIDCEmployeeNotProvisioned
		}
		member, err := s.memberService.GetMembership(ctx, user.ID, tenantID)
		if err != nil {
			return nil, false, err
		}
		if member == nil || member.Status != types.TenantMemberStatusActive {
			return nil, false, errOIDCEmployeeNotProvisioned
		}
		tenant, err := s.tenantService.GetTenantByID(ctx, tenantID)
		if err != nil || tenant == nil || tenant.Status != "active" {
			return nil, false, errors.New("企业空间暂不可用，请联系管理员")
		}
	}
	if identity == nil {
		err = s.userRepo.BindOIDCIdentity(ctx, &types.UserOIDCIdentity{
			Issuer: cfg.IssuerURL, Subject: info.Subject, UserID: user.ID, CreatedAt: time.Now(),
		})
		if err != nil {
			return nil, false, fmt.Errorf("failed to bind OIDC identity: %w", err)
		}
	}
	return user, created, nil
}
