package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestPreferencesCannotClearPasswordChangeRestriction(t *testing.T) {
	svc := newAuthTestUserService(&stubAuthTokenRepo{})
	repo := svc.userRepo.(*stubUserRepoForAuth)
	repo.users["user-1"].Preferences.MustChangePassword = true
	tenantID := uint64(2)
	prefs, err := svc.UpdateUserPreferences(context.Background(), "user-1", types.UserPreferences{
		LastActiveTenantID: &tenantID, MustChangePassword: false,
	})
	require.NoError(t, err)
	require.True(t, prefs.MustChangePassword)
	require.Equal(t, tenantID, *prefs.LastActiveTenantID)
}
