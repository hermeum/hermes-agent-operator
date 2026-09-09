package usecase

import (
	"context"
	"sync"
	"time"
)

const (
	// HeartbeatEvent is emitted periodically so that the number of distinct
	// install IDs sending heartbeats reflects the number of operator installs.
	HeartbeatEvent = "hermes_agent_heartbeat"

	// HeartbeatInterval is how often the heartbeat is reported.
	HeartbeatInterval = time.Hour

	// PropertyOperatorVersion is the operator version.
	PropertyOperatorVersion = "operator_version"
)

// HeartbeatReporter periodically emits an anonymous heartbeat so that the
// number of running operator installations can be counted. Each installation
// sends heartbeats under its own anonymous install ID, so the distinct
// number of senders equals the number of installs. It is safe for
// concurrent use.
type HeartbeatReporter struct {
	sender  HeartbeatSender
	version string

	ticker *time.Ticker
	done   chan struct{}
	once   sync.Once
}

// NewHeartbeatReporter creates a reporter emitting heartbeats to sender.
// version is reported as the operator version on every event. The heartbeat
// goroutine must be stopped with Close.
func NewHeartbeatReporter(sender HeartbeatSender, version string) *HeartbeatReporter {
	r := &HeartbeatReporter{
		sender:  sender,
		version: version,
		ticker:  time.NewTicker(HeartbeatInterval),
		done:    make(chan struct{}),
	}
	go r.run()
	return r
}

// Close stops the heartbeat and flushes pending events.
func (r *HeartbeatReporter) Close() {
	r.once.Do(func() {
		r.ticker.Stop()
		close(r.done)
		r.sender.Close()
	})
}

func (r *HeartbeatReporter) run() {
	for {
		select {
		case <-r.done:
			return
		case <-r.ticker.C:
			r.sender.SendHeartbeat(context.Background(), HeartbeatEvent, map[string]any{
				PropertyOperatorVersion: r.version,
			})
		}
	}
}
