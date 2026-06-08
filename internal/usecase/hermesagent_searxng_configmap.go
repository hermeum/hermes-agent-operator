package usecase

import (
	"context"
	"time"

	agentsv1alpha1 "hermeum/hermes-agent-operator/api/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

func (u *HermesAgentUseCase) reconcileSearXNGConfigMap(ctx context.Context, ha *agentsv1alpha1.HermesAgent) (ctrl.Result, error) {
	nsName := types.NamespacedName{Namespace: ha.Namespace, Name: ha.Name}
	cmNsName := types.NamespacedName{Name: ha.GetSearXNGName(), Namespace: ha.Namespace}

	existing, err := u.kube.GetConfigMap(ctx, GetConfigMapParam{NamespacedName: cmNsName})
	if err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}

	if !ha.GetSearXNG().IsEnabled() {
		if existing != nil {
			err := u.kube.DeleteConfigMap(ctx, DeleteConfigMapParam{NamespacedName: cmNsName})
			if err != nil {
				return ctrl.Result{RequeueAfter: 30 * time.Second}, err
			}
			u.tel.Debug(ctx, "SearXNG ConfigMap deleted", "namespacedName", nsName)
		}
		ha.Status.ManagedResources.SearXNGConfigMap = ""
		if err := u.kube.UpdateHermesAgentStatus(ctx, UpdateHermesAgentStatusParam{HermesAgent: ha}); err != nil {
			return ctrl.Result{RequeueAfter: 30 * time.Second}, err
		}
		return ctrl.Result{}, nil
	}

	desired := buildSearXNGConfigMap(ha)
	if existing != nil {
		desired.ResourceVersion = existing.ResourceVersion
		err := u.kube.UpdateConfigMapOwnedByHermesAgent(ctx, UpdateConfigMapParam{HermesAgent: ha, ConfigMap: desired})
		if err != nil {
			return ctrl.Result{RequeueAfter: 30 * time.Second}, err
		}
		u.tel.Debug(ctx, "SearXNG ConfigMap updated", "namespacedName", nsName)
		ha.Status.ManagedResources.SearXNGConfigMap = ha.GetSearXNGName()
		return ctrl.Result{}, nil
	}

	err = u.kube.CreateConfigMapOwnedByHermesAgent(ctx, CreateConfigMapOfHermesAgentParam{HermesAgent: ha, ConfigMap: desired})
	if err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}
	u.tel.Debug(ctx, "SearXNG ConfigMap created", "namespacedName", nsName)
	ha.Status.ManagedResources.SearXNGConfigMap = ha.GetSearXNGName()
	return ctrl.Result{}, nil
}

func buildSearXNGConfigMap(ha *agentsv1alpha1.HermesAgent) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ha.GetSearXNGName(),
			Namespace: ha.Namespace,
			Labels:    resourceLabels(ha),
		},
		Data: ha.GetSearXNG().GetConfigFiles(),
	}
}
