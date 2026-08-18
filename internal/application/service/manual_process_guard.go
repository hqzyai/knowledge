package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/google/uuid"
)

var ErrManualProcessConcurrent = errors.New("manual knowledge processing is already running")

const (
	manualProcessGuardTTL            = 31 * time.Minute
	manualProcessGuardReleaseTimeout = 2 * time.Second
)

var manualProcessInflight sync.Map // knowledge ID -> struct{}

// acquireManualProcessGuard serializes destructive re-indexes for one manual
// document. Different documents/users remain fully parallel. Redis provides
// cross-replica coordination; Lite mode falls back to a process-local guard.
func (s *knowledgeService) acquireManualProcessGuard(
	ctx context.Context, tenantID uint64, knowledgeID string,
) (func(), error) {
	key := fmt.Sprintf("manual:process:%d:%s", tenantID, knowledgeID)
	if s.redisClient != nil {
		token := uuid.NewString()
		acquired, err := s.redisClient.SetNX(ctx, key, token, manualProcessGuardTTL).Result()
		switch {
		case err != nil:
			logger.Warnf(ctx, "manual process guard unavailable, using local fallback: %v", err)
		case !acquired:
			return nil, ErrManualProcessConcurrent
		default:
			return func() {
				releaseCtx, cancel := context.WithTimeout(
					context.WithoutCancel(ctx), manualProcessGuardReleaseTimeout)
				defer cancel()
				const compareAndDelete = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0`
				if err := s.redisClient.Eval(releaseCtx, compareAndDelete, []string{key}, token).Err(); err != nil {
					logger.Warnf(releaseCtx, "failed to release manual process guard %s: %v", key, err)
				}
			}, nil
		}
	}

	if _, loaded := manualProcessInflight.LoadOrStore(key, struct{}{}); loaded {
		return nil, ErrManualProcessConcurrent
	}
	return func() { manualProcessInflight.Delete(key) }, nil
}
