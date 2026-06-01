package usecase

import (
	"context"

	agentsv1alpha1 "noahingh/hermes-agent-operator/api/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

func (u *HermesAgentUseCase) reconcileStatus(ctx context.Context, ha *agentsv1alpha1.HermesAgent) error {
	ha.Status.Phase = u.derivePhase(ctx, ha)
	return u.kube.UpdateHermesAgentStatus(ctx, UpdateHermesAgentStatusParam{HermesAgent: ha})
}

func (u *HermesAgentUseCase) derivePhase(ctx context.Context, ha *agentsv1alpha1.HermesAgent) agentsv1alpha1.HermesAgentPhase {
	if ha.IsSuspended() {
		return agentsv1alpha1.PhaseSuspended
	}
	pod, err := u.kube.GetPod(ctx, GetPodParam{
		NamespacedName: types.NamespacedName{Name: ha.Name + "-0", Namespace: ha.Namespace},
	})
	if err != nil {
		return agentsv1alpha1.PhaseUnknown
	}
	if pod == nil {
		return agentsv1alpha1.PhasePending
	}
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
