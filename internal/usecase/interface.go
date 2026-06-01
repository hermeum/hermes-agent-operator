package usecase

import (
	"context"
	agentsv1alpha1 "noahingh/hermes-agent-operator/api/v1alpha1"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/types"
)

// Result is the outcome of a reconcile or sub-operation, used as a metric label.
type Result string

const (
	ResultSuccess  Result = "success"
	ResultError    Result = "error"
	ResultNotFound Result = "not_found"
)

func (r Result) String() string { return string(r) }

// Operation is the kind of write performed on a child resource, used as a metric label.
type Operation string

const (
	OperationCreate Operation = "create"
	OperationUpdate Operation = "update"
	OperationDelete Operation = "delete"
)

func (o Operation) String() string { return string(o) }

// Telemetry collects logs and metrics emitted by the usecase. Each metric has
// its own specific method; the implementation owns the underlying collector names.
type Telemetry interface {
	// Logging
	Debug(ctx context.Context, msg string, keysAndValues ...any)
	Info(ctx context.Context, msg string, keysAndValues ...any)
	Error(ctx context.Context, err error, msg string, keysAndValues ...any)

	// Metrics
	IncReconcile(ctx context.Context, param IncReconcileParam)
	ObserveReconcileDuration(ctx context.Context, param ObserveReconcileDurationParam)
	IncConfigMapOperation(ctx context.Context, param IncConfigMapOperationParam)
	IncStatefulSetOperation(ctx context.Context, param IncStatefulSetOperationParam)
	IncServiceAccountOperation(ctx context.Context, param IncServiceAccountOperationParam)
	IncRoleOperation(ctx context.Context, param IncRoleOperationParam)
	IncRoleBindingOperation(ctx context.Context, param IncRoleBindingOperationParam)
	IncServiceOperation(ctx context.Context, param IncServiceOperationParam)
	IncIngressOperation(ctx context.Context, param IncIngressOperationParam)
	IncNetworkPolicyOperation(ctx context.Context, param IncNetworkPolicyOperationParam)
	IncNotFound(ctx context.Context, param IncNotFoundParam)
}

type IncReconcileParam struct {
	NamespacedName types.NamespacedName
	Result         Result
}

type ObserveReconcileDurationParam struct {
	NamespacedName types.NamespacedName
	Seconds        float64
}

type IncConfigMapOperationParam struct {
	NamespacedName types.NamespacedName
	Operation      Operation
	Result         Result
}

type IncStatefulSetOperationParam struct {
	NamespacedName types.NamespacedName
	Operation      Operation
	Result         Result
}

type IncServiceAccountOperationParam struct {
	NamespacedName types.NamespacedName
	Operation      Operation
	Result         Result
}

type IncRoleOperationParam struct {
	NamespacedName types.NamespacedName
	Operation      Operation
	Result         Result
}

type IncRoleBindingOperationParam struct {
	NamespacedName types.NamespacedName
	Operation      Operation
	Result         Result
}

type IncServiceOperationParam struct {
	NamespacedName types.NamespacedName
	Operation      Operation
	Result         Result
}

type IncIngressOperationParam struct {
	NamespacedName types.NamespacedName
	Operation      Operation
	Result         Result
}

type IncNetworkPolicyOperationParam struct {
	NamespacedName types.NamespacedName
	Operation      Operation
	Result         Result
}

type IncNotFoundParam struct {
	NamespacedName types.NamespacedName
}

