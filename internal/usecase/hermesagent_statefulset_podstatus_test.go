package usecase

import (
	"context"
	"errors"
	"testing"

	agentsv1alpha1 "hermeum/hermes-agent-operator/api/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
				Reason: reasonError, Message: "distribution requires Hermes >=0.13.0",
			},
		},
		{
			name: "init container failure wins over a container failure",
			pod: &corev1.Pod{Status: corev1.PodStatus{
				Phase:                 corev1.PodRunning,
				InitContainerStatuses: []corev1.ContainerStatus{terminated("init-hermes", 2, "Error", "boom")},
				ContainerStatuses:     []corev1.ContainerStatus{waiting(hermesContainerName, "CrashLoopBackOff", "")},
			}},
			want: &podFailure{Container: "init-hermes", Init: true, Reason: reasonError, Message: "boom"},
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

// reasonError is the reason kubelet reports for a container that exited
// non-zero, and the fallback this package supplies when kubelet reports none.
const reasonError = "Error"

// `initPod` returns a `Pod` in the phase kubelet reports while init containers
// are still being worked through: Pending, scheduled, with the init container
// statuses given.
func initPod(statuses ...corev1.ContainerStatus) *corev1.Pod {
	return &corev1.Pod{Status: corev1.PodStatus{
		Phase:                 corev1.PodPending,
		Conditions:            []corev1.PodCondition{{Type: corev1.PodScheduled, Status: corev1.ConditionTrue}},
		InitContainerStatuses: statuses,
	}}
}

func TestInitContainerFailure(t *testing.T) {
	running := corev1.ContainerStatus{
		Name:  "init-hermes",
		State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
	}
	completed := corev1.ContainerStatus{
		Name: "init-hermes",
		State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
			ExitCode: 0, Reason: "Completed",
		}},
	}
	waitingTurn := corev1.ContainerStatus{
		Name:  "init-profile-coder",
		State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "PodInitializing"}},
	}

	tests := []struct {
		name string
		pod  *corev1.Pod
		want *podFailure
	}{
		{
			name: "no pod",
			pod:  nil,
			want: nil,
		},
		{
			// The regression this function exists for: a `StatefulSet` `Pod` stays
			// Pending for as long as its init containers run, so a healthy
			// start must not be read as a failure.
			name: "a healthy start is not a failure",
			pod:  initPod(running, waitingTurn),
			want: nil,
		},
		{
			name: "nothing has started yet",
			pod: initPod(corev1.ContainerStatus{
				Name:  "init-hermes",
				State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ContainerCreating"}},
			}),
			want: nil,
		},
		{
			name: "every init container has completed",
			pod:  initPod(completed),
			want: nil,
		},
		{
			// The failure this feature exists to report, in the phase it
			// actually occurs in.
			name: "a step that ran and failed, while the pod is still Pending",
			pod: initPod(completed, corev1.ContainerStatus{
				Name: "init-profile-coder",
				State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
					ExitCode: 1, Reason: "Error",
					Message: "Profile coder: distribution requires Hermes >=0.13.0",
				}},
			}),
			want: &podFailure{
				Container: "init-profile-coder", Init: true, Reason: reasonError,
				Message: "Profile coder: distribution requires Hermes >=0.13.0",
			},
		},
		{
			name: "an exit without a reason still reports one",
			pod: initPod(corev1.ContainerStatus{
				Name: "init-hermes",
				State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
					ExitCode: 2, Message: "boom",
				}},
			}),
			want: &podFailure{Container: "init-hermes", Init: true, Reason: reasonError, Message: "boom"},
		},
		{
			// While kubelet backs off, the step's own output has moved to
			// `LastTerminationState`; the back-off text is not a diagnosis.
			name: "a backing-off step reports its last output, not the back-off text",
			pod: initPod(corev1.ContainerStatus{
				Name: "init-profile-coder",
				State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{
					Reason: "CrashLoopBackOff", Message: "back-off 10s restarting failed container",
				}},
				LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
					ExitCode: 1, Reason: reasonError, Message: "Profile coder: could not fetch ref v9.9.9",
				}},
			}),
			want: &podFailure{
				Container: "init-profile-coder", Init: true, Reason: "CrashLoopBackOff",
				Message: "Profile coder: could not fetch ref v9.9.9",
			},
		},
		{
			name: "an image that cannot be pulled",
			pod: initPod(corev1.ContainerStatus{
				Name: "init-hermes",
				State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{
					Reason: "ImagePullBackOff", Message: "pull access denied",
				}},
			}),
			want: &podFailure{
				Container: "init-hermes", Init: true,
				Reason: "ImagePullBackOff", Message: "pull access denied",
			},
		},
		{
			name: "a failure in the agent container is not an init failure",
			pod: &corev1.Pod{Status: corev1.PodStatus{
				Phase:                 corev1.PodRunning,
				InitContainerStatuses: []corev1.ContainerStatus{completed},
				ContainerStatuses: []corev1.ContainerStatus{{
					Name:  hermesContainerName,
					State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}},
				}},
			}},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := initContainerFailure(tt.pod)
			switch {
			case tt.want == nil && got != nil:
				t.Fatalf("initContainerFailure() = %+v, want nil", got)
			case tt.want != nil && got == nil:
				t.Fatalf("initContainerFailure() = nil, want %+v", tt.want)
			case tt.want != nil && *got != *tt.want:
				t.Errorf("initContainerFailure() = %+v, want %+v", *got, *tt.want)
			}
		})
	}
}

