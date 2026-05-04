package chatd

import (
	"context"

	"github.com/google/uuid"

	"github.com/coder/coder/v2/coderd/x/chatd/chatloop"
)

// WaitUntilIdleForTest waits for background chat work tracked by the server to
// finish without shutting the server down. Tests use this to assert final
// database state only after asynchronous chat processing has completed.
// Close waits for the same tracked work, but also stops the server.
func WaitUntilIdleForTest(server *Server) {
	server.drainInflight()
}

// ConsumeWakeIfPendingForTest reports whether the server has a queued wake
// signal, consuming it when one is pending.
func ConsumeWakeIfPendingForTest(server *Server) bool {
	select {
	case <-server.wakeCh:
		return true
	default:
		return false
	}
}

// PersistChatContextSummaryForTest exposes summary persistence to black-box
// tests without running a full chat loop.
func PersistChatContextSummaryForTest(
	ctx context.Context,
	server *Server,
	chatID uuid.UUID,
	modelConfigID uuid.UUID,
	toolCallID string,
	result chatloop.CompactionResult,
) error {
	return server.persistChatContextSummary(ctx, chatID, modelConfigID, toolCallID, result)
}
