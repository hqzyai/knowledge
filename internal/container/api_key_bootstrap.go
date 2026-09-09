package container

import (
	"context"
	"fmt"
	"os"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

func initializeHQZYAdminAPIKey(apiKeyService interfaces.TenantAPIKeyService) error {
	ctx := context.Background()
	token := os.Getenv("WEKNORA_BOOTSTRAP_ADMIN_API_KEY")
	if token == "" {
		logger.Info(ctx, "[bootstrap] WEKNORA_BOOTSTRAP_ADMIN_API_KEY is empty; administrator API key initialization skipped")
		return nil
	}
	initialized, err := apiKeyService.InitializeHQZYAdminAPIKey(ctx, token)
	if err != nil {
		return fmt.Errorf("initialize HQZY administrator API key: %w", err)
	}
	if initialized {
		// Never write the token to logs. The workspace Owner can copy it from
		// the existing API integration settings page.
		logger.Info(ctx, "[bootstrap] configured HQZY Admin Full Access API key for workspace 10000")
	}
	return nil
}