func TestPodStatusReason(t *testing.T) {
	failedStep := initPod(
		corev1.ContainerStatus{Name: "init-hermes", State: corev1.ContainerState{
			Terminated: &corev1.ContainerStateTerminated{ExitCode: 0, Reason: "Completed"}}},
		corev1.ContainerStatus{Name: "init-profile-coder", State: corev1.ContainerState{
			Terminated: &corev1.ContainerStateTerminated{ExitCode: 1, Reason: reasonError, Message: "boom"}}},
		corev1.ContainerStatus{Name: "init-extra", State: corev1.ContainerState{
			Waiting: &corev1.ContainerStateWaiting{Reason: "PodInitializing"}}},
	)
	// Without the init failure taking precedence this reports `PodInitializing`,
	// which says nothing about the step that failed.
	if got := podStatusReason(failedStep, initContainerFailure(failedStep)); got != reasonError {
		t.Errorf("podStatusReason() = %q, want Error", got)
	}

	// A healthy start still reports why the `Pod` is pending.
	starting := initPod(corev1.ContainerStatus{Name: "init-hermes", State: corev1.ContainerState{
		Waiting: &corev1.ContainerStateWaiting{Reason: "PodInitializing"}}})
	if got := podStatusReason(starting, initContainerFailure(starting)); got != "PodInitializing" {
		t.Errorf("podStatusReason() = %q, want PodInitializing", got)
	}

	// A `Pod`-level failure with no init failure is unaffected.
	unschedulable := &corev1.Pod{Status: corev1.PodStatus{
		Phase: corev1.PodPending,
		Conditions: []corev1.PodCondition{{
			Type: corev1.PodScheduled, Status: corev1.ConditionFalse, Reason: "Unschedulable",
		}},
	}}
	if got := podStatusReason(unschedulable, initContainerFailure(unschedulable)); got != "Unschedulable" {
		t.Errorf("podStatusReason() = %q, want Unschedulable", got)
	}

	// An init failure wins over a `Pod` level failure reported at the same
	// time: the step that failed is the more useful diagnosis, and a `Pod` can
	// carry both after it is rescheduled.
	bothFailures := &corev1.Pod{Status: corev1.PodStatus{
		Phase: corev1.PodPending,
		Conditions: []corev1.PodCondition{{
			Type: corev1.PodScheduled, Status: corev1.ConditionFalse, Reason: "Unschedulable",
		}},
		InitContainerStatuses: []corev1.ContainerStatus{{
			Name: "init-profile-coder",
			State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
				ExitCode: 1, Reason: reasonError, Message: "boom",
			}},
		}},
	}}
	if got := podStatusReason(bothFailures, initContainerFailure(bothFailures)); got != reasonError {
		t.Errorf("podStatusReason() = %q, want Error", got)
	}

	if got := podStatusReason(nil, nil); got != "" {
		t.Errorf("podStatusReason(nil, nil) = %q, want empty", got)
	}
}

