package usecase

import (
	"context"
	"time"

	agentsv1alpha1 "hermeum/hermes-agent-operator/api/v1alpha1"

	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	domain = "hermeum.app"
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

func (u *HermesAgentUseCase) Reconcile(ctx context.Context, param ReconcileParam) (ctrl.Result, error) { //nolint:gocyclo
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
		u.tel.Debug(ctx, "HermesAgent not found", "namespacedName", param.NamespacedName)
		u.tel.IncNotFound(ctx, IncNotFoundParam(param))
		u.tel.IncReconcile(ctx, IncReconcileParam{NamespacedName: param.NamespacedName, Result: ResultNotFound})
		return ctrl.Result{}, nil
	}

	nsName := types.NamespacedName{Namespace: ha.Namespace, Name: ha.Name}

	ha.Status.ManagedResources = agentsv1alpha1.ManagedResources{}

	if result, err := u.reconcileHermesConfigMap(ctx, ha); err != nil || !result.IsZero() {
		if err != nil {
			u.tel.Error(ctx, err, "Failed to reconcile Hermes ConfigMap", "namespacedName", nsName)
			u.tel.IncReconcile(ctx, IncReconcileParam{NamespacedName: nsName, Result: ResultError})
		}
		return result, err
	}
	if result, err := u.reconcileHermesSecret(ctx, ha); err != nil || !result.IsZero() {
		if err != nil {
			u.tel.Error(ctx, err, "Failed to reconcile Hermes Secret", "namespacedName", nsName)
			u.tel.IncReconcile(ctx, IncReconcileParam{NamespacedName: nsName, Result: ResultError})
		}
		return result, err
	}
	if result, err := u.reconcileSearXNGConfigMap(ctx, ha); err != nil || !result.IsZero() {
		if err != nil {
			u.tel.Error(ctx, err, "Failed to reconcile SearXNG ConfigMap", "namespacedName", nsName)
			u.tel.IncReconcile(ctx, IncReconcileParam{NamespacedName: nsName, Result: ResultError})
		}
		return result, err
	}
	if result, err := u.reconcileSearXNGSecret(ctx, ha); err != nil || !result.IsZero() {
		if err != nil {
			u.tel.Error(ctx, err, "Failed to reconcile SearXNG Secret", "namespacedName", nsName)
			u.tel.IncReconcile(ctx, IncReconcileParam{NamespacedName: nsName, Result: ResultError})
		}
		return result, err
	}
	if result, err := u.reconcileServiceAccount(ctx, ha); err != nil || !result.IsZero() {
		if err != nil {
			u.tel.Error(ctx, err, "Failed to reconcile ServiceAccount", "namespacedName", nsName)
			u.tel.IncReconcile(ctx, IncReconcileParam{NamespacedName: nsName, Result: ResultError})
		}
		return result, err
	}
	if result, err := u.reconcileRole(ctx, ha); err != nil || !result.IsZero() {
		if err != nil {
			u.tel.Error(ctx, err, "Failed to reconcile Role", "namespacedName", nsName)
			u.tel.IncReconcile(ctx, IncReconcileParam{NamespacedName: nsName, Result: ResultError})
		}
		return result, err
	}
	if result, err := u.reconcileService(ctx, ha); err != nil || !result.IsZero() {
		if err != nil {
			u.tel.Error(ctx, err, "Failed to reconcile Service", "namespacedName", nsName)
			u.tel.IncReconcile(ctx, IncReconcileParam{NamespacedName: nsName, Result: ResultError})
		}
		return result, err
	}
	if result, err := u.reconcileIngress(ctx, ha); err != nil || !result.IsZero() {
		if err != nil {
			u.tel.Error(ctx, err, "Failed to reconcile Ingress", "namespacedName", nsName)
			u.tel.IncReconcile(ctx, IncReconcileParam{NamespacedName: nsName, Result: ResultError})
		}
		return result, err
	}
	if result, err := u.reconcileNetworkPolicy(ctx, ha); err != nil || !result.IsZero() {
		if err != nil {
			u.tel.Error(ctx, err, "Failed to reconcile NetworkPolicy", "namespacedName", nsName)
			u.tel.IncReconcile(ctx, IncReconcileParam{NamespacedName: nsName, Result: ResultError})
		}
		return result, err
	}
	if result, err := u.reconcileStatefulSet(ctx, ha); err != nil || !result.IsZero() {
		if err != nil {
			u.tel.Error(ctx, err, "Failed to reconcile StatefulSet", "namespacedName", nsName)
			u.tel.IncReconcile(ctx, IncReconcileParam{NamespacedName: nsName, Result: ResultError})
		}
		return result, err
	}

	u.tel.Info(ctx, "Reconciliation completed successfully", "namespacedName", param.NamespacedName)
	u.tel.IncReconcile(ctx, IncReconcileParam{NamespacedName: nsName, Result: ResultSuccess})
	return ctrl.Result{}, nil
}

func resultOf(err error) Result {
	if err != nil {
		return ResultError
	}
	return ResultSuccess
}
