package usecase

import (
	"context"
	"time"

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
	u.tel.Info(ctx, "Starting reconciliation")

	ha, err := u.kube.GetHermesAgent(ctx, GetHermesAgentParam(param))
	if err != nil {
		u.tel.Error(ctx, err, "Failed to get HermesAgent")
		u.tel.IncReconcile(ctx, IncReconcileParam{NamespacedName: param.NamespacedName, Result: ResultError})
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}
	if ha == nil {
		u.tel.Debug(ctx, "HermesAgent not found")
		u.tel.IncNotFound(ctx, IncNotFoundParam(param))
		u.tel.IncReconcile(ctx, IncReconcileParam{NamespacedName: param.NamespacedName, Result: ResultNotFound})
		return ctrl.Result{}, nil
	}

	nsName := types.NamespacedName{Namespace: ha.Namespace, Name: ha.Name}

	if result, err := u.reconcileHermesConfigMap(ctx, ha); err != nil || !result.IsZero() {
		if err != nil {
			u.tel.Error(ctx, err, "Failed to reconcile Hermes ConfigMap")
			u.tel.IncReconcile(ctx, IncReconcileParam{NamespacedName: nsName, Result: ResultError})
		}
		return result, err
	}
	if result, err := u.reconcileHermesSecret(ctx, ha); err != nil || !result.IsZero() {
		if err != nil {
			u.tel.Error(ctx, err, "Failed to reconcile Hermes Secret")
			u.tel.IncReconcile(ctx, IncReconcileParam{NamespacedName: nsName, Result: ResultError})
		}
		return result, err
	}
	if result, err := u.reconcileSearXNGConfigMap(ctx, ha); err != nil || !result.IsZero() {
		if err != nil {
			u.tel.Error(ctx, err, "Failed to reconcile SearXNG ConfigMap")
			u.tel.IncReconcile(ctx, IncReconcileParam{NamespacedName: nsName, Result: ResultError})
		}
		return result, err
	}
	if result, err := u.reconcileSearXNGSecret(ctx, ha); err != nil || !result.IsZero() {
		if err != nil {
			u.tel.Error(ctx, err, "Failed to reconcile SearXNG Secret")
			u.tel.IncReconcile(ctx, IncReconcileParam{NamespacedName: nsName, Result: ResultError})
		}
		return result, err
	}
	if result, err := u.reconcileServiceAccount(ctx, ha); err != nil || !result.IsZero() {
		if err != nil {
			u.tel.Error(ctx, err, "Failed to reconcile ServiceAccount")
			u.tel.IncReconcile(ctx, IncReconcileParam{NamespacedName: nsName, Result: ResultError})
		}
		return result, err
	}
	if result, err := u.reconcileRole(ctx, ha); err != nil || !result.IsZero() {
		if err != nil {
			u.tel.Error(ctx, err, "Failed to reconcile Role")
			u.tel.IncReconcile(ctx, IncReconcileParam{NamespacedName: nsName, Result: ResultError})
		}
		return result, err
	}
	if result, err := u.reconcileService(ctx, ha); err != nil || !result.IsZero() {
		if err != nil {
			u.tel.Error(ctx, err, "Failed to reconcile Service")
			u.tel.IncReconcile(ctx, IncReconcileParam{NamespacedName: nsName, Result: ResultError})
		}
		return result, err
	}
	if result, err := u.reconcileIngress(ctx, ha); err != nil || !result.IsZero() {
		if err != nil {
			u.tel.Error(ctx, err, "Failed to reconcile Ingress")
			u.tel.IncReconcile(ctx, IncReconcileParam{NamespacedName: nsName, Result: ResultError})
		}
		return result, err
	}
	if result, err := u.reconcileNetworkPolicy(ctx, ha); err != nil || !result.IsZero() {
		if err != nil {
			u.tel.Error(ctx, err, "Failed to reconcile NetworkPolicy")
			u.tel.IncReconcile(ctx, IncReconcileParam{NamespacedName: nsName, Result: ResultError})
		}
		return result, err
	}
	if result, err := u.reconcileStatefulSet(ctx, ha); err != nil || !result.IsZero() {
		if err != nil {
			u.tel.Error(ctx, err, "Failed to reconcile StatefulSet")
			u.tel.IncReconcile(ctx, IncReconcileParam{NamespacedName: nsName, Result: ResultError})
		}
		return result, err
	}

	u.tel.Info(ctx, "Reconciliation completed successfully")
	u.tel.IncReconcile(ctx, IncReconcileParam{NamespacedName: nsName, Result: ResultSuccess})
	return ctrl.Result{}, nil
}