// TestApplyInitFailedConditionDuringHealthyStart is the regression guard: an
// agent starting normally must not report InitFailed at any point.
func TestApplyInitFailedConditionDuringHealthyStart(t *testing.T) {
	uc := NewHermesAgentUseCase(&fakeSnapshotKube{}, silentTelemetry{})
	ha := minimalHA()

	starting := initPod(
		corev1.ContainerStatus{Name: "init-hermes", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
		corev1.ContainerStatus{Name: "init-profile-coder", State: corev1.ContainerState{
			Waiting: &corev1.ContainerStateWaiting{Reason: "PodInitializing"}}},
	)
	if uc.applyInitFailedCondition(context.Background(), ha, initContainerFailure(starting)) {
		t.Error("a healthy start must not change the status")
	}
	if cond := initFailedCondition(ha); cond != nil {
		t.Errorf("a healthy start must not set InitFailed, got %+v", cond)
	}
}

// initFailedCondition returns the InitFailed condition of the agent, or nil.
func initFailedCondition(ha *agentsv1alpha1.HermesAgent) *metav1.Condition {
	return meta.FindStatusCondition(ha.Status.Conditions, string(agentsv1alpha1.ConditionInitFailed))
}

func TestApplyInitFailedCondition(t *testing.T) {
	ctx := context.Background()
	uc := NewHermesAgentUseCase(&fakeSnapshotKube{}, silentTelemetry{})

	t.Run("init container failure sets the condition", func(t *testing.T) {
		ha := minimalHA()
		if !uc.applyInitFailedCondition(ctx, ha, &podFailure{
			Container: "init-profile-coder", Init: true,
			Reason: "Error", Message: "distribution requires Hermes >=0.13.0",
		}) {
			t.Error("expected the first set to report a change")
		}

		cond := initFailedCondition(ha)
		if cond == nil {
			t.Fatal("expected InitFailed condition")
		}
		if cond.Status != metav1.ConditionTrue {
			t.Errorf("expected InitFailed=True, got %s", cond.Status)
		}
		if cond.Reason != "Error" {
			t.Errorf("expected reason Error, got %s", cond.Reason)
		}
		want := "Init container init-profile-coder failed: distribution requires Hermes >=0.13.0"
		if cond.Message != want {
			t.Errorf("expected message %q, got %q", want, cond.Message)
		}
		if cond.ObservedGeneration != ha.Generation {
			t.Errorf("expected ObservedGeneration %d, got %d", ha.Generation, cond.ObservedGeneration)
		}
	})

	t.Run("a failure without a message names the container alone", func(t *testing.T) {
		ha := minimalHA()
		uc.applyInitFailedCondition(ctx, ha, &podFailure{
			Container: "init-hermes", Init: true, Reason: "ImagePullBackOff",
		})
		if got := initFailedCondition(ha).Message; got != "Init container init-hermes failed" {
			t.Errorf("unexpected message %q", got)
		}
	})

	t.Run("repeated identical failures report no change", func(t *testing.T) {
		ha := minimalHA()
		f := &podFailure{Container: "init-hermes", Init: true, Reason: "Error", Message: "boom"}
		uc.applyInitFailedCondition(ctx, ha, f)
		if uc.applyInitFailedCondition(ctx, ha, f) {
			t.Error("identical failure must report unchanged, to avoid a redundant status write")
		}
	})

	t.Run("a container failure leaves the condition unset", func(t *testing.T) {
		ha := minimalHA()
		if uc.applyInitFailedCondition(ctx, ha, &podFailure{
			Container: hermesContainerName, Reason: "CrashLoopBackOff",
		}) {
			t.Error("expected no change for a failure outside an init container")
		}
		if initFailedCondition(ha) != nil {
			t.Error("InitFailed must not be set for a failure outside an init container")
		}
	})

	t.Run("recovery clears the condition", func(t *testing.T) {
		ha := minimalHA()
		uc.applyInitFailedCondition(ctx, ha, &podFailure{Container: "init-hermes", Init: true, Reason: "Error"})
		if !uc.applyInitFailedCondition(ctx, ha, nil) {
			t.Error("expected clearing to report a change")
		}
		if initFailedCondition(ha) != nil {
			t.Error("expected the InitFailed condition to be removed")
		}
		if uc.applyInitFailedCondition(ctx, ha, nil) {
			t.Error("clearing an absent condition must report unchanged")
		}
	})
}
