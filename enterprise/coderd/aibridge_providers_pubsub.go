package coderd

import (
	"context"

	"cdr.dev/slog/v3"
	"github.com/coder/coder/v2/coderd/database/pubsub"
)

// AIBridgeProvidersChangedChannel is the pubsub channel that the
// aibridged daemons in every replica subscribe to in order to
// invalidate their RequestBridge pool when a provider row is
// inserted, updated, or soft-deleted via the API.
//
// Messages have no payload; receivers refresh their state by
// re-querying the database. This keeps the channel agnostic to
// dbcrypt-key changes and avoids bus traffic carrying secrets.
const AIBridgeProvidersChangedChannel = "ai_providers_changed"

// publishAIBridgeProvidersChanged publishes a notification on the
// providers-changed channel. Errors are logged but never returned to
// callers; a missed notification only delays the runtime catching up
// to the new state, and the next mutation will retry.
func publishAIBridgeProvidersChanged(ctx context.Context, ps pubsub.Pubsub, logger slog.Logger) {
	if ps == nil {
		return
	}
	if err := ps.Publish(AIBridgeProvidersChangedChannel, nil); err != nil {
		logger.Warn(ctx, "failed to publish ai_providers_changed",
			slog.Error(err))
	}
}