type Kubernetes interface {
	GetHermesAgent(ctx context.Context, param GetHermesAgentParam) (*agentsv1alpha1.HermesAgent, error)

	GetConfigMap(ctx context.Context, param GetConfigMapParam) (*corev1.ConfigMap, error)
	CreateConfigMapOwnedByHermesAgent(ctx context.Context, param CreateConfigMapOfHermesAgentParam) error
	UpdateConfigMapOwnedByHermesAgent(ctx context.Context, param UpdateConfigMapParam) error
	DeleteConfigMap(ctx context.Context, param DeleteConfigMapParam) error

	GetStatefulSet(ctx context.Context, param GetStatefulSetParam) (*appsv1.StatefulSet, error)
	CreateStatefulSetOwnedByHermesAgent(ctx context.Context, param CreateStatefulSetOfHermesAgentParam) error
	UpdateStatefulSetOwnedByHermesAgent(ctx context.Context, param UpdateStatefulSetParam) error

	GetServiceAccount(ctx context.Context, param GetServiceAccountParam) (*corev1.ServiceAccount, error)
	CreateServiceAccountOwnedByHermesAgent(ctx context.Context, param CreateServiceAccountOfHermesAgentParam) error
	UpdateServiceAccountOwnedByHermesAgent(ctx context.Context, param UpdateServiceAccountParam) error
	DeleteServiceAccount(ctx context.Context, param DeleteServiceAccountParam) error

	GetRole(ctx context.Context, param GetRoleParam) (*rbacv1.Role, error)
	CreateRoleOwnedByHermesAgent(ctx context.Context, param CreateRoleOfHermesAgentParam) error
	UpdateRoleOwnedByHermesAgent(ctx context.Context, param UpdateRoleParam) error
	DeleteRole(ctx context.Context, param DeleteRoleParam) error

	GetRoleBinding(ctx context.Context, param GetRoleBindingParam) (*rbacv1.RoleBinding, error)
	CreateRoleBindingOwnedByHermesAgent(ctx context.Context, param CreateRoleBindingOfHermesAgentParam) error
	UpdateRoleBindingOwnedByHermesAgent(ctx context.Context, param UpdateRoleBindingParam) error
	DeleteRoleBinding(ctx context.Context, param DeleteRoleBindingParam) error

	GetService(ctx context.Context, param GetServiceParam) (*corev1.Service, error)
	CreateServiceOwnedByHermesAgent(ctx context.Context, param CreateServiceOfHermesAgentParam) error
	UpdateServiceOwnedByHermesAgent(ctx context.Context, param UpdateServiceParam) error

	GetIngress(ctx context.Context, param GetIngressParam) (*networkingv1.Ingress, error)
	CreateIngressOwnedByHermesAgent(ctx context.Context, param CreateIngressOfHermesAgentParam) error
	UpdateIngressOwnedByHermesAgent(ctx context.Context, param UpdateIngressParam) error
	DeleteIngress(ctx context.Context, param DeleteIngressParam) error

	GetNetworkPolicy(ctx context.Context, param GetNetworkPolicyParam) (*networkingv1.NetworkPolicy, error)
	CreateNetworkPolicyOwnedByHermesAgent(ctx context.Context, param CreateNetworkPolicyOfHermesAgentParam) error
	UpdateNetworkPolicyOwnedByHermesAgent(ctx context.Context, param UpdateNetworkPolicyParam) error
	DeleteNetworkPolicy(ctx context.Context, param DeleteNetworkPolicyParam) error
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

type DeleteConfigMapParam struct {
	NamespacedName types.NamespacedName
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

type GetServiceAccountParam struct {
	NamespacedName types.NamespacedName
}

type CreateServiceAccountOfHermesAgentParam struct {
	HermesAgent    *agentsv1alpha1.HermesAgent
	ServiceAccount *corev1.ServiceAccount
}

type UpdateServiceAccountParam struct {
	HermesAgent    *agentsv1alpha1.HermesAgent
	ServiceAccount *corev1.ServiceAccount
}

type DeleteServiceAccountParam struct {
	NamespacedName types.NamespacedName
}

type GetRoleParam struct {
	NamespacedName types.NamespacedName
}

type CreateRoleOfHermesAgentParam struct {
	HermesAgent *agentsv1alpha1.HermesAgent
	Role        *rbacv1.Role
}

type UpdateRoleParam struct {
	HermesAgent *agentsv1alpha1.HermesAgent
	Role        *rbacv1.Role
}

type DeleteRoleParam struct {
	NamespacedName types.NamespacedName
}

type GetRoleBindingParam struct {
	NamespacedName types.NamespacedName
}

type CreateRoleBindingOfHermesAgentParam struct {
	HermesAgent *agentsv1alpha1.HermesAgent
	RoleBinding *rbacv1.RoleBinding
}

type UpdateRoleBindingParam struct {
	HermesAgent *agentsv1alpha1.HermesAgent
	RoleBinding *rbacv1.RoleBinding
}

type DeleteRoleBindingParam struct {
	NamespacedName types.NamespacedName
}

type GetServiceParam struct {
	NamespacedName types.NamespacedName
}

type CreateServiceOfHermesAgentParam struct {
	HermesAgent *agentsv1alpha1.HermesAgent
	Service     *corev1.Service
}

type UpdateServiceParam struct {
	HermesAgent *agentsv1alpha1.HermesAgent
	Service     *corev1.Service
}

type GetIngressParam struct {
	NamespacedName types.NamespacedName
}

type CreateIngressOfHermesAgentParam struct {
	HermesAgent *agentsv1alpha1.HermesAgent
	Ingress     *networkingv1.Ingress
}

type UpdateIngressParam struct {
	HermesAgent *agentsv1alpha1.HermesAgent
	Ingress     *networkingv1.Ingress
}

type DeleteIngressParam struct {
	NamespacedName types.NamespacedName
}

type GetNetworkPolicyParam struct {
	NamespacedName types.NamespacedName
}

type CreateNetworkPolicyOfHermesAgentParam struct {
	HermesAgent   *agentsv1alpha1.HermesAgent
	NetworkPolicy *networkingv1.NetworkPolicy
}

type UpdateNetworkPolicyParam struct {
	HermesAgent   *agentsv1alpha1.HermesAgent
	NetworkPolicy *networkingv1.NetworkPolicy
}

type DeleteNetworkPolicyParam struct {
	NamespacedName types.NamespacedName
}
