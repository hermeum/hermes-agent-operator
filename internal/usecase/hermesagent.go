package usecase

import (
	"context"
	"time"

	agentsv1alpha1 "noahingh/hermes-agent-operator/api/v1alpha1"

	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	domain = "hermes-agent-operator.xyz"
)

type HermesAgentUseCase struct {
	kube Kubernetes
	tel  Telemetry
}

type ReconcileParam struct {
	NamespacedName types.NamespacedName
}

func NewHermesAgentUseCase(kube Kubernetes, tel Telemetry) *HermesAgentUseCase {
	return &HermesAgentUseCase{
		kube: kube,
		tel:  tel,
	}
}

func (u *HermesAgentUseCase) Reconcile(ctx context.Context, param ReconcileParam) (ctrl.Result, error) {
	start := time.Now()
	defer func() {
		u.tel.ObserveReconcileDuration(ctx, ObserveReconcileDurationParam{
			NamespacedName: param.NamespacedName,
			Seconds:        time.Since(start).Seconds(),
		})
	}()
	u.tel.Info(ctx, "Starting reconciliation", "namespacedName", param.NamespacedName)

	ha, err := u.kube.GetHermesAgent(ctx, GetHermesAgentParam(param))
	if err != nil {
		u.tel.Error(ctx, err, "Failed to get HermesAgent", "namespacedName", param.NamespacedName)
		u.tel.IncReconcile(ctx, IncReconcileParam{NamespacedName: param.NamespacedName, Result: ResultError})
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}
	if ha == nil {
		u.tel.Info(ctx, "HermesAgent not found", "namespacedName", param.NamespacedName)
		u.tel.IncNotFound(ctx, IncNotFoundParam(param))
		u.tel.IncReconcile(ctx, IncReconcileParam{NamespacedName: param.NamespacedName, Result: ResultNotFound})
		return ctrl.Result{}, nil
	}

	nsName := types.NamespacedName{Namespace: ha.Namespace, Name: ha.Name}

	if err := u.reconcileHermesConfigMap(ctx, ha); err != nil {
		u.tel.Error(ctx, err, "Failed to reconcile Hermes ConfigMap", "namespacedName", param.NamespacedName)
		u.tel.IncReconcile(ctx, IncReconcileParam{NamespacedName: nsName, Result: ResultError})
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}
	u.tel.Info(ctx, "ConfigMap reconciled successfully", "namespacedName", param.NamespacedName)

	if err := u.reconcileSearXNGConfigMap(ctx, ha); err != nil {
		u.tel.Error(ctx, err, "Failed to reconcile SearXNG ConfigMap", "namespacedName", param.NamespacedName)
		u.tel.IncReconcile(ctx, IncReconcileParam{NamespacedName: nsName, Result: ResultError})
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}
	u.tel.Info(ctx, "SearXNG ConfigMap reconciled successfully", "namespacedName", param.NamespacedName)

	if err := u.reconcileServiceAccount(ctx, ha); err != nil {
		u.tel.Error(ctx, err, "Failed to reconcile ServiceAccount", "namespacedName", param.NamespacedName)
		u.tel.IncReconcile(ctx, IncReconcileParam{NamespacedName: nsName, Result: ResultError})
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}
	u.tel.Info(ctx, "ServiceAccount reconciled successfully", "namespacedName", param.NamespacedName)

	if err := u.reconcileRole(ctx, ha); err != nil {
		u.tel.Error(ctx, err, "Failed to reconcile Role", "namespacedName", param.NamespacedName)
		u.tel.IncReconcile(ctx, IncReconcileParam{NamespacedName: nsName, Result: ResultError})
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}
	u.tel.Info(ctx, "Role reconciled successfully", "namespacedName", param.NamespacedName)

	if err := u.reconcileStatefulSet(ctx, ha); err != nil {
		u.tel.Error(ctx, err, "Failed to reconcile StatefulSet", "namespacedName", param.NamespacedName)
		u.tel.IncReconcile(ctx, IncReconcileParam{NamespacedName: nsName, Result: ResultError})
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}
	u.tel.Info(ctx, "StatefulSet reconciled successfully", "namespacedName", param.NamespacedName)

	if err := u.reconcileService(ctx, ha); err != nil {
		u.tel.Error(ctx, err, "Failed to reconcile Service", "namespacedName", param.NamespacedName)
		u.tel.IncReconcile(ctx, IncReconcileParam{NamespacedName: nsName, Result: ResultError})
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}
	u.tel.Info(ctx, "Service reconciled successfully", "namespacedName", param.NamespacedName)

	if err := u.reconcileIngress(ctx, ha); err != nil {
		u.tel.Error(ctx, err, "Failed to reconcile Ingress", "namespacedName", param.NamespacedName)
		u.tel.IncReconcile(ctx, IncReconcileParam{NamespacedName: nsName, Result: ResultError})
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}
	u.tel.Info(ctx, "Ingress reconciled successfully", "namespacedName", param.NamespacedName)

	if err := u.reconcileNetworkPolicy(ctx, ha); err != nil {
		u.tel.Error(ctx, err, "Failed to reconcile NetworkPolicy", "namespacedName", param.NamespacedName)
		u.tel.IncReconcile(ctx, IncReconcileParam{NamespacedName: nsName, Result: ResultError})
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}
	u.tel.Info(ctx, "NetworkPolicy reconciled successfully", "namespacedName", param.NamespacedName)

	if err := u.reconcileStatus(ctx, ha); err != nil {
		u.tel.Error(ctx, err, "Failed to reconcile status", "namespacedName", param.NamespacedName)
		u.tel.IncReconcile(ctx, IncReconcileParam{NamespacedName: nsName, Result: ResultError})
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}

	u.tel.Info(ctx, "Reconciliation completed successfully", "namespacedName", param.NamespacedName)
	u.tel.IncReconcile(ctx, IncReconcileParam{NamespacedName: nsName, Result: ResultSuccess})

	if ha.Status.Phase == agentsv1alpha1.PhasePending || ha.Status.Phase == agentsv1alpha1.PhaseUnknown {
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}
	return ctrl.Result{}, nil
}

func resultOf(err error) Result {
	if err != nil {
		return ResultError
	}
	return ResultSuccess
}
