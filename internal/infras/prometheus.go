package infras

import (
	"context"

	"hermeum/hermes-agent-operator/internal/usecase"

	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

const telemetryLoggerName = "hermesagent"

type PrometheusTelemetry struct {
	reconcileTotal    *prometheus.CounterVec
	reconcileDuration *prometheus.HistogramVec
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
		notFoundTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "hermesagent_not_found_total",
			Help: "Total number of reconciliations where the HermesAgent was not found.",
		}, []string{"namespace", "name"}),
	}

	metrics.Registry.MustRegister(
		m.reconcileTotal,
		m.reconcileDuration,
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

func (m *PrometheusTelemetry) IncNotFound(_ context.Context, param usecase.IncNotFoundParam) {
	m.notFoundTotal.WithLabelValues(param.NamespacedName.Namespace, param.NamespacedName.Name).Inc()
}
