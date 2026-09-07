package usecase

import "context"

// HeartbeatSender emits anonymous heartbeat events to an external service.
// Unlike Telemetry, which collects in-cluster Prometheus metrics, the
// heartbeat reports that this operator installation is running, so the
// number of distinct install IDs sending heartbeats reflects the number of
// operator installations. Implementations must never affect reconciliation;
// errors are swallowed.
type HeartbeatSender interface {
	// SendHeartbeat emits a single heartbeat event. properties may be nil.
	SendHeartbeat(ctx context.Context, event string, properties map[string]any)
	// Close flushes any pending events and releases resources.
	Close()
}

// NoopHeartbeat discards all events. It is used when the heartbeat is
// disabled and in unit tests.
type NoopHeartbeat struct{}

func (NoopHeartbeat) SendHeartbeat(_ context.Context, _ string, _ map[string]any) {}

func (NoopHeartbeat) Close() {}
