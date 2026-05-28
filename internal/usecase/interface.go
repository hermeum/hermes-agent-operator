package usecase

import (
	"context"
	agentsv1alpha1 "noahingh/hermes-agent-operator/api/v1alpha1"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
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
	IncNotFound(ctx context.Context, param IncNotFoundParam)
}

type IncReconcileParam struct {
	Result Result
}

type ObserveReconcileDurationParam struct {
	Seconds float64
}

type IncConfigMapOperationParam struct {
	Operation Operation
	Result    Result
}

type IncStatefulSetOperationParam struct {
	Operation Operation
	Result    Result
}

type IncNotFoundParam struct{}

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
