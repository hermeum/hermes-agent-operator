package usecase

import (
	"context"
	"errors"
	"testing"

	agentsv1alpha1 "hermeum/hermes-agent-operator/api/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestHermesAgentPhase(t *testing.T) {
	tests := []struct {
		name string
		pod  corev1.PodPhase
		want agentsv1alpha1.HermesAgentPhase
	}{
		{"pending", corev1.PodPending, agentsv1alpha1.PhasePending},
		{"running", corev1.PodRunning, agentsv1alpha1.PhaseRunning},
		{"succeeded", corev1.PodSucceeded, agentsv1alpha1.PhaseSucceeded},
		{"failed", corev1.PodFailed, agentsv1alpha1.PhaseFailed},
		{"empty phase is unknown", "", agentsv1alpha1.PhaseUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := &corev1.Pod{Status: corev1.PodStatus{Phase: tt.pod}}
			if got := hermesAgentPhase(pod); got != tt.want {
				t.Errorf("hermesAgentPhase() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFirstPodFailure(t *testing.T) {
	terminated := func(name string, exitCode int32, reason, message string) corev1.ContainerStatus {
		return corev1.ContainerStatus{
			Name: name,
			State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
				ExitCode: exitCode, Reason: reason, Message: message,
			}},
		}
	}
	waiting := func(name, reason, message string) corev1.ContainerStatus {
		return corev1.ContainerStatus{
			Name:  name,
			State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: reason, Message: message}},
		}
	}

	tests := []struct {
		name string
		pod  *corev1.Pod
		want *podFailure
	}{
		{
			name: "running pod without failures",
			pod: &corev1.Pod{Status: corev1.PodStatus{
				Phase:             corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{{Name: hermesContainerName}},
			}},
			want: nil,
		},
		{
			name: "pending pod that cannot be scheduled",
			pod: &corev1.Pod{Status: corev1.PodStatus{
				Phase: corev1.PodPending,
				Conditions: []corev1.PodCondition{{
					Type:    corev1.PodScheduled,
					Status:  corev1.ConditionFalse,
					Reason:  "Unschedulable",
					Message: "0/3 nodes are available",
				}},
			}},
			want: &podFailure{Reason: "Unschedulable", Message: "0/3 nodes are available"},
		},
		{
			name: "pending pod with a waiting init container",
			pod: &corev1.Pod{Status: corev1.PodStatus{
				Phase:                 corev1.PodPending,
				InitContainerStatuses: []corev1.ContainerStatus{waiting("init-hermes", "ImagePullBackOff", "pull denied")},
			}},
			want: &podFailure{Container: "init-hermes", Init: true, Reason: "ImagePullBackOff", Message: "pull denied"},
		},
		{
			name: "pending pod with nothing to report",
			pod: &corev1.Pod{Status: corev1.PodStatus{
				Phase:      corev1.PodPending,
				Conditions: []corev1.PodCondition{{Type: corev1.PodScheduled, Status: corev1.ConditionTrue}},
			}},
			want: nil,
		},
		{
			name: "failed init container reports its termination message",
			pod: &corev1.Pod{Status: corev1.PodStatus{
				Phase: corev1.PodFailed,
				InitContainerStatuses: []corev1.ContainerStatus{
					terminated("init-hermes", 0, "Completed", ""),
					terminated("init-profile-coder", 1, "Error", "distribution requires Hermes >=0.13.0"),
				},
			}},
			want: &podFailure{
				Container: "init-profile-coder", Init: true,
				Reason: "Error", Message: "distribution requires Hermes >=0.13.0",
			},
		},
		{
			name: "init container failure wins over a container failure",
			pod: &corev1.Pod{Status: corev1.PodStatus{
				Phase:                 corev1.PodRunning,
				InitContainerStatuses: []corev1.ContainerStatus{terminated("init-hermes", 2, "Error", "boom")},
				ContainerStatuses:     []corev1.ContainerStatus{waiting(hermesContainerName, "CrashLoopBackOff", "")},
			}},
			want: &podFailure{Container: "init-hermes", Init: true, Reason: "Error", Message: "boom"},
		},
		{
			name: "waiting container",
			pod: &corev1.Pod{Status: corev1.PodStatus{
				Phase:             corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{waiting(hermesContainerName, "CrashLoopBackOff", "back-off 5m0s")},
			}},
			want: &podFailure{Container: hermesContainerName, Reason: "CrashLoopBackOff", Message: "back-off 5m0s"},
		},
		{
			name: "container that last terminated abnormally",
			pod: &corev1.Pod{Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{{
					Name: hermesContainerName,
					LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
						ExitCode: 137, Reason: "OOMKilled",
					}},
				}},
			}},
			want: &podFailure{Container: hermesContainerName, Reason: "OOMKilled"},
		},
		{
			name: "pod level reason as a last resort",
			pod: &corev1.Pod{Status: corev1.PodStatus{
				Phase:  corev1.PodFailed,
				Reason: "Evicted",
			}},
			want: &podFailure{Reason: "Evicted"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := firstPodFailure(tt.pod)
			switch {
			case tt.want == nil && got != nil:
				t.Fatalf("firstPodFailure() = %+v, want nil", got)
			case tt.want != nil && got == nil:
				t.Fatalf("firstPodFailure() = nil, want %+v", tt.want)
			case tt.want != nil && *got != *tt.want:
				t.Errorf("firstPodFailure() = %+v, want %+v", *got, *tt.want)
			}

			// `status.reason` carries the failure's reason, and is empty when
			// there is no failure to report.
			wantReason := ""
			if tt.want != nil {
				wantReason = tt.want.Reason
			}
			if got := got.GetReason(); got != wantReason {
				t.Errorf("GetReason() = %q, want %q", got, wantReason)
			}
		})
	}
}

