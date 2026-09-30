package usecase

import (
	"context"
	"strings"
	"testing"

	agentsv1alpha1 "hermeum/hermes-agent-operator/api/v1alpha1"
)

// `distProfile` returns an agent with one "researcher" distribution profile
// resuming the named jobs.
func distProfile(resume ...string) *agentsv1alpha1.HermesAgent {
	ha := minimalHA()
	ha.Spec.Hermes = &agentsv1alpha1.Hermes{
		Profiles: map[string]agentsv1alpha1.HermesProfile{
			"researcher": {Distribution: &agentsv1alpha1.HermesProfileDistribution{
				Source:      "github.com/you/research-bot",
				Ref:         "v1.2.0",
				ResumeCrons: resume,
			}},
		},
	}
	return ha
}

// eventReasons returns the reason of every recorded event, in order.
func eventReasons(kube *fakeSnapshotKube) []string {
	out := make([]string, 0, len(kube.events))
	for _, e := range kube.events {
		out = append(out, e.Reason)
	}
	return out
}

func TestApplyProfileStatus(t *testing.T) {
	ctx := context.Background()

	t.Run("records the distribution and the resumed jobs", func(t *testing.T) {
		kube := &fakeSnapshotKube{}
		uc := NewHermesAgentUseCase(kube, silentTelemetry{})
		ha := distProfile("daily-digest")

		uc.applyProfileStatus(ctx, ha)

		if len(ha.Status.Profiles) != 1 {
			t.Fatalf("expected 1 profile status, got %d", len(ha.Status.Profiles))
		}
		got := ha.Status.Profiles[0]
		if got.Name != "researcher" {
			t.Errorf("name = %q", got.Name)
		}
		if got.Distribution == nil || got.Distribution.Source != "github.com/you/research-bot" || got.Distribution.Ref != "v1.2.0" {
			t.Errorf("distribution = %+v", got.Distribution)
		}
		if len(got.ResumedCrons) != 1 || got.ResumedCrons[0] != "daily-digest" {
			t.Errorf("resumedCrons = %v", got.ResumedCrons)
		}
	})

	t.Run("a profile without a distribution is recorded by name alone", func(t *testing.T) {
		kube := &fakeSnapshotKube{}
		uc := NewHermesAgentUseCase(kube, silentTelemetry{})
		ha := minimalHA()
		ha.Spec.Hermes = &agentsv1alpha1.Hermes{
			Profiles: map[string]agentsv1alpha1.HermesProfile{"plain": {}},
		}

		uc.applyProfileStatus(ctx, ha)

		if len(ha.Status.Profiles) != 1 || ha.Status.Profiles[0].Distribution != nil {
			t.Errorf("unexpected status %+v", ha.Status.Profiles)
		}
		if len(kube.events) != 0 {
			t.Errorf("expected no events, got %v", eventReasons(kube))
		}
	})

	t.Run("newly allowlisted jobs are recorded as events", func(t *testing.T) {
		kube := &fakeSnapshotKube{}
		uc := NewHermesAgentUseCase(kube, silentTelemetry{})
		ha := distProfile("daily-digest", "arxiv-sweep")

		uc.applyProfileStatus(ctx, ha)

		want := []string{eventReasonResumedCronJob, eventReasonResumedCronJob}
		if got := eventReasons(kube); len(got) != len(want) {
			t.Fatalf("expected %v, got %v", want, got)
		}
		if !strings.Contains(kube.events[0].Message, `"daily-digest"`) ||
			!strings.Contains(kube.events[0].Message, `"researcher"`) {
			t.Errorf("event message does not name the job and profile: %q", kube.events[0].Message)
		}
		if kube.events[0].Type != EventTypeNormal {
			t.Errorf("expected a Normal event, got %q", kube.events[0].Type)
		}
	})

	t.Run("an unchanged allowlist records nothing", func(t *testing.T) {
		kube := &fakeSnapshotKube{}
		uc := NewHermesAgentUseCase(kube, silentTelemetry{})
		ha := distProfile("daily-digest")

		uc.applyProfileStatus(ctx, ha)
		before := len(kube.events)
		uc.applyProfileStatus(ctx, ha)

		if len(kube.events) != before {
			t.Errorf("expected no further events on an unchanged allowlist, got %v", eventReasons(kube))
		}
	})

	t.Run("a dropped job is recorded as paused", func(t *testing.T) {
		kube := &fakeSnapshotKube{}
		uc := NewHermesAgentUseCase(kube, silentTelemetry{})
		ha := distProfile("daily-digest", "arxiv-sweep")
		uc.applyProfileStatus(ctx, ha)
		kube.events = nil

		// Drop one name, as reverting the field would.
		ha.Spec.Hermes.Profiles["researcher"].Distribution.ResumeCrons = []string{"daily-digest"}
		uc.applyProfileStatus(ctx, ha)

		if got := eventReasons(kube); len(got) != 1 || got[0] != eventReasonPausedCronJob {
			t.Fatalf("expected one pause event, got %v", got)
		}
		if !strings.Contains(kube.events[0].Message, `"arxiv-sweep"`) {
			t.Errorf("expected the dropped job named, got %q", kube.events[0].Message)
		}
		if len(ha.Status.Profiles[0].ResumedCrons) != 1 {
			t.Errorf("expected the status to follow the spec, got %v", ha.Status.Profiles[0].ResumedCrons)
		}
	})

	t.Run("a removed profile leaves the status without it and records no pause", func(t *testing.T) {
		kube := &fakeSnapshotKube{}
		uc := NewHermesAgentUseCase(kube, silentTelemetry{})
		ha := distProfile("daily-digest")
		uc.applyProfileStatus(ctx, ha)
		kube.events = nil

		// Deleting the profile deletes its jobs with it.
		ha.Spec.Hermes.Profiles = nil
		uc.applyProfileStatus(ctx, ha)

		if len(ha.Status.Profiles) != 0 {
			t.Errorf("expected no profile status, got %+v", ha.Status.Profiles)
		}
		if len(kube.events) != 0 {
			t.Errorf("expected no events for a deleted profile, got %v", eventReasons(kube))
		}
	})

	t.Run("profiles are recorded in name order", func(t *testing.T) {
		kube := &fakeSnapshotKube{}
		uc := NewHermesAgentUseCase(kube, silentTelemetry{})
		ha := minimalHA()
		ha.Spec.Hermes = &agentsv1alpha1.Hermes{
			Profiles: map[string]agentsv1alpha1.HermesProfile{"writer": {}, "coder": {}},
		}

		uc.applyProfileStatus(ctx, ha)

		if len(ha.Status.Profiles) != 2 ||
			ha.Status.Profiles[0].Name != "coder" || ha.Status.Profiles[1].Name != "writer" {
			t.Errorf("expected [coder writer], got %+v", ha.Status.Profiles)
		}
	})
}
