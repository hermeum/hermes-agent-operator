package usecase

import (
	"context"

	agentsv1alpha1 "hermeum/hermes-agent-operator/api/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// `podFailure` describes the first failure found in a `Pod`'s status, with
// enough detail to name the container that failed and why.
type podFailure struct {
	// Container is the name of the container the failure was read from.  It is
	// empty for a `Pod` level failure, such as a scheduling failure.
	Container string
	// Init reports whether Container is an init container.
	Init bool
	// Reason is the short, CamelCase code Kubernetes reported (e.g. "Error",
	// "CrashLoopBackOff", "OOMKilled", "Unschedulable").  Never empty.
	Reason string
	// Message is the human-readable detail Kubernetes reported, which for a
	// terminated container is its termination message.  Often empty.
	Message string
}

func hermesAgentPhase(pod *corev1.Pod) agentsv1alpha1.HermesAgentPhase {
	switch pod.Status.Phase {
	case corev1.PodPending:
		return agentsv1alpha1.PhasePending
	case corev1.PodRunning:
		return agentsv1alpha1.PhaseRunning
	case corev1.PodSucceeded:
		return agentsv1alpha1.PhaseSucceeded
	case corev1.PodFailed:
		return agentsv1alpha1.PhaseFailed
	default:
		return agentsv1alpha1.PhaseUnknown
	}
}

// `firstPodFailure` returns the first failure the `Pod` reports, or nil when it
// reports none and when there is no `Pod`.  A pending `Pod` is diagnosed from
// its scheduling condition and its waiting init containers, since a container
// that never started has no termination state to read.  Any other `Pod` is
// diagnosed from its init containers first, then its containers, so the
// earliest step to fail is the one reported.
func firstPodFailure(pod *corev1.Pod) *podFailure {
	if pod == nil {
		return nil
	}

	if pod.Status.Phase == corev1.PodPending {
		for _, c := range pod.Status.Conditions {
			if c.Type == corev1.PodScheduled && c.Status == corev1.ConditionFalse && c.Reason != "" {
				return &podFailure{Reason: c.Reason, Message: c.Message}
			}
		}
		for _, cs := range pod.Status.InitContainerStatuses {
			if w := cs.State.Waiting; w != nil && w.Reason != "" {
				return &podFailure{Container: cs.Name, Init: true, Reason: w.Reason, Message: w.Message}
			}
		}
		return nil
	}

	for _, cs := range pod.Status.InitContainerStatuses {
		if t := cs.State.Terminated; t != nil && t.ExitCode != 0 && t.Reason != "" {
			return &podFailure{Container: cs.Name, Init: true, Reason: t.Reason, Message: t.Message}
		}
		if w := cs.State.Waiting; w != nil && w.Reason != "" {
			return &podFailure{Container: cs.Name, Init: true, Reason: w.Reason, Message: w.Message}
		}
	}

	for _, cs := range pod.Status.ContainerStatuses {
		if w := cs.State.Waiting; w != nil && w.Reason != "" {
			return &podFailure{Container: cs.Name, Reason: w.Reason, Message: w.Message}
		}
		if t := cs.LastTerminationState.Terminated; t != nil && t.ExitCode != 0 && t.Reason != "" {
			return &podFailure{Container: cs.Name, Reason: t.Reason, Message: t.Message}
		}
	}

	if pod.Status.Reason != "" {
		return &podFailure{Reason: pod.Status.Reason}
	}
	return nil
}

// `GetReason` returns the short, CamelCase code for `status.reason`, which is
// empty when there is no failure to report.
func (f *podFailure) GetReason() string {
	if f == nil {
		return ""
	}
	return f.Reason
}

// `startingUpWaitingReasons` are the Waiting reasons an init container reports
// while it is simply waiting its turn, rather than failing.  Every other
// Waiting reason describes something that went wrong: the image could not be
// pulled, the container could not be created, or it has already failed and
// kubelet is backing off before the next attempt.
var startingUpWaitingReasons = map[string]bool{
	"PodInitializing":   true,
	"ContainerCreating": true,
}

// `initContainerFailure` returns the first init container failure the `Pod`
// reports, or nil when it reports none.
//
// This deliberately does not go through `firstPodFailure`.  A `Pod` whose init
// containers are still running stays in phase Pending for as long as that
// takes, so a diagnosis that keys off the phase cannot tell "still starting"
// from "the first step failed": every init container that has not had its turn
// yet reports Waiting with `PodInitializing`, and an init container that ran
// and exited non-zero reports Terminated while the `Pod` is still Pending.
//
// Once kubelet starts backing off, a failed init container reports Waiting with
// `CrashLoopBackOff` and its own output moves to `LastTerminationState`, so the
// message is read from there in preference to kubelet's back-off text.
func initContainerFailure(pod *corev1.Pod) *podFailure {
	if pod == nil {
		return nil
	}

	for _, cs := range pod.Status.InitContainerStatuses {
		if t := cs.State.Terminated; t != nil && t.ExitCode != 0 {
			return &podFailure{
				Container: cs.Name, Init: true,
				Reason: terminationReason(t), Message: t.Message,
			}
		}
		w := cs.State.Waiting
		if w == nil || w.Reason == "" || startingUpWaitingReasons[w.Reason] {
			continue
		}
		message := w.Message
		if last := cs.LastTerminationState.Terminated; last != nil && last.Message != "" {
			message = last.Message
		}
		return &podFailure{Container: cs.Name, Init: true, Reason: w.Reason, Message: message}
	}

	return nil
}

// `terminationReason` returns the reason to report for a container that exited,
// which Kubernetes leaves empty often enough to need a fallback: a condition's
// reason has to be a non-empty CamelCase code.
func terminationReason(t *corev1.ContainerStateTerminated) string {
	if t.Reason != "" {
		return t.Reason
	}
	return "Error"
}

// `podStatusReason` returns the code for `status.reason`.  An init container
// failure takes precedence over the `Pod`'s own first failure.  A `Pod` stays
// Pending while its init containers are worked through, so the `Pod` level
// diagnosis of a `Pod` whose first step is failing is "PodInitializing", which
// describes the `Pod` accurately and the problem not at all.
func podStatusReason(pod *corev1.Pod, initFailure *podFailure) string {
	if reason := initFailure.GetReason(); reason != "" {
		return reason
	}
	return firstPodFailure(pod).GetReason()
}

// `applyInitFailedCondition` sets the InitFailed condition when failure is an
// init container failure, and clears it otherwise.  It reports whether the
// status changed.
func (u *HermesAgentUseCase) applyInitFailedCondition(ctx context.Context, ha *agentsv1alpha1.HermesAgent, failure *podFailure) bool {
	if failure == nil || !failure.Init {
		changed := meta.RemoveStatusCondition(&ha.Status.Conditions, string(agentsv1alpha1.ConditionInitFailed))
		if changed {
			u.tel.Info(ctx, "Cleared InitFailed condition for HermesAgent")
		}
		return changed
	}

	message := "Init container " + failure.Container + " failed"
	if failure.Message != "" {
		message += ": " + failure.Message
	}
	changed := meta.SetStatusCondition(&ha.Status.Conditions, metav1.Condition{
		Type:               string(agentsv1alpha1.ConditionInitFailed),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: ha.Generation,
		Reason:             failure.Reason,
		Message:            message,
	})
	if changed {
		u.tel.Info(ctx, "Init container failed for HermesAgent",
			"container", failure.Container, "reason", failure.Reason)
	}
	return changed
}
