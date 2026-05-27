package infras

import (
	"context"

	agentsv1alpha1 "noahingh/hermes-agent-operator/api/v1alpha1"
	"noahingh/hermes-agent-operator/internal/usecase"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type KubernetesClient struct {
	client client.Client
	scheme *runtime.Scheme
}

func NewKubernetesClient(c client.Client, scheme *runtime.Scheme) *KubernetesClient {
	return &KubernetesClient{client: c, scheme: scheme}
}

func (k *KubernetesClient) GetHermesAgent(ctx context.Context, param usecase.GetHermesAgentParam) (*agentsv1alpha1.HermesAgent, error) {
	ha := &agentsv1alpha1.HermesAgent{}
	if err := k.client.Get(ctx, param.NamespacedName, ha); err != nil {
		if errors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return ha, nil
}

func (k *KubernetesClient) GetConfigMap(ctx context.Context, param usecase.GetConfigMapParam) (*corev1.ConfigMap, error) {
	cm := &corev1.ConfigMap{}
	if err := k.client.Get(ctx, param.NamespacedName, cm); err != nil {
		if errors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return cm, nil
}

func (k *KubernetesClient) CreateConfigMapOwnedByHermesAgent(ctx context.Context, param usecase.CreateConfigMapOfHermesAgentParam) error {
	if err := ctrl.SetControllerReference(param.HermesAgent, param.ConfigMap, k.scheme); err != nil {
		return err
	}
	return k.client.Create(ctx, param.ConfigMap)
}

func (k *KubernetesClient) UpdateConfigMapOwnedByHermesAgent(ctx context.Context, param usecase.UpdateConfigMapParam) error {
	if err := ctrl.SetControllerReference(param.HermesAgent, param.ConfigMap, k.scheme); err != nil {
		return err
	}
	return k.client.Update(ctx, param.ConfigMap)
}

func (k *KubernetesClient) GetStatefulSet(ctx context.Context, param usecase.GetStatefulSetParam) (*appsv1.StatefulSet, error) {
	sts := &appsv1.StatefulSet{}
	if err := k.client.Get(ctx, param.NamespacedName, sts); err != nil {
		if errors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return sts, nil
}

func (k *KubernetesClient) CreateStatefulSetOwnedByHermesAgent(ctx context.Context, param usecase.CreateStatefulSetOfHermesAgentParam) error {
	if err := ctrl.SetControllerReference(param.HermesAgent, param.StatefulSet, k.scheme); err != nil {
		return err
	}
	return k.client.Create(ctx, param.StatefulSet)
}

func (k *KubernetesClient) UpdateStatefulSetOwnedByHermesAgent(ctx context.Context, param usecase.UpdateStatefulSetParam) error {
	if err := ctrl.SetControllerReference(param.HermesAgent, param.StatefulSet, k.scheme); err != nil {
		return err
	}
	return k.client.Update(ctx, param.StatefulSet)
}
