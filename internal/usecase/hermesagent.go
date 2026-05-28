package usecase

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/types"
)

const (
	domain                 = "hermes-agent-operator.xyz"
	workspacePathSeparator = "--"
	defaultPathEnv         = "/opt/data/.local/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	gatewayPortName        = "gateway"
	gatewayPort            = int32(8642)
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

func (u *HermesAgentUseCase) Reconcile(ctx context.Context, param ReconcileParam) error {
	start := time.Now()
	defer func() {
		u.tel.ObserveReconcileDuration(ctx, ObserveReconcileDurationParam{Seconds: time.Since(start).Seconds()})
	}()
	u.tel.Info(ctx, "Starting reconciliation", "namespacedName", param.NamespacedName)

	ha, err := u.kube.GetHermesAgent(ctx, GetHermesAgentParam(param))
	if err != nil {
		u.tel.Error(ctx, err, "Failed to get HermesAgent", "namespacedName", param.NamespacedName)
		u.tel.IncReconcile(ctx, IncReconcileParam{Result: ResultError})
		return err
	}
	if ha == nil {
		u.tel.Info(ctx, "HermesAgent not found", "namespacedName", param.NamespacedName)
		u.tel.IncNotFound(ctx, IncNotFoundParam{})
		u.tel.IncReconcile(ctx, IncReconcileParam{Result: ResultNotFound})
		return nil
	}

	if err := u.reconcileConfigMap(ctx, ha); err != nil {
		u.tel.Error(ctx, err, "Failed to reconcile ConfigMap", "namespacedName", param.NamespacedName)
		u.tel.IncReconcile(ctx, IncReconcileParam{Result: ResultError})
		return err
	}
	u.tel.Info(ctx, "ConfigMap reconciled successfully", "namespacedName", param.NamespacedName)

	if err := u.reconcileServiceAccount(ctx, ha); err != nil {
		u.tel.Error(ctx, err, "Failed to reconcile ServiceAccount", "namespacedName", param.NamespacedName)
		u.tel.IncReconcile(ctx, IncReconcileParam{Result: ResultError})
		return err
	}
	u.tel.Info(ctx, "ServiceAccount reconciled successfully", "namespacedName", param.NamespacedName)

	if err := u.reconcileRole(ctx, ha); err != nil {
		u.tel.Error(ctx, err, "Failed to reconcile Role", "namespacedName", param.NamespacedName)
		u.tel.IncReconcile(ctx, IncReconcileParam{Result: ResultError})
		return err
	}
	u.tel.Info(ctx, "Role reconciled successfully", "namespacedName", param.NamespacedName)

	if err := u.reconcileStatefulSet(ctx, ha); err != nil {
		u.tel.Error(ctx, err, "Failed to reconcile StatefulSet", "namespacedName", param.NamespacedName)
		u.tel.IncReconcile(ctx, IncReconcileParam{Result: ResultError})
		return err
	}
	u.tel.Info(ctx, "StatefulSet reconciled successfully", "namespacedName", param.NamespacedName)

	if err := u.reconcileService(ctx, ha); err != nil {
		u.tel.Error(ctx, err, "Failed to reconcile Service", "namespacedName", param.NamespacedName)
		u.tel.IncReconcile(ctx, IncReconcileParam{Result: ResultError})
		return err
	}
	u.tel.Info(ctx, "Service reconciled successfully", "namespacedName", param.NamespacedName)

	if err := u.reconcileIngress(ctx, ha); err != nil {
		u.tel.Error(ctx, err, "Failed to reconcile Ingress", "namespacedName", param.NamespacedName)
		u.tel.IncReconcile(ctx, IncReconcileParam{Result: ResultError})
		return err
	}
	u.tel.Info(ctx, "Ingress reconciled successfully", "namespacedName", param.NamespacedName)

	u.tel.Info(ctx, "Reconciliation completed successfully", "namespacedName", param.NamespacedName)
	u.tel.IncReconcile(ctx, IncReconcileParam{Result: ResultSuccess})
	return nil
}

func resultOf(err error) Result {
	if err != nil {
		return ResultError
	}
	return ResultSuccess
}
