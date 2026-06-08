package usecase

import (
	"context"
	"maps"
	"time"

	agentsv1alpha1 "hermeum/hermes-agent-operator/api/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

func (u *HermesAgentUseCase) reconcileServiceAccount(ctx context.Context, ha *agentsv1alpha1.HermesAgent) (ctrl.Result, error) {
	nsName := types.NamespacedName{Namespace: ha.Namespace, Name: ha.Name}

	existing, err := u.kube.GetServiceAccount(ctx, GetServiceAccountParam{NamespacedName: nsName})
	if err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}

	if !ha.GetSecurity().GetRBAC().ShouldCreateServiceAccount() {
		if existing != nil {
			err := u.kube.DeleteServiceAccount(ctx, DeleteServiceAccountParam{NamespacedName: nsName})
			if err != nil {
				return ctrl.Result{RequeueAfter: 30 * time.Second}, err
			}
			u.tel.Debug(ctx, "ServiceAccount deleted", "namespacedName", nsName)
		}
		ha.Status.ManagedResources.ServiceAccount = ""
		if err := u.kube.UpdateHermesAgentStatus(ctx, UpdateHermesAgentStatusParam{HermesAgent: ha}); err != nil {
			return ctrl.Result{RequeueAfter: 30 * time.Second}, err
		}
		return ctrl.Result{}, nil
	}

	desired := buildServiceAccount(ha)
	if existing != nil {
		desired.ResourceVersion = existing.ResourceVersion
		err := u.kube.UpdateServiceAccountOwnedByHermesAgent(ctx, UpdateServiceAccountParam{HermesAgent: ha, ServiceAccount: desired})
		if err != nil {
			return ctrl.Result{RequeueAfter: 30 * time.Second}, err
		}
		u.tel.Debug(ctx, "ServiceAccount updated", "namespacedName", nsName)
		ha.Status.ManagedResources.ServiceAccount = ha.Name
		return ctrl.Result{}, nil
	}

	err = u.kube.CreateServiceAccountOwnedByHermesAgent(ctx, CreateServiceAccountOfHermesAgentParam{HermesAgent: ha, ServiceAccount: desired})
	if err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}
	u.tel.Debug(ctx, "ServiceAccount created", "namespacedName", nsName)
	ha.Status.ManagedResources.ServiceAccount = ha.Name
	return ctrl.Result{}, nil
}

func buildServiceAccount(ha *agentsv1alpha1.HermesAgent) *corev1.ServiceAccount {
	var annotations map[string]string
	if r := ha.GetSecurity().GetRBAC(); r != nil && len(r.ServiceAccountAnnotations) > 0 {
		annotations = make(map[string]string, len(r.ServiceAccountAnnotations))
		maps.Copy(annotations, r.ServiceAccountAnnotations)
	}
	return &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:        ha.Name,
			Namespace:   ha.Namespace,
			Labels:      resourceLabels(ha),
			Annotations: annotations,
		},
	}
}
