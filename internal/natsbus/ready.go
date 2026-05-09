package natsbus

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

// ErrReadyTimeout is returned by ReadyWaiter.Wait when the agent does not
// signal readiness within the configured timeout. Callers typically log
// and proceed (publishing to the input topic anyway).
var ErrReadyTimeout = errors.New("agent ready timeout")

// ReadyWaiter blocks the caller until an agent container is ready to
// receive input on its NATS subjects. It is constructed before the
// container is started and resolves once the readiness condition is met.
//
// Callers MUST Close the waiter when done (defer is fine).
type ReadyWaiter struct {
	bus           *Bus
	agentID       string
	clientsBefore int
}

// PrepareReadyWaiter snapshots the bus state needed to detect when the
// agent identified by agentID becomes ready. Must be called BEFORE the
// container is started.
func PrepareReadyWaiter(bus *Bus, _ *Client, agentID string) (*ReadyWaiter, error) {
	return &ReadyWaiter{
		bus:           bus,
		agentID:       agentID,
		clientsBefore: bus.NumClients(),
	}, nil
}

// Wait blocks until the agent is ready, the timeout elapses, or ctx is
// cancelled. Returns nil on success, ErrReadyTimeout on timeout, or
// ctx.Err() on cancellation.
func (w *ReadyWaiter) Wait(ctx context.Context, timeout time.Duration) error {
	deadline := time.After(timeout)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			slog.Warn("agent ready timeout", "agent", w.agentID)
			return ErrReadyTimeout
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if w.bus.NumClients() > w.clientsBefore {
				// Grace period for the new client to register subscriptions.
				time.Sleep(500 * time.Millisecond)
				slog.Info("agent container ready", "agent", w.agentID)
				return nil
			}
		}
	}
}

// Close releases resources held by the waiter. Safe to call multiple times.
func (w *ReadyWaiter) Close() {}
