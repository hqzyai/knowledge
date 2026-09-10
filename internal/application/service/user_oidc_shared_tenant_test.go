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

type sharedOIDCUsers struct {
	interfaces.UserRepository
	users   map[string]*types.User
	created []*types.User
	deleted []string
}

func (r *sharedOIDCUsers) GetUserByEmail(_ context.Context, email string) (*types.User, error) {
	for _, u := range r.users {
		if u.Email == email {
			return u, nil
		}
	}
	return nil, apprepo.ErrUserNotFound
}
func (r *sharedOIDCUsers) GetUserByUsername(_ context.Context, name string) (*types.User, error) {
	for _, u := range r.users {
		if u.Username == name {
			return u, nil
		}
	}
	return nil, apprepo.ErrUserNotFound
}
func (r *sharedOIDCUsers) CreateUser(_ context.Context, u *types.User) error {
	r.users[u.ID] = u
	r.created = append(r.created, u)
	return nil
}
func (r *sharedOIDCUsers) UpdateUser(_ context.Context, u *types.User) error {
	r.users[u.ID] = u
	return nil
}
func (r *sharedOIDCUsers) DeleteUser(_ context.Context, id string) error {
	r.deleted = append(r.deleted, id)
	delete(r.users, id)
	return nil
}

type sharedOIDCTenants struct {
	interfaces.TenantService
	active  bool
	created int
	deleted int
}

func (s *sharedOIDCTenants) GetTenantByID(_ context.Context, id uint64) (*types.Tenant, error) {
	if !s.active {
		return nil, errors.New("unavailable")
	}
	return &types.Tenant{ID: id, Name: "Company", Status: "active"}, nil
}
func (s *sharedOIDCTenants) CreateTenant(context.Context, *types.Tenant) (*types.Tenant, error) {
	s.created++
	return &types.Tenant{ID: 999, Name: "Personal", Status: "active"}, nil
}
func (s *sharedOIDCTenants) DeleteTenant(context.Context, uint64) error { s.deleted++; return nil }

type sharedOIDCMembers struct {
	interfaces.TenantMemberService
	added    []*types.TenantMember
	existing []*types.TenantMember
	addError error
	owners   int
}

func (s *sharedOIDCMembers) AddMember(_ context.Context, uid string, tid uint64, role types.TenantRole, _ *string) (*types.TenantMember, error) {
	if s.addError != nil {
		return nil, s.addError
	}
	m := &types.TenantMember{UserID: uid, TenantID: tid, Role: role, Status: types.TenantMemberStatusActive}
	s.added = append(s.added, m)
	return m, nil
}
func (s *sharedOIDCMembers) ListByUser(_ context.Context, uid string) ([]*types.TenantMember, error) {
	var result []*types.TenantMember
	for _, m := range append(append([]*types.TenantMember{}, s.existing...), s.added...) {
		if m.UserID == uid {
			result = append(result, m)
		}
	}
	return result, nil
}
func (s *sharedOIDCMembers) GetMembership(ctx context.Context, uid string, tid uint64) (*types.TenantMember, error) {
	members, _ := s.ListByUser(ctx, uid)
	for _, m := range members {
		if m.TenantID == tid {
			return m, nil
		}
	}
	return nil, nil
}
func (s *sharedOIDCMembers) EnsureOwner(context.Context, string, uint64) (*types.TenantMember, error) {
	s.owners++
	return nil, errors.New("unexpected owner grant")
}

