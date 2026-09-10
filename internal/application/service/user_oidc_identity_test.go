package service

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	apprepo "github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type identityUserRepo struct {
	interfaces.UserRepository
	users    map[string]*types.User
	bindings map[string]*types.UserOIDCIdentity
	creates  int
}

func (r *identityUserRepo) GetOIDCIdentity(_ context.Context, issuer, subject string) (*types.UserOIDCIdentity, error) {
	return r.bindings[issuer+"|"+subject], nil
}
func (r *identityUserRepo) BindOIDCIdentity(_ context.Context, identity *types.UserOIDCIdentity) error {
	k := identity.Issuer + "|" + identity.Subject
	if old := r.bindings[k]; old != nil && old.UserID != identity.UserID {
		return apprepo.ErrOIDCIdentityConflict
	}
	r.bindings[k] = identity
	return nil
}
func (r *identityUserRepo) GetUserByID(_ context.Context, id string) (*types.User, error) {
	if user := r.users[id]; user != nil {
		return user, nil
	}
	return nil, apprepo.ErrUserNotFound
}
func (r *identityUserRepo) GetUserByEmail(_ context.Context, email string) (*types.User, error) {
	for _, u := range r.users {
		if u.Email == email {
			return u, nil
		}
	}
	return nil, apprepo.ErrUserNotFound
}
func (*identityUserRepo) GetUserByUsername(context.Context, string) (*types.User, error) {
	return nil, apprepo.ErrUserNotFound
}
func (r *identityUserRepo) CreateUser(_ context.Context, u *types.User) error {
	r.creates++
	r.users[u.ID] = u
	return nil
}
func (r *identityUserRepo) UpdateUser(_ context.Context, u *types.User) error {
	r.users[u.ID] = u
	return nil
}

type identityMembers struct {
	interfaces.TenantMemberService
	active bool
}

func (m *identityMembers) GetMembership(_ context.Context, uid string, tid uint64) (*types.TenantMember, error) {
	if !m.active {
		return nil, nil
	}
	return &types.TenantMember{UserID: uid, TenantID: tid, Role: types.TenantRoleContributor, Status: types.TenantMemberStatusActive}, nil
}
func (m *identityMembers) ListByUser(ctx context.Context, uid string) ([]*types.TenantMember, error) {
	member, _ := m.GetMembership(ctx, uid, 10000)
	return []*types.TenantMember{member}, nil
}

type identityTenants struct{ interfaces.TenantService }

func (*identityTenants) GetTenantByID(_ context.Context, tid uint64) (*types.Tenant, error) {
	return &types.Tenant{ID: tid, Name: "Company", Status: "active"}, nil
}

func identityFixture() (*userService, *identityUserRepo, *config.OIDCAuthConfig) {
	r := &identityUserRepo{users: map[string]*types.User{"employee": {ID: "employee", Email: "old@example.com", TenantID: 10000, IsActive: true, PasswordHash: "keep-password", Preferences: types.UserPreferences{MustChangePassword: true}}}, bindings: map[string]*types.UserOIDCIdentity{}}
	s := &userService{userRepo: r, memberService: &identityMembers{active: true}, tenantService: &identityTenants{}, tokenRepo: &stubAuthTokenRepo{}}
	return s, r, &config.OIDCAuthConfig{IssuerURL: "https://idp.example", ExistingUserTenantID: 10000}
}

func TestOIDCStableIdentityKeepsOriginalEmployeeWhenEmailChanges(t *testing.T) {
	s, r, cfg := identityFixture()
	ctx := context.Background()
	info := &types.OIDCUserInfo{Subject: "employee-subject", Email: "old@example.com"}
	u, created, err := s.resolveOIDCAccount(ctx, cfg, info, types.TenantProvisioningCreatePersonal)
	if err != nil || created || u.ID != "employee" {
		t.Fatalf("first login: user=%v created=%v err=%v", u, created, err)
	}
	// A pre-existing duplicate with the provider's new email must not win.
	r.users["duplicate"] = &types.User{ID: "duplicate", Email: "new@example.com", TenantID: 10001, IsActive: true}
	info.Email = "new@example.com"
	u, created, err = s.resolveOIDCAccount(ctx, cfg, info, types.TenantProvisioningCreatePersonal)
	if err != nil || created || u.ID != "employee" || u.TenantID != 10000 || u.PasswordHash != "keep-password" || !u.Preferences.MustChangePassword || r.creates != 0 {
		t.Fatalf("changed-email login did not preserve employee: user=%v created=%v err=%v", u, created, err)
	}
}

