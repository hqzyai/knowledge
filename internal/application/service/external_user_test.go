package service

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"

	apprepo "github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type externalUserRepo struct {
	interfaces.UserRepository
	mu    sync.Mutex
	users map[string]*types.User
}

func newExternalUserRepo() *externalUserRepo {
	return &externalUserRepo{users: map[string]*types.User{}}
}

func cloneExternalUser(user *types.User) *types.User {
	if user == nil {
		return nil
	}
	out := *user
	return &out
}

func (r *externalUserRepo) CreateUser(_ context.Context, user *types.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.users {
		if existing.ID == user.ID || existing.Email == user.Email || existing.Username == user.Username {
			return errors.New("unique constraint failed")
		}
	}
	r.users[user.ID] = cloneExternalUser(user)
	return nil
}

func (r *externalUserRepo) GetUserByID(_ context.Context, id string) (*types.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if user := r.users[id]; user != nil {
		return cloneExternalUser(user), nil
	}
	return nil, apprepo.ErrUserNotFound
}

func (r *externalUserRepo) GetUserByEmail(_ context.Context, email string) (*types.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, user := range r.users {
		if user.Email == email {
			return cloneExternalUser(user), nil
		}
	}
	return nil, apprepo.ErrUserNotFound
}

func (r *externalUserRepo) GetUserByUsername(_ context.Context, username string) (*types.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, user := range r.users {
		if user.Username == username {
			return cloneExternalUser(user), nil
		}
	}
	return nil, apprepo.ErrUserNotFound
}

type externalUserTenantService struct{ interfaces.TenantService }

func (externalUserTenantService) GetTenantByID(_ context.Context, id uint64) (*types.Tenant, error) {
	if id != types.ExternalUserDefaultTenantID {
		return nil, errors.New("tenant not found")
	}
	return &types.Tenant{ID: id, Status: "active"}, nil
}

type externalUserMemberService struct {
	interfaces.TenantMemberService
	mu      sync.Mutex
	members map[string]*types.TenantMember
}

func newExternalUserMemberService() *externalUserMemberService {
	return &externalUserMemberService{members: map[string]*types.TenantMember{}}
}

func externalMemberKey(userID string, tenantID uint64) string {
	return userID + ":" + strconv.FormatUint(tenantID, 10)
}

func (s *externalUserMemberService) GetMembership(
	_ context.Context, userID string, tenantID uint64,
) (*types.TenantMember, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	member := s.members[externalMemberKey(userID, tenantID)]
	if member == nil {
		return nil, nil
	}
	out := *member
	return &out, nil
}

func (s *externalUserMemberService) AddMember(
	_ context.Context, userID string, tenantID uint64, role types.TenantRole, invitedBy *string,
) (*types.TenantMember, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := externalMemberKey(userID, tenantID)
	if s.members[key] != nil {
		return nil, ErrMembershipAlreadyExists
	}
	member := &types.TenantMember{
		UserID: userID, TenantID: tenantID, Role: role,
		Status: types.TenantMemberStatusActive, InvitedBy: invitedBy,
	}
	s.members[key] = member
	out := *member
	return &out, nil
}

func (s *externalUserMemberService) UpdateRole(
	_ context.Context, userID string, tenantID uint64, role types.TenantRole,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	member := s.members[externalMemberKey(userID, tenantID)]
	if member == nil {
		return errors.New("membership not found")
	}
	member.Role = role
	return nil
}

func TestProvisionExternalUserIsIdempotentContributor(t *testing.T) {
	repo := newExternalUserRepo()
	members := newExternalUserMemberService()
	svc := &userService{
		userRepo: repo, tenantService: externalUserTenantService{}, memberService: members,
	}
	req := &types.ExternalUserCreateRequest{
		UserID: "hermes-user-123", Username: "hermes_user_123",
		Email: "Hermes.User.123@Example.com", Password: "Hermes123456",
	}

	first, created, err := svc.ProvisionExternalUser(
		context.Background(), req, types.ExternalUserDefaultTenantID)
	require.NoError(t, err)
	require.True(t, created)
	require.Equal(t, types.ExternalUserInternalID(types.ExternalUserDefaultTenantID, req.UserID), first.ID)
	require.Equal(t, types.ExternalUserDefaultTenantID, first.TenantID)
	require.Equal(t, "hermes.user.123@example.com", first.Email)

	member, err := members.GetMembership(context.Background(), first.ID, first.TenantID)
	require.NoError(t, err)
	require.NotNil(t, member)
	require.Equal(t, types.TenantRoleContributor, member.Role)

	second, created, err := svc.ProvisionExternalUser(
		context.Background(), req, types.ExternalUserDefaultTenantID)
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, first.ID, second.ID)
	require.Len(t, repo.users, 1)
	require.Len(t, members.members, 1)
}

func TestProvisionExternalUserRejectsIdentityMutation(t *testing.T) {
	repo := newExternalUserRepo()
	members := newExternalUserMemberService()
	svc := &userService{
		userRepo: repo, tenantService: externalUserTenantService{}, memberService: members,
	}
	req := &types.ExternalUserCreateRequest{
		UserID: "stable-id", Username: "stable_user", Email: "stable@example.com", Password: "Stable1234",
	}
	_, _, err := svc.ProvisionExternalUser(context.Background(), req, types.ExternalUserDefaultTenantID)
	require.NoError(t, err)

	changed := *req
	changed.Email = "someone-else@example.com"
	_, _, err = svc.ProvisionExternalUser(context.Background(), &changed, types.ExternalUserDefaultTenantID)
	require.ErrorIs(t, err, ErrExternalUserIdentityConflict)
}
