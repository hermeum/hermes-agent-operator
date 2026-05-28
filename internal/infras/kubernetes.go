package infras

import (
	"context"

	agentsv1alpha1 "noahingh/hermes-agent-operator/api/v1alpha1"
	"noahingh/hermes-agent-operator/internal/usecase"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func (k *KubernetesClient) GetServiceAccount(ctx context.Context, param usecase.GetServiceAccountParam) (*corev1.ServiceAccount, error) {
	sa := &corev1.ServiceAccount{}
	if err := k.client.Get(ctx, param.NamespacedName, sa); err != nil {
		if errors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return sa, nil
}

func (k *KubernetesClient) CreateServiceAccountOwnedByHermesAgent(ctx context.Context, param usecase.CreateServiceAccountOfHermesAgentParam) error {
	if err := ctrl.SetControllerReference(param.HermesAgent, param.ServiceAccount, k.scheme); err != nil {
		return err
	}
	return k.client.Create(ctx, param.ServiceAccount)
}

func (k *KubernetesClient) UpdateServiceAccountOwnedByHermesAgent(ctx context.Context, param usecase.UpdateServiceAccountParam) error {
	if err := ctrl.SetControllerReference(param.HermesAgent, param.ServiceAccount, k.scheme); err != nil {
		return err
	}
	return k.client.Update(ctx, param.ServiceAccount)
}

func (k *KubernetesClient) DeleteServiceAccount(ctx context.Context, param usecase.DeleteServiceAccountParam) error {
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      param.NamespacedName.Name,
			Namespace: param.NamespacedName.Namespace,
		},
	}
	return client.IgnoreNotFound(k.client.Delete(ctx, sa))
}

func (k *KubernetesClient) GetRole(ctx context.Context, param usecase.GetRoleParam) (*rbacv1.Role, error) {
	role := &rbacv1.Role{}
	if err := k.client.Get(ctx, param.NamespacedName, role); err != nil {
		if errors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return role, nil
}

func (k *KubernetesClient) CreateRoleOwnedByHermesAgent(ctx context.Context, param usecase.CreateRoleOfHermesAgentParam) error {
	if err := ctrl.SetControllerReference(param.HermesAgent, param.Role, k.scheme); err != nil {
		return err
	}
	return k.client.Create(ctx, param.Role)
}

func (k *KubernetesClient) UpdateRoleOwnedByHermesAgent(ctx context.Context, param usecase.UpdateRoleParam) error {
	if err := ctrl.SetControllerReference(param.HermesAgent, param.Role, k.scheme); err != nil {
		return err
	}
	return k.client.Update(ctx, param.Role)
}

func (k *KubernetesClient) DeleteRole(ctx context.Context, param usecase.DeleteRoleParam) error {
	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      param.NamespacedName.Name,
			Namespace: param.NamespacedName.Namespace,
		},
	}
	return client.IgnoreNotFound(k.client.Delete(ctx, role))
}

func (k *KubernetesClient) GetRoleBinding(ctx context.Context, param usecase.GetRoleBindingParam) (*rbacv1.RoleBinding, error) {
	rb := &rbacv1.RoleBinding{}
	if err := k.client.Get(ctx, param.NamespacedName, rb); err != nil {
		if errors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return rb, nil
}

func (k *KubernetesClient) CreateRoleBindingOwnedByHermesAgent(ctx context.Context, param usecase.CreateRoleBindingOfHermesAgentParam) error {
	if err := ctrl.SetControllerReference(param.HermesAgent, param.RoleBinding, k.scheme); err != nil {
		return err
	}
	return k.client.Create(ctx, param.RoleBinding)
}

func (k *KubernetesClient) UpdateRoleBindingOwnedByHermesAgent(ctx context.Context, param usecase.UpdateRoleBindingParam) error {
	if err := ctrl.SetControllerReference(param.HermesAgent, param.RoleBinding, k.scheme); err != nil {
		return err
	}
	return k.client.Update(ctx, param.RoleBinding)
}

func (k *KubernetesClient) DeleteRoleBinding(ctx context.Context, param usecase.DeleteRoleBindingParam) error {
	rb := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      param.NamespacedName.Name,
			Namespace: param.NamespacedName.Namespace,
		},
	}
	return client.IgnoreNotFound(k.client.Delete(ctx, rb))
}

func (k *KubernetesClient) GetService(ctx context.Context, param usecase.GetServiceParam) (*corev1.Service, error) {
	svc := &corev1.Service{}
	if err := k.client.Get(ctx, param.NamespacedName, svc); err != nil {
		if errors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return svc, nil
}

func (k *KubernetesClient) CreateServiceOwnedByHermesAgent(ctx context.Context, param usecase.CreateServiceOfHermesAgentParam) error {
	if err := ctrl.SetControllerReference(param.HermesAgent, param.Service, k.scheme); err != nil {
		return err
	}
	return k.client.Create(ctx, param.Service)
}

func (k *KubernetesClient) UpdateServiceOwnedByHermesAgent(ctx context.Context, param usecase.UpdateServiceParam) error {
	if err := ctrl.SetControllerReference(param.HermesAgent, param.Service, k.scheme); err != nil {
		return err
	}
	return k.client.Update(ctx, param.Service)
}

func (k *KubernetesClient) GetIngress(ctx context.Context, param usecase.GetIngressParam) (*networkingv1.Ingress, error) {
	ing := &networkingv1.Ingress{}
	if err := k.client.Get(ctx, param.NamespacedName, ing); err != nil {
		if errors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return ing, nil
}

func (k *KubernetesClient) CreateIngressOwnedByHermesAgent(ctx context.Context, param usecase.CreateIngressOfHermesAgentParam) error {
	if err := ctrl.SetControllerReference(param.HermesAgent, param.Ingress, k.scheme); err != nil {
		return err
	}
	return k.client.Create(ctx, param.Ingress)
}

func (k *KubernetesClient) UpdateIngressOwnedByHermesAgent(ctx context.Context, param usecase.UpdateIngressParam) error {
	if err := ctrl.SetControllerReference(param.HermesAgent, param.Ingress, k.scheme); err != nil {
		return err
	}
	return k.client.Update(ctx, param.Ingress)
}

func (k *KubernetesClient) DeleteIngress(ctx context.Context, param usecase.DeleteIngressParam) error {
	ing := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      param.NamespacedName.Name,
			Namespace: param.NamespacedName.Namespace,
		},
	}
	return client.IgnoreNotFound(k.client.Delete(ctx, ing))
}