func TestOIDCExistingWorkspaceRejectsUnknownOrRemovedEmployee(t *testing.T) {
	for _, tc := range []string{"unknown", "membership-removed", "disabled", "deleted-binding"} {
		t.Run(tc, func(t *testing.T) {
			s, r, cfg := identityFixture()
			info := &types.OIDCUserInfo{Subject: "subject", Email: "old@example.com"}
			switch tc {
			case "unknown":
				info.Email = "new@example.com"
			case "membership-removed":
				s.memberService = &identityMembers{}
			case "disabled":
				r.users["employee"].IsActive = false
			case "deleted-binding":
				r.bindings[cfg.IssuerURL+"|subject"] = &types.UserOIDCIdentity{UserID: "missing"}
			}
			u, _, err := s.resolveOIDCAccount(context.Background(), cfg, info, types.TenantProvisioningCreatePersonal)
			if err == nil || u != nil || r.creates != 0 {
				t.Fatalf("unexpected account/provisioning: user=%v err=%v creates=%d", u, err, r.creates)
			}
		})
	}
}

func TestOIDCBindingDoesNotCrossIssuers(t *testing.T) {
	s, r, cfg := identityFixture()
	r.bindings["https://other-idp.example|subject"] = &types.UserOIDCIdentity{UserID: "employee"}
	_, _, err := s.resolveOIDCAccount(context.Background(), cfg, &types.OIDCUserInfo{Subject: "subject", Email: "unknown@example.com"}, types.TenantProvisioningCreatePersonal)
	if !errors.Is(err, errOIDCEmployeeNotProvisioned) {
		t.Fatalf("other issuer binding was used: %v", err)
	}
}

func TestOIDCDefaultModeStillProvisionsTenantlessUsers(t *testing.T) {
	s, r, cfg := identityFixture()
	cfg.ExistingUserTenantID = 0
	u, created, err := s.resolveOIDCAccount(context.Background(), cfg, &types.OIDCUserInfo{Subject: "new-subject", Email: "new@example.com", Username: "newemployee"}, types.TenantProvisioningTenantless)
	if err != nil || !created || u.TenantID != 0 || r.creates != 1 || len(r.bindings) != 1 {
		t.Fatalf("default provisioning: user=%v created=%v err=%v", u, created, err)
	}
}

func TestOIDCLoginUsesBoundEmployeeAndEnterpriseTenant(t *testing.T) {
	withOIDCSSRFWhitelist(t, "127.0.0.1")
	s, r, cfg := identityFixture()
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	var provider *httptest.Server
	provider = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/keys":
			_ = json.NewEncoder(w).Encode(oidcJWKS{Keys: []oidcJWK{rsaJWK(key, "key")}})
		case "/token":
			claims := baseClaims(provider.URL, "client")
			claims["sub"] = "subject"
			claims["email"] = "changed@example.com"
			_ = json.NewEncoder(w).Encode(oidcTokenResponse{IDToken: signedIDToken(t, key, "key", claims)})
		default:
			http.NotFound(w, req)
		}
	}))
	defer provider.Close()
	cfg.Enable = true
	cfg.IssuerURL = provider.URL
	cfg.ClientID = "client"
	cfg.ClientSecret = "secret"
	cfg.AuthorizationEndpoint = provider.URL + "/authorize"
	cfg.TokenEndpoint = provider.URL + "/token"
	cfg.JwksURI = provider.URL + "/keys"
	s.config = &config.Config{OIDCAuth: cfg}
	r.bindings[provider.URL+"|subject"] = &types.UserOIDCIdentity{UserID: "employee"}
	resp, err := s.LoginWithOIDC(context.Background(), "code", "https://app.example/callback", types.TenantProvisioningCreatePersonal)
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Success || resp.IsNewUser || resp.User.ID != "employee" || resp.Tenant.ID != 10000 || len(resp.Memberships) != 1 || r.creates != 0 {
		t.Fatalf("wrong login account/workspace: %+v", resp)
	}
}

func TestOIDCUserinfoCannotReplaceVerifiedSubject(t *testing.T) {
	withOIDCSSRFWhitelist(t, "127.0.0.1")
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	keys := jwksServer(t, key, "key")
	defer keys.Close()
	for _, subject := range []any{"another-user", []string{"user-123"}, nil} {
		userinfo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"sub": subject, "email": "old@example.com"})
		}))
		cfg := oidcVerifyCfg(keys.URL)
		cfg.UserInfoEndpoint = userinfo.URL
		_, err := (&userService{}).resolveOIDCUserInfo(context.Background(), cfg, &oidcTokenResponse{
			IDToken: signedIDToken(t, key, "key", baseClaims(cfg.IssuerURL, cfg.ClientID)), AccessToken: "test-token",
		})
		userinfo.Close()
		if err == nil {
			t.Fatalf("userinfo subject %v was accepted", subject)
		}
	}
}
