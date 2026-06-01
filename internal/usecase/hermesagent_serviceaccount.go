package usecase

import (
	"context"
	"maps"
	agentsv1alpha1 "noahingh/hermes-agent-operator/api/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func (u *HermesAgentUseCase) reconcileServiceAccount(ctx context.Context, ha *agentsv1alpha1.HermesAgent) error {
	name := ha.Name
	nsName := types.NamespacedName{Name: name, Namespace: ha.Namespace}

	existing, err := u.kube.GetServiceAccount(ctx, GetServiceAccountParam{NamespacedName: nsName})
	if err != nil {
		return err
	}

	if !ha.GetSecurity().GetRBAC().ShouldCreateServiceAccount() {
		if existing == nil {
			return nil
		}
		err := u.kube.DeleteServiceAccount(ctx, DeleteServiceAccountParam{NamespacedName: nsName})
		u.tel.IncServiceAccountOperation(ctx, IncServiceAccountOperationParam{NamespacedName: types.NamespacedName{Namespace: ha.Namespace, Name: ha.Name}, Operation: OperationDelete, Result: resultOf(err)})
		return err
	}

	desired := buildServiceAccount(ha)
	if existing != nil {
		desired.ResourceVersion = existing.ResourceVersion
		err := u.kube.UpdateServiceAccountOwnedByHermesAgent(ctx, UpdateServiceAccountParam{HermesAgent: ha, ServiceAccount: desired})
		u.tel.IncServiceAccountOperation(ctx, IncServiceAccountOperationParam{NamespacedName: types.NamespacedName{Namespace: ha.Namespace, Name: ha.Name}, Operation: OperationUpdate, Result: resultOf(err)})
		return err
	}

	err = u.kube.CreateServiceAccountOwnedByHermesAgent(ctx, CreateServiceAccountOfHermesAgentParam{HermesAgent: ha, ServiceAccount: desired})
	u.tel.IncServiceAccountOperation(ctx, IncServiceAccountOperationParam{NamespacedName: types.NamespacedName{Namespace: ha.Namespace, Name: ha.Name}, Operation: OperationCreate, Result: resultOf(err)})
	return err
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
