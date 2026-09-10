package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	agentsv1alpha1 "hermeum/hermes-agent-operator/api/v1alpha1"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// failingConfigMapKube fails ConfigMap creation and records every status write.
type failingConfigMapKube struct {
	fakeSnapshotKube
	getErr      error
	createErr   error
	statusErr   error
	statusCalls int
}

func (f *failingConfigMapKube) GetConfigMap(ctx context.Context, param GetConfigMapParam) (*corev1.ConfigMap, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return nil, nil
}

func (f *failingConfigMapKube) CreateConfigMapOwnedByHermesAgent(ctx context.Context, param CreateConfigMapOfHermesAgentParam) error {
	return f.createErr
}

func (f *failingConfigMapKube) UpdateHermesAgentStatus(ctx context.Context, param UpdateHermesAgentStatusParam) error {
	f.statusCalls++
	if f.statusErr != nil {
		return f.statusErr
	}
	return nil
}

// readyCondition returns the Ready condition of the agent, or nil.
func readyCondition(ha *agentsv1alpha1.HermesAgent) *metav1.Condition {
	return meta.FindStatusCondition(ha.Status.Conditions, string(agentsv1alpha1.ConditionReady))
}

func TestReconcileHermesConfigMap_FailureSetsReadyCondition(t *testing.T) {
	ctx := context.Background()
	createErr := errors.New("forbidden")
	kube := &failingConfigMapKube{createErr: createErr}
	uc := NewHermesAgentUseCase(kube, silentTelemetry{})

	ha := minimalHA()
	result, err := uc.reconcileHermesConfigMap(ctx, ha)
	if err == nil {
		t.Fatal("expected the original reconcile error to be returned")
	}
	if !errors.Is(err, createErr) {
		t.Errorf("expected original error, got %v", err)
	}
	if result.RequeueAfter != 30*time.Second {
		t.Errorf("expected 30s requeue, got %v", result.RequeueAfter)
	}

	cond := readyCondition(ha)
	if cond == nil {
		t.Fatal("expected Ready condition")
	}
	if cond.Status != metav1.ConditionFalse {
		t.Errorf("expected Ready=False, got %s", cond.Status)
	}
	if cond.Reason != condReasonHermesConfigMapFailed {
		t.Errorf("expected reason %s, got %s", condReasonHermesConfigMapFailed, cond.Reason)
	}
	if cond.ObservedGeneration != ha.Generation {
		t.Errorf("expected ObservedGeneration %d, got %d", ha.Generation, cond.ObservedGeneration)
	}
	if kube.statusCalls != 1 {
		t.Errorf("expected 1 status write on first failure, got %d", kube.statusCalls)
	}
}

func TestReconcileHermesConfigMap_RepeatedFailureDoesNotRewriteStatus(t *testing.T) {
	ctx := context.Background()
	kube := &failingConfigMapKube{createErr: errors.New("forbidden")}
	uc := NewHermesAgentUseCase(kube, silentTelemetry{})

	ha := minimalHA()
	for range 3 {
		if _, err := uc.reconcileHermesConfigMap(ctx, ha); err == nil {
			t.Fatal("expected error")
		}
	}
	if kube.statusCalls != 1 {
		t.Errorf("expected 1 status write across repeated identical failures, got %d", kube.statusCalls)
	}
}

func TestReconcileHermesConfigMap_StatusWriteFailureReturnsOriginalError(t *testing.T) {
	ctx := context.Background()
	createErr := errors.New("forbidden")
	kube := &failingConfigMapKube{
		createErr: createErr,
		statusErr: errors.New("conflict"),
	}
	uc := NewHermesAgentUseCase(kube, silentTelemetry{})

	ha := minimalHA()
	_, err := uc.reconcileHermesConfigMap(ctx, ha)
	if !errors.Is(err, createErr) {
		t.Errorf("expected the original reconcile error, got %v", err)
	}
	if errors.Is(err, kube.statusErr) {
		t.Error("status write failure must not mask the original error")
	}
}

func TestReconcileStatefulSet_SetstReadyTrueOnSuccess(t *testing.T) {
	ctx := context.Background()
	kube := &fakeSnapshotKube{}
	uc := NewHermesAgentUseCase(kube, silentTelemetry{})

	ha := minimalHA()
	// minimalHA has no RBAC service account name, so deriveStatus's GetPod
	// returns nil (pod missing → Pending) but reconciliation still succeeds.
	result, err := uc.reconcileStatefulSet(ctx, ha)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter != 10*time.Second {
		t.Errorf("expected 10s requeue for pending phase, got %v", result.RequeueAfter)
	}

	cond := readyCondition(ha)
	if cond == nil {
		t.Fatal("expected Ready condition")
	}
	if cond.Status != metav1.ConditionTrue {
		t.Errorf("expected Ready=True, got %s", cond.Status)
	}
	if cond.Reason != condReasonReconcileSucceeded {
		t.Errorf("expected reason %s, got %s", condReasonReconcileSucceeded, cond.Reason)
	}
	if len(kube.statuses) != 1 {
		t.Errorf("expected exactly 1 status write, got %d", len(kube.statuses))
	}
}

func TestReconcileStatefulSet_FailureSetsReadyCondition(t *testing.T) {
	ctx := context.Background()
	getErr := apierrors.NewNotFound(schema.GroupResource{Group: "apps", Resource: "statefulsets"}, "x")
	kube := &failingStatefulSetKube{getErr: getErr}
	uc := NewHermesAgentUseCase(kube, silentTelemetry{})

	ha := minimalHA()
	_, err := uc.reconcileStatefulSet(ctx, ha)
	if err == nil {
		t.Fatal("expected error")
	}

	cond := readyCondition(ha)
	if cond == nil {
		t.Fatal("expected Ready condition")
	}
	if cond.Status != metav1.ConditionFalse || cond.Reason != condReasonStatefulSetFailed {
		t.Errorf("unexpected condition: %+v", cond)
	}
}

// failingStatefulSetKube fails StatefulSet lookups.
type failingStatefulSetKube struct {
	fakeSnapshotKube
	getErr error
}

func (f *failingStatefulSetKube) GetStatefulSet(ctx context.Context, param GetStatefulSetParam) (*appsv1.StatefulSet, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return nil, nil
}

func TestSetReadyCondition_ChangeDetection(t *testing.T) {
	ha := minimalHA()

	if setReadyCondition(ha, metav1.ConditionTrue, condReasonReconcileSucceeded, "msg") != true {
		t.Error("first set must report changed")
	}
	// Identical set must report unchanged (no redundant status writes).
	if setReadyCondition(ha, metav1.ConditionTrue, condReasonReconcileSucceeded, "msg") != false {
		t.Error("identical condition must report unchanged")
	}
	// Only the reason changing must report changed.
	if setReadyCondition(ha, metav1.ConditionTrue, condReasonHermesConfigMapFailed, "msg") != true {
		t.Error("reason change must report changed")
	}
}
