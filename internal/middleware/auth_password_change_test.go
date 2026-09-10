package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestInitialPasswordSessionRestriction(t *testing.T) {
	gin.SetMode(gin.TestMode)
	user := &types.User{ID: "employee", Preferences: types.UserPreferences{MustChangePassword: true}}
	for _, tc := range []struct {
		method, path string
		allowed      bool
	}{
		{http.MethodGet, "/api/v1/auth/me", true},
		{http.MethodGet, "/api/v1/auth/validate", true},
		{http.MethodPost, "/api/v1/auth/change-password", true},
		{http.MethodPost, "/api/v1/auth/logout", true},
		{http.MethodPut, "/api/v1/auth/me", false},
		{http.MethodPut, "/api/v1/auth/me/preferences", false},
		{http.MethodPost, "/api/v1/auth/switch-tenant", false},
		{http.MethodGet, "/api/v1/knowledge-bases", false},
		{http.MethodPost, "/api/v1/conversation-sync", false},
		{http.MethodPost, "/api/v1/auth/change-password/other", false},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(tc.method, tc.path, nil)
			// Invalid tenant headers and unavailable tenant services cannot
			// bypass the restriction or prevent a password change.
			c.Request.Header.Set("X-Tenant-ID", "invalid")
			require.Equal(t, tc.allowed, authenticateJWTUser(c, nil, nil, nil, user, 42))
			if tc.allowed {
				id, ok := types.UserIDFromContext(c.Request.Context())
				require.True(t, ok)
				require.Equal(t, user.ID, id)
			} else {
				require.True(t, c.IsAborted())
				require.Equal(t, http.StatusForbidden, w.Code)
				require.Contains(t, w.Body.String(), "PASSWORD_CHANGE_REQUIRED")
			}
		})
	}
}