func TestFirstPodFailureWithoutAPod(t *testing.T) {
	// `deriveStatus` reports no `Pod` in three cases, each covered by
	// `TestDeriveStatus`.
	if got := firstPodFailure(nil); got != nil {
		t.Errorf("firstPodFailure(nil) = %+v, want nil", got)
	}
}

// podKube serves one `Pod`, or an error, and records the name asked for.
type podKube struct {
	fakeSnapshotKube
	pod       *corev1.Pod
	err       error
	requested types.NamespacedName
}

func (f *podKube) GetPod(ctx context.Context, param GetPodParam) (*corev1.Pod, error) {
	f.requested = param.NamespacedName
	if f.err != nil {
		return nil, f.err
	}
	return f.pod, nil
}

func TestDeriveStatus(t *testing.T) {
	ctx := context.Background()
	running := &corev1.Pod{Status: corev1.PodStatus{Phase: corev1.PodRunning}}

	t.Run("a suspended agent is not inspected", func(t *testing.T) {
		kube := &podKube{pod: running}
		uc := NewHermesAgentUseCase(kube, silentTelemetry{})
		ha := minimalHA()
		suspend := true
		ha.Spec.Suspend = &suspend

		phase, pod := uc.deriveStatus(ctx, ha)
		if phase != agentsv1alpha1.PhaseSuspended {
			t.Errorf("phase = %q, want %q", phase, agentsv1alpha1.PhaseSuspended)
		}
		if pod != nil {
			t.Errorf("expected no Pod, got %+v", pod.Status)
		}
		if kube.requested.Name != "" {
			t.Errorf("a suspended agent must not be looked up, got %q", kube.requested.Name)
		}
	})

	t.Run("a Pod that cannot be read reports Unknown", func(t *testing.T) {
		kube := &podKube{err: errors.New("forbidden")}
		uc := NewHermesAgentUseCase(kube, silentTelemetry{})

		phase, pod := uc.deriveStatus(ctx, minimalHA())
		if phase != agentsv1alpha1.PhaseUnknown {
			t.Errorf("phase = %q, want %q", phase, agentsv1alpha1.PhaseUnknown)
		}
		if pod != nil {
			t.Errorf("expected no Pod, got %+v", pod.Status)
		}
	})

	t.Run("a Pod that does not exist yet reports Pending", func(t *testing.T) {
		kube := &podKube{}
		uc := NewHermesAgentUseCase(kube, silentTelemetry{})

		phase, pod := uc.deriveStatus(ctx, minimalHA())
		if phase != agentsv1alpha1.PhasePending {
			t.Errorf("phase = %q, want %q", phase, agentsv1alpha1.PhasePending)
		}
		if pod != nil {
			t.Errorf("expected no Pod, got %+v", pod.Status)
		}
	})

	t.Run("an existing Pod is reported alongside its phase", func(t *testing.T) {
		kube := &podKube{pod: running}
		uc := NewHermesAgentUseCase(kube, silentTelemetry{})

		phase, pod := uc.deriveStatus(ctx, minimalHA())
		if phase != agentsv1alpha1.PhaseRunning {
			t.Errorf("phase = %q, want %q", phase, agentsv1alpha1.PhaseRunning)
		}
		// The caller reads this Pod instead of fetching it again, so it has to
		// be the one that was read.
		if pod != running {
			t.Errorf("expected the Pod that was read, got %+v", pod)
		}
		want := types.NamespacedName{Name: "test-0", Namespace: "default"}
		if kube.requested != want {
			t.Errorf("looked up %v, want %v", kube.requested, want)
		}
	})
}

func TestPodFailureGetReasonIsNilSafe(t *testing.T) {
	var f *podFailure
	if got := f.GetReason(); got != "" {
		t.Errorf("GetReason() = %q, want empty", got)
	}
}
