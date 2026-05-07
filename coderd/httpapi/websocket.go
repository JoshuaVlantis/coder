package httpapi

import (
	"context"
	"errors"
	"net"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/xerrors"

	"cdr.dev/slog/v3"
	"github.com/coder/quartz"
	"github.com/coder/websocket"
)

const HeartbeatInterval time.Duration = 15 * time.Second

type Heartbeater interface {
	HeartbeatClose(ctx context.Context, logger slog.Logger, exit func(), conn *websocket.Conn)
}

// HeartbeatCloser periodically checks websocket connection liveness and closes it if the ping fails.
type HeartbeatCloser struct {
	clk        quartz.Clock
	heartbeats *prometheus.CounterVec
	pathFn     func(context.Context) string
}

func NewHeartbeatCloser(pathFn func(context.Context) string, opts ...func(*HeartbeatCloser)) *HeartbeatCloser {
	hbc := &HeartbeatCloser{
		clk: quartz.NewReal(),
		heartbeats: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "coderd",
			Subsystem: "api",
			Name:      "websocket_heartbeats_total",
			Help:      "Number of websocket heartbeats by path. Compare against coderd_api_concurrent_websockets_total to detect wedged handlers.",
		}, []string{"path"}),
		pathFn: pathFn,
	}
	for _, opt := range opts {
		opt(hbc)
	}
	return hbc
}

func (hc *HeartbeatCloser) inc(ctx context.Context) {
	if hc == nil {
		return
	}
	path := hc.pathFn(ctx)
	if path == "" {
		return
	}
	hc.heartbeats.WithLabelValues(path).Inc()
}

// HeartbeatClose loops to ping a WebSocket to keep it alive.
// It calls `exit` on ping failure.
func (hc *HeartbeatCloser) HeartbeatClose(ctx context.Context, logger slog.Logger, exit func(), conn *websocket.Conn) {
	if hc == nil { // ensure nil-safety
		heartbeatCloseWith(ctx, logger, nil, exit, conn, quartz.NewReal(), HeartbeatInterval)
		return
	}
	heartbeatCloseWith(ctx, logger, hc.inc, exit, conn, hc.clk, HeartbeatInterval)
}

// Collect implements prometheus.Collector.
func (hc *HeartbeatCloser) Collect(ch chan<- prometheus.Metric) {
	if hc == nil {
		return
	}
	hc.heartbeats.Collect(ch)
}

// Describe implements prometheus.Collector.
func (hc *HeartbeatCloser) Describe(ch chan<- *prometheus.Desc) {
	if hc == nil {
		return
	}
	hc.heartbeats.Describe(ch)
}

func heartbeatCloseWith(ctx context.Context, logger slog.Logger, countFn func(context.Context), exit func(), conn *websocket.Conn, clk quartz.Clock, interval time.Duration) {
	ticker := clk.NewTicker(interval, "HeartbeatClose")
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		err := pingWithTimeout(ctx, conn, interval)
		if err != nil {
			// These errors are all expected during normal connection
			// teardown and should not be logged at error level:
			//   - context.DeadlineExceeded: client disconnected
			//     without sending a close frame.
			//   - context.Canceled: request context was canceled.
			//   - net.ErrClosed: connection was already closed by
			//     another goroutine (e.g. handler returned).
			//   - websocket.CloseError: a close frame was
			//     received or sent.
			if errors.Is(err, context.DeadlineExceeded) ||
				errors.Is(err, context.Canceled) ||
				errors.Is(err, net.ErrClosed) ||
				websocket.CloseStatus(err) != -1 {
				logger.Debug(ctx, "heartbeat ping stopped", slog.Error(err))
			} else {
				logger.Error(ctx, "failed to heartbeat ping", slog.Error(err))
			}
			_ = conn.Close(websocket.StatusGoingAway, "Ping failed")
			exit()
			return
		}
		if countFn != nil {
			countFn(ctx)
		}
	}
}

func pingWithTimeout(ctx context.Context, conn *websocket.Conn, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	err := conn.Ping(ctx)
	if err != nil {
		return xerrors.Errorf("failed to ping: %w", err)
	}

	return nil
}
