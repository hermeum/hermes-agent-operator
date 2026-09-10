package usecase

import (
	"context"

	agentsv1alpha1 "hermeum/hermes-agent-operator/api/v1alpha1"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Reconciliation reasons for the Ready condition, one per resource reconcile
// loop so the failing loop is identifiable from the condition alone.
const (
	condReasonReconcileSucceeded    = "ReconcileSucceeded"
	condReasonHermesConfigMapFailed = "HermesConfigMapReconcileFailed"
	condReasonHermesSecretFailed    = "HermesSecretReconcileFailed"
	condReasonSearXNGConfigMapFail  = "SearXNGConfigMapReconcileFailed"
	condReasonSearXNGSecretFailed   = "SearXNGSecretReconcileFailed"
	condReasonServiceAccountFailed  = "ServiceAccountReconcileFailed"
	condReasonRoleFailed            = "RoleReconcileFailed"
	condReasonServiceFailed         = "ServiceReconcileFailed"
	condReasonIngressFailed         = "IngressReconcileFailed"
	condReasonNetworkPolicyFailed   = "NetworkPolicyReconcileFailed"
	condReasonStatefulSetFailed     = "StatefulSetReconcileFailed"
)

// markReady sets the Ready condition to True with reason ReconcileSucceeded
// and reports whether the status changed. The caller is responsible for
// persisting the status.
func (u *HermesAgentUseCase) markReady(ctx context.Context, ha *agentsv1alpha1.HermesAgent) bool {
	changed := setReadyCondition(ha, metav1.ConditionTrue, condReasonReconcileSucceeded, "Reconciled all managed resources")
	if changed {
		u.tel.Info(ctx, "HermesAgent reconciled successfully")
	}
	return changed
}

// markReconcileFailed sets the Ready condition to False with the failing
// loop's reason and persists the status best-effort. It always returns the
// original reconcile error so a failing status write never masks it.
func (u *HermesAgentUseCase) markReconcileFailed(ctx context.Context, ha *agentsv1alpha1.HermesAgent, reason string, reconcileErr error) error {
	changed := setReadyCondition(ha, metav1.ConditionFalse, reason, reconcileErr.Error())
	if !changed {
		return reconcileErr
	}
	u.tel.Error(ctx, reconcileErr, "Reconciliation failed", "reason", reason)
	if err := u.kube.UpdateHermesAgentStatus(ctx, UpdateHermesAgentStatusParam{HermesAgent: ha}); err != nil {
		u.tel.Error(ctx, err, "Could not update Ready condition after reconcile failure")
	}
	return reconcileErr
}

// setReadyCondition sets (or updates) the Ready condition and reports whether
// anything changed.
func setReadyCondition(ha *agentsv1alpha1.HermesAgent, status metav1.ConditionStatus, reason, message string) bool {
	return meta.SetStatusCondition(&ha.Status.Conditions, metav1.Condition{
		Type:               string(agentsv1alpha1.ConditionReady),
		Status:             status,
		ObservedGeneration: ha.Generation,
		Reason:             reason,
		Message:            message,
	})
}
