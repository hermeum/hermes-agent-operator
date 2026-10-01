package usecase

import (
	agentsv1alpha1 "hermeum/hermes-agent-operator/api/v1alpha1"

	corev1 "k8s.io/api/core/v1"
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