func TestOIDCSharedWorkspaceKeepsEmailMatchingWithoutMergingAccounts(t *testing.T) {
	withOIDCSSRFWhitelist(t, "127.0.0.1")
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	for _, sameEmail := range []bool{false, true} {
		t.Run(map[bool]string{false: "different_email_creates_independent_user", true: "same_email_reuses_existing_user"}[sameEmail], func(t *testing.T) {
			old := &types.User{ID: "old-user", Username: "employee", Email: "old@example.com", TenantID: 20000, IsActive: true, PasswordHash: "unchanged-password", Preferences: types.UserPreferences{MustChangePassword: true}}
			users := &sharedOIDCUsers{users: map[string]*types.User{old.ID: old}}
			tenants := &sharedOIDCTenants{active: true}
			members := &sharedOIDCMembers{existing: []*types.TenantMember{{UserID: old.ID, TenantID: old.TenantID, Role: types.TenantRoleContributor, Status: types.TenantMemberStatusActive}}}
			var provider *httptest.Server
			provider = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/keys":
					_ = json.NewEncoder(w).Encode(oidcJWKS{Keys: []oidcJWK{rsaJWK(key, "key")}})
				case "/token":
					claims := baseClaims(provider.URL, "client")
					claims["sub"] = "same-feishu-subject"
					claims["name"] = "employee"
					claims["email"] = "new@example.com"
					if sameEmail {
						claims["email"] = old.Email
					}
					_ = json.NewEncoder(w).Encode(oidcTokenResponse{IDToken: signedIDToken(t, key, "key", claims)})
				default:
					http.NotFound(w, r)
				}
			}))
			defer provider.Close()
			cfg := &config.OIDCAuthConfig{Enable: true, NewUserTenantID: 10000, IssuerURL: provider.URL, ClientID: "client", ClientSecret: "secret", AuthorizationEndpoint: provider.URL + "/authorize", TokenEndpoint: provider.URL + "/token", JwksURI: provider.URL + "/keys"}
			svc := &userService{config: &config.Config{OIDCAuth: cfg}, userRepo: users, tenantService: tenants, memberService: members, tokenRepo: &stubAuthTokenRepo{}}
			result, err := svc.LoginWithOIDC(context.Background(), "code", "https://app.example/callback", types.TenantProvisioningCreatePersonal)
			if err != nil {
				t.Fatal(err)
			}
			wantTenantID := uint64(10000)
			if sameEmail {
				wantTenantID = old.TenantID
			}
			if !result.Success || result.Tenant.ID != wantTenantID || result.User.TenantID != wantTenantID || tenants.created != 0 || members.owners != 0 {
				t.Fatalf("unexpected provisioning: result=%+v tenants=%d owners=%d", result, tenants.created, members.owners)
			}
			if sameEmail {
				if result.User.ID != old.ID || result.IsNewUser || len(users.created) != 0 || len(members.added) != 0 {
					t.Fatal("existing user was modified or replaced")
				}
			} else {
				if result.User.ID == old.ID || !result.IsNewUser || len(users.created) != 1 || len(members.added) != 1 || members.added[0].Role != types.TenantRoleContributor {
					t.Fatal("new identity was merged or not added as contributor")
				}
				if result.User.Preferences.OidcOnlyLogin == nil || !*result.User.Preferences.OidcOnlyLogin {
					t.Fatal("new account lost OIDC-only behavior")
				}
			}
			if old.Email != "old@example.com" || old.PasswordHash != "unchanged-password" || old.TenantID != 20000 || !old.Preferences.MustChangePassword {
				t.Fatal("original employee changed")
			}
		})
	}
}

func TestOIDCSharedWorkspaceFailureDoesNotCreateOrDeleteWorkspaces(t *testing.T) {
	for _, stage := range []string{"missing-workspace", "membership-failure"} {
		t.Run(stage, func(t *testing.T) {
			users := &sharedOIDCUsers{users: map[string]*types.User{}}
			tenants := &sharedOIDCTenants{active: stage != "missing-workspace"}
			members := &sharedOIDCMembers{}
			if stage == "membership-failure" {
				members.addError = errors.New("membership unavailable")
			}
			svc := &userService{userRepo: users, tenantService: tenants, memberService: members}
			_, err := svc.provisionOIDCUser(context.Background(), &types.OIDCUserInfo{Username: "employee", Email: "new@example.com"}, types.TenantProvisioningCreatePersonal, 10000)
			if err == nil || tenants.created != 0 || tenants.deleted != 0 || len(users.users) != 0 {
				t.Fatalf("failure modified workspace or left account: err=%v tenants=%+v users=%d", err, tenants, len(users.users))
			}
		})
	}
}

func TestPublicRegistrationCannotChooseSharedWorkspace(t *testing.T) {
	var req types.RegisterRequest
	if err := json.Unmarshal([]byte(`{"username":"employee","email":"new@example.com","password":"Password123!","tenant_provisioning":"join_existing","join_tenant_id":10000}`), &req); err != nil {
		t.Fatal(err)
	}
	if req.TenantProvisioning != "" || req.JoinTenantID != 0 {
		t.Fatal("browser supplied trusted provisioning fields")
	}
}
