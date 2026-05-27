package usecase

import (
	"context"
	agentsv1alpha1 "noahingh/hermes-agent-operator/api/v1alpha1"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

type Kubernetes interface {
	GetHermesAgent(ctx context.Context, param GetHermesAgentParam) (*agentsv1alpha1.HermesAgent, error)

	GetConfigMap(ctx context.Context, param GetConfigMapParam) (*corev1.ConfigMap, error)
	CreateConfigMapOwnedByHermesAgent(ctx context.Context, param CreateConfigMapOfHermesAgentParam) error
	UpdateConfigMapOwnedByHermesAgent(ctx context.Context, param UpdateConfigMapParam) error

	GetStatefulSet(ctx context.Context, param GetStatefulSetParam) (*appsv1.StatefulSet, error)
	CreateStatefulSetOwnedByHermesAgent(ctx context.Context, param CreateStatefulSetOfHermesAgentParam) error
	UpdateStatefulSetOwnedByHermesAgent(ctx context.Context, param UpdateStatefulSetParam) error
}

type GetHermesAgentParam struct {
	NamespacedName types.NamespacedName
}

type GetConfigMapParam struct {
	NamespacedName types.NamespacedName
}

type CreateConfigMapOfHermesAgentParam struct {
	HermesAgent *agentsv1alpha1.HermesAgent
	ConfigMap   *corev1.ConfigMap
}

type UpdateConfigMapParam struct {
	HermesAgent *agentsv1alpha1.HermesAgent
	ConfigMap   *corev1.ConfigMap
}

type GetStatefulSetParam struct {
	NamespacedName types.NamespacedName
}

type CreateStatefulSetOfHermesAgentParam struct {
	HermesAgent *agentsv1alpha1.HermesAgent
	StatefulSet *appsv1.StatefulSet
}

type UpdateStatefulSetParam struct {
	HermesAgent *agentsv1alpha1.HermesAgent
	StatefulSet *appsv1.StatefulSet
}
