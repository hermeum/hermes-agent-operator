package infras

import (
	"context"
	"time"

	"github.com/posthog/posthog-go"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// postHogAPIKey is the PostHog project API key the anonymous deployment
	// heartbeat is reported to.
	postHogAPIKey = "phc_z2nKsNVLSSSYbszJYmxzJZY2FZw4qBWnnrqNKM4vTbgZ"
	// postHogHost is the PostHog endpoint (US region).
	postHogHost = "https://us.i.posthog.com"
	// deploymentIDGroupName is the PostHog group type under which events are
	// aggregated.
	deploymentIDGroupName = "deployment"
)

// PostHogHeartbeat reports anonymous heartbeat events to a PostHog project.
// Events are batched by the underlying client and flushed on Close. Any
// error while sending is logged and swallowed so the heartbeat can never
// break reconciliation.
type PostHogHeartbeat struct {
	client       posthog.Client
	deploymentID string
}

// NewPostHogHeartbeat creates a PostHog-backed HeartbeatSender. deploymentID
// is the anonymous identifier of this controller run, generated at startup;
// every event is attributed to the "deployment" PostHog group carrying that
// identifier.
func NewPostHogHeartbeat(deploymentID string) (*PostHogHeartbeat, error) {
	cl, err := posthog.NewWithConfig(postHogAPIKey, posthog.Config{
		Endpoint:  postHogHost,
		Interval:  30 * time.Second,
		BatchSize: 100,
	})
	if err != nil {
		return nil, err
	}
	return &PostHogHeartbeat{client: cl, deploymentID: deploymentID}, nil
}

func (p *PostHogHeartbeat) SendHeartbeat(ctx context.Context, event string, properties map[string]any) {
	if properties == nil {
		properties = map[string]any{}
	}
	properties["$groups"] = map[string]string{deploymentIDGroupName: p.deploymentID}
	err := p.client.Enqueue(posthog.Capture{
		DistinctId: p.deploymentID,
		Event:      event,
		Properties: properties,
		Timestamp:  time.Now(),
	})
	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to enqueue heartbeat event", "event", event)
	}
}

func (p *PostHogHeartbeat) Close() {
	// Enqueue errors are already logged in SendHeartbeat; flushing is
	// best-effort.
	_ = p.client.Close()
}
