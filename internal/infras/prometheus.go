package infras

import (
	"context"

	"noahingh/hermes-agent-operator/internal/usecase"

	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

const telemetryLoggerName = "hermesagent"

type PrometheusTelemetry struct {
	reconcileTotal    *prometheus.CounterVec
	reconcileDuration *prometheus.HistogramVec
	configMapOps      *prometheus.CounterVec
	statefulSetOps    *prometheus.CounterVec
	serviceAccountOps *prometheus.CounterVec
	roleOps           *prometheus.CounterVec
	roleBindingOps    *prometheus.CounterVec
	serviceOps        *prometheus.CounterVec
	ingressOps        *prometheus.CounterVec
	networkPolicyOps  *prometheus.CounterVec
	notFoundTotal     *prometheus.CounterVec
}

func NewPrometheusTelemetry() *PrometheusTelemetry {
	m := &PrometheusTelemetry{
		reconcileTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "hermesagent_reconcile_total",
			Help: "Total number of HermesAgent reconciliations.",
		}, []string{"namespace", "name", "result"}),
		reconcileDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "hermesagent_reconcile_duration_seconds",
			Help:    "Duration of HermesAgent reconciliations in seconds.",
			Buckets: prometheus.DefBuckets,
		}, []string{"namespace", "name"}),
		configMapOps: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "hermesagent_configmap_operations_total",
			Help: "Total number of ConfigMap create/update operations.",
		}, []string{"namespace", "name", "operation", "result"}),
		statefulSetOps: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "hermesagent_statefulset_operations_total",
			Help: "Total number of StatefulSet create/update operations.",
		}, []string{"namespace", "name", "operation", "result"}),
		serviceAccountOps: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "hermesagent_serviceaccount_operations_total",
			Help: "Total number of ServiceAccount create/update/delete operations.",
		}, []string{"namespace", "name", "operation", "result"}),
		roleOps: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "hermesagent_role_operations_total",
			Help: "Total number of Role create/update/delete operations.",
		}, []string{"namespace", "name", "operation", "result"}),
		roleBindingOps: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "hermesagent_rolebinding_operations_total",
			Help: "Total number of RoleBinding create/update/delete operations.",
		}, []string{"namespace", "name", "operation", "result"}),
		serviceOps: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "hermesagent_service_operations_total",
			Help: "Total number of Service create/update operations.",
		}, []string{"namespace", "name", "operation", "result"}),
		ingressOps: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "hermesagent_ingress_operations_total",
			Help: "Total number of Ingress create/update/delete operations.",
		}, []string{"namespace", "name", "operation", "result"}),
		networkPolicyOps: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "hermesagent_networkpolicy_operations_total",
			Help: "Total number of NetworkPolicy create/update/delete operations.",
		}, []string{"namespace", "name", "operation", "result"}),
		notFoundTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "hermesagent_not_found_total",
			Help: "Total number of reconciliations where the HermesAgent was not found.",
		}, []string{"namespace", "name"}),
	}

	metrics.Registry.MustRegister(
		m.reconcileTotal,
		m.reconcileDuration,
		m.configMapOps,
		m.statefulSetOps,
		m.serviceAccountOps,
		m.roleOps,
		m.roleBindingOps,
		m.serviceOps,
		m.ingressOps,
		m.networkPolicyOps,
		m.notFoundTotal,
	)

	return m
}

// debugVerbosity is the logr V-level used for debug logs. logr has no Debug
// method; higher V-levels are more verbose, and V(1) is the debug convention.
const debugVerbosity = 1

func (m *PrometheusTelemetry) Debug(ctx context.Context, msg string, keysAndValues ...any) {
	log.FromContext(ctx).WithName(telemetryLoggerName).V(debugVerbosity).Info(msg, keysAndValues...)
}

func (m *PrometheusTelemetry) Info(ctx context.Context, msg string, keysAndValues ...any) {
	log.FromContext(ctx).WithName(telemetryLoggerName).Info(msg, keysAndValues...)
}

func (m *PrometheusTelemetry) Error(ctx context.Context, err error, msg string, keysAndValues ...any) {
	log.FromContext(ctx).WithName(telemetryLoggerName).Error(err, msg, keysAndValues...)
}

func (m *PrometheusTelemetry) IncReconcile(_ context.Context, param usecase.IncReconcileParam) {
	m.reconcileTotal.WithLabelValues(param.NamespacedName.Namespace, param.NamespacedName.Name, param.Result.String()).Inc()
}

func (m *PrometheusTelemetry) ObserveReconcileDuration(_ context.Context, param usecase.ObserveReconcileDurationParam) {
	m.reconcileDuration.WithLabelValues(param.NamespacedName.Namespace, param.NamespacedName.Name).Observe(param.Seconds)
}

func (m *PrometheusTelemetry) IncConfigMapOperation(_ context.Context, param usecase.IncConfigMapOperationParam) {
	m.configMapOps.WithLabelValues(param.NamespacedName.Namespace, param.NamespacedName.Name, param.Operation.String(), param.Result.String()).Inc()
}

func (m *PrometheusTelemetry) IncStatefulSetOperation(_ context.Context, param usecase.IncStatefulSetOperationParam) {
	m.statefulSetOps.WithLabelValues(param.NamespacedName.Namespace, param.NamespacedName.Name, param.Operation.String(), param.Result.String()).Inc()
}

func (m *PrometheusTelemetry) IncServiceAccountOperation(_ context.Context, param usecase.IncServiceAccountOperationParam) {
	m.serviceAccountOps.WithLabelValues(param.NamespacedName.Namespace, param.NamespacedName.Name, param.Operation.String(), param.Result.String()).Inc()
}

func (m *PrometheusTelemetry) IncRoleOperation(_ context.Context, param usecase.IncRoleOperationParam) {
	m.roleOps.WithLabelValues(param.NamespacedName.Namespace, param.NamespacedName.Name, param.Operation.String(), param.Result.String()).Inc()
}

func (m *PrometheusTelemetry) IncRoleBindingOperation(_ context.Context, param usecase.IncRoleBindingOperationParam) {
	m.roleBindingOps.WithLabelValues(param.NamespacedName.Namespace, param.NamespacedName.Name, param.Operation.String(), param.Result.String()).Inc()
}

func (m *PrometheusTelemetry) IncServiceOperation(_ context.Context, param usecase.IncServiceOperationParam) {
	m.serviceOps.WithLabelValues(param.NamespacedName.Namespace, param.NamespacedName.Name, param.Operation.String(), param.Result.String()).Inc()
}

func (m *PrometheusTelemetry) IncIngressOperation(_ context.Context, param usecase.IncIngressOperationParam) {
	m.ingressOps.WithLabelValues(param.NamespacedName.Namespace, param.NamespacedName.Name, param.Operation.String(), param.Result.String()).Inc()
}

func (m *PrometheusTelemetry) IncNetworkPolicyOperation(_ context.Context, param usecase.IncNetworkPolicyOperationParam) {
	m.networkPolicyOps.WithLabelValues(param.NamespacedName.Namespace, param.NamespacedName.Name, param.Operation.String(), param.Result.String()).Inc()
}

func (m *PrometheusTelemetry) IncNotFound(_ context.Context, param usecase.IncNotFoundParam) {
	m.notFoundTotal.WithLabelValues(param.NamespacedName.Namespace, param.NamespacedName.Name).Inc()
}
