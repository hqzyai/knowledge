package service

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	apprepo "github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/utils"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const bootstrapTestToken = "sk-test-hqzy-admin-fixed-0123456789abcdef"

func TestHQZYAdminAPIKeyBootstrapLifecycle(t *testing.T) {
	t.Setenv("SYSTEM_AES_KEY", "0123456789abcdef0123456789abcdef")
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "keys.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&types.TenantAPIKey{}))
	repo := apprepo.NewTenantAPIKeyRepository(db)
	ctx := context.Background()
	svc := NewTenantAPIKeyService(repo)

	initialized, err := svc.InitializeHQZYAdminAPIKey(ctx, bootstrapTestToken)
	require.NoError(t, err)
	require.False(t, initialized, "startup must not create a key without the migration seed")

	tenantID := types.ExternalUserDefaultTenantID
	seed := &types.TenantAPIKey{
		TenantID: &tenantID, ScopeType: types.APIKeyScopeTenant,
		Name: "HQZY Admin Full Access", KeyHash: types.HQZYAdminAPIKeyPendingHash,
		FullAccess: true,
	}
	require.NoError(t, repo.CreateAPIKey(ctx, seed))
	initialized, err = svc.InitializeHQZYAdminAPIKey(ctx, "")
	require.NoError(t, err)
	require.False(t, initialized, "empty deployment configuration must never generate a random token")
	unchanged, err := repo.GetAPIKeyByHash(ctx, types.HQZYAdminAPIKeyPendingHash)
	require.NoError(t, err)
	require.Empty(t, unchanged.APIKey)
	_, err = svc.AuthenticateAPIKey(ctx, types.HQZYAdminAPIKeyPendingHash)
	require.ErrorIs(t, err, apprepo.ErrTenantAPIKeyNotFound)

	// Independent service instances racing to start must install one token.
	var wg sync.WaitGroup
	results := make(chan bool, 8)
	errors := make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			created, initErr := NewTenantAPIKeyService(repo).InitializeHQZYAdminAPIKey(ctx, bootstrapTestToken)
			results <- created
			errors <- initErr
		})
	}
	wg.Wait()
	close(results)
	close(errors)
	for initErr := range errors {
		require.NoError(t, initErr)
	}
	winners := 0
	for created := range results {
		if created {
			winners++
		}
	}
	require.Equal(t, 1, winners)

	keys, err := svc.ListAPIKeys(ctx, tenantID)
	require.NoError(t, err)
	require.Len(t, keys, 1)
	key := keys[0]
	require.Equal(t, seed.ID, key.ID)
	require.Equal(t, "HQZY Admin Full Access", key.Name)
	require.True(t, key.FullAccess)
	require.False(t, key.IsPlatform())
	require.Nil(t, key.ExpiresAt)
	require.Equal(t, bootstrapTestToken, key.APIKey)

	var stored types.TenantAPIKey
	require.NoError(t, db.Session(&gorm.Session{SkipHooks: true}).First(&stored, key.ID).Error)
	require.Contains(t, stored.APIKey, utils.EncPrefix)
	require.NotEqual(t, key.APIKey, stored.APIKey)
	require.Equal(t, hashTenantAPIKey(key.APIKey), stored.KeyHash)
	// Use a no-op last-used writer so authentication leaves no asynchronous
	// database work running after the test closes the connection.
	verifier := NewTenantAPIKeyService(&bootstrapAuthRepo{TenantAPIKeyRepository: repo})
	authenticated, err := verifier.AuthenticateAPIKey(ctx, key.APIKey)
	require.NoError(t, err)
	require.Equal(t, tenantID, authenticated.TenantIDValue())
	require.True(t, authenticated.FullAccess)

	// A stale initializer cannot replace the winning token.
	initialized, err = repo.SetAPIKeyToken(ctx, key.ID, types.HQZYAdminAPIKeyPendingHash, "sk-loser", "loser-hash")
	require.NoError(t, err)
	require.False(t, initialized)
	initialized, err = svc.InitializeHQZYAdminAPIKey(ctx, bootstrapTestToken)
	require.NoError(t, err)
	require.False(t, initialized)
	keys, err = svc.ListAPIKeys(ctx, tenantID)
	require.NoError(t, err)
	require.Equal(t, key.APIKey, keys[0].APIKey)

	// Configuration changes replace the old token on the same named record,
	// including a credential issued by the earlier random bootstrap version.
	const replacement = "sk-test-hqzy-admin-updated-0123456789abcdef"
	initialized, err = svc.InitializeHQZYAdminAPIKey(ctx, replacement)
	require.NoError(t, err)
	require.True(t, initialized)
	_, err = verifier.AuthenticateAPIKey(ctx, bootstrapTestToken)
	require.ErrorIs(t, err, apprepo.ErrTenantAPIKeyNotFound)
	authenticated, err = verifier.AuthenticateAPIKey(ctx, replacement)
	require.NoError(t, err)
	require.Equal(t, key.ID, authenticated.ID)
	require.True(t, authenticated.FullAccess)
	initialized, err = svc.InitializeHQZYAdminAPIKey(ctx, replacement)
	require.NoError(t, err)
	require.False(t, initialized)

	require.NoError(t, svc.RevokeAPIKey(ctx, tenantID, key.ID))
	initialized, err = svc.InitializeHQZYAdminAPIKey(ctx, replacement)
	require.NoError(t, err)
	require.False(t, initialized)
	_, err = verifier.AuthenticateAPIKey(ctx, replacement)
	require.ErrorIs(t, err, apprepo.ErrTenantAPIKeyNotFound)
	var count int64
	require.NoError(t, db.Model(&types.TenantAPIKey{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

type bootstrapAuthRepo struct {
	interfaces.TenantAPIKeyRepository
}

func (*bootstrapAuthRepo) UpdateAPIKeyLastUsed(context.Context, uint64, time.Time) error {
	return nil
}

func TestHQZYAdminAPIKeyBootstrapRespectsPendingRevocation(t *testing.T) {
	repo := newFakeTenantAPIKeyRepo()
	now := time.Now().UTC()
	seed := &types.TenantAPIKey{
		TenantID: uint64Pointer(10000), ScopeType: types.APIKeyScopeTenant,
		KeyHash: types.HQZYAdminAPIKeyPendingHash, FullAccess: true, RevokedAt: &now,
	}
	require.NoError(t, repo.CreateAPIKey(context.Background(), seed))
	initialized, err := NewTenantAPIKeyService(repo).InitializeHQZYAdminAPIKey(context.Background(), bootstrapTestToken)
	require.NoError(t, err)
	require.False(t, initialized)
	require.Empty(t, repo.byHash[types.HQZYAdminAPIKeyPendingHash].APIKey)
}

func TestHQZYAdminAPIKeyBootstrapRejectsInvalidConfiguredToken(t *testing.T) {
	for _, token := range []string{
		"sk-short", strings.Repeat("a", 40), "sk-" + strings.Repeat("a", 254),
		bootstrapTestToken + "\n", " " + bootstrapTestToken, bootstrapTestToken + "中文",
	} {
		repo := newFakeTenantAPIKeyRepo()
		initialized, err := NewTenantAPIKeyService(repo).InitializeHQZYAdminAPIKey(context.Background(), token)
		require.Error(t, err)
		require.NotContains(t, err.Error(), token)
		require.False(t, initialized)
		require.Empty(t, repo.byHash)
	}
}
