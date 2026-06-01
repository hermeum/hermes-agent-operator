package usecase

import (
	"context"
	agentsv1alpha1 "noahingh/hermes-agent-operator/api/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func (u *HermesAgentUseCase) reconcileSearXNGConfigMap(ctx context.Context, ha *agentsv1alpha1.HermesAgent) error {
	nsName := types.NamespacedName{Name: ha.GetSearXNGName(), Namespace: ha.Namespace}

	existing, err := u.kube.GetConfigMap(ctx, GetConfigMapParam{NamespacedName: nsName})
	if err != nil {
		return err
	}

	if !ha.GetSearXNG().IsEnabled() {
		if existing == nil {
			return nil
		}
		err := u.kube.DeleteConfigMap(ctx, DeleteConfigMapParam{NamespacedName: nsName})
		u.tel.IncConfigMapOperation(ctx, IncConfigMapOperationParam{Operation: OperationDelete, Result: resultOf(err)})
		return err
	}

	desired := u.buildSearXNGConfigMap(ha)
	if existing != nil {
		desired.ResourceVersion = existing.ResourceVersion
		err := u.kube.UpdateConfigMapOwnedByHermesAgent(ctx, UpdateConfigMapParam{HermesAgent: ha, ConfigMap: desired})
		u.tel.IncConfigMapOperation(ctx, IncConfigMapOperationParam{Operation: OperationUpdate, Result: resultOf(err)})
		return err
	}

	err = u.kube.CreateConfigMapOwnedByHermesAgent(ctx, CreateConfigMapOfHermesAgentParam{HermesAgent: ha, ConfigMap: desired})
	u.tel.IncConfigMapOperation(ctx, IncConfigMapOperationParam{Operation: OperationCreate, Result: resultOf(err)})
	return err
}

func (u *HermesAgentUseCase) buildSearXNGConfigMap(ha *agentsv1alpha1.HermesAgent) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ha.GetSearXNGName(),
			Namespace: ha.Namespace,
		},
		Data: ha.GetSearXNG().GetConfigFiles(),
	}
}
