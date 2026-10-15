package usecase

import (
	"context"
	"testing"
	"time"

	agentsv1alpha1 "hermeum/hermes-agent-operator/api/v1alpha1"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// persistenceHA builds a HermesAgent with persistence enabled at the given
// size ("" keeps the default).
func persistenceHA(size string) *agentsv1alpha1.HermesAgent {
	ha := minimalHA()
	ha.Spec.Hermes = &agentsv1alpha1.Hermes{
		Storage: &agentsv1alpha1.HermesStorage{
			Persistence: &agentsv1alpha1.HermesPersistence{Enabled: true},
		},
	}
	if size != "" {
		ha.Spec.Hermes.Storage.Persistence.Size = resourceQty(size)
	}
	return ha
}

// emptyDirStatefulSet builds the live StatefulSet shape an agent has before
// persistence is enabled: hermes-data is an emptyDir volume.
func emptyDirStatefulSet() *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: appsv1.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{
						{Name: hermesHomeVolume, VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
					},
				},
			},
		},
	}
}

func TestDataVolumeShapeOf(t *testing.T) {
	t.Run("persistence enabled is the volumeClaimTemplate shape", func(t *testing.T) {
		if got := dataVolumeShapeOf(persistenceHA("")); got != dataVolumeClaim {
			t.Errorf("dataVolumeShapeOf(enabled) = %v, want dataVolumeClaim", got)
		}
	})
	t.Run("no persistence is the emptyDir shape", func(t *testing.T) {
		if got := dataVolumeShapeOf(minimalHA()); got != dataVolumeEmptyDir {
			t.Errorf("dataVolumeShapeOf(minimal) = %v, want dataVolumeEmptyDir", got)
		}
	})
	t.Run("existingClaim is the explicit-volume shape", func(t *testing.T) {
		ha := minimalHA()
		ha.Spec.Hermes = &agentsv1alpha1.Hermes{
			Storage: &agentsv1alpha1.HermesStorage{
				Persistence: &agentsv1alpha1.HermesPersistence{
					Enabled:       true,
					ExistingClaim: ptrString("my-pvc"),
				},
			},
		}
		if got := dataVolumeShapeOf(ha); got != dataVolumeExplicit {
			t.Errorf("dataVolumeShapeOf(existingClaim) = %v, want dataVolumeExplicit", got)
		}
	})
	t.Run("existingSnapshot is the explicit-volume shape", func(t *testing.T) {
		if got := dataVolumeShapeOf(restoreHA("snap-1")); got != dataVolumeExplicit {
			t.Errorf("dataVolumeShapeOf(existingSnapshot) = %v, want dataVolumeExplicit", got)
		}
	})
	t.Run("suspension keeps the branch of the persistence spec", func(t *testing.T) {
		ha := persistenceHA("")
		suspend := true
		ha.Spec.Suspend = &suspend
		if got := dataVolumeShapeOf(ha); got != dataVolumeClaim {
			t.Errorf("suspension must not change the desired shape, got %v", got)
		}
	})
}

func TestLiveDataVolumeShape(t *testing.T) {
	t.Run("volumeClaimTemplate detected", func(t *testing.T) {
		if got := liveDataVolumeShape(vctStatefulSet()); got != dataVolumeClaim {
			t.Errorf("liveDataVolumeShape(vct) = %v, want dataVolumeClaim", got)
		}
	})
	t.Run("emptyDir volume detected", func(t *testing.T) {
		if got := liveDataVolumeShape(emptyDirStatefulSet()); got != dataVolumeEmptyDir {
			t.Errorf("liveDataVolumeShape(emptyDir) = %v, want dataVolumeEmptyDir", got)
		}
	})
	t.Run("explicit PVC volume detected", func(t *testing.T) {
		sts := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			Spec: appsv1.StatefulSetSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Volumes: []corev1.Volume{
							{Name: hermesHomeVolume, VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "restored"},
							}},
						},
					},
				},
			},
		}
		if got := liveDataVolumeShape(sts); got != dataVolumeExplicit {
			t.Errorf("liveDataVolumeShape(explicit) = %v, want dataVolumeExplicit", got)
		}
	})
	t.Run("missing volume", func(t *testing.T) {
		sts := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		}
		if got := liveDataVolumeShape(sts); got != dataVolumeNone {
			t.Errorf("liveDataVolumeShape(none) = %v, want dataVolumeNone", got)
		}
	})
}

// TestReconcileDataVolumeReshape_EmptyDirToClaim is the unit form of issue
// #133: an agent that ran without persistence gets persistence enabled. The
// StatefulSet must be deleted once (the volumeClaimTemplate cannot be added
// by an update); the recreated StatefulSet then carries the volumeClaimTemplate.
func TestReconcileDataVolumeReshape_EmptyDirToClaim(t *testing.T) {
	ctx := context.Background()
	kube := newFakeRestoreKube()
	kube.sts = emptyDirStatefulSet()
	uc := NewHermesAgentUseCase(kube, silentTelemetry{})
	ha := persistenceHA("2Gi")

	result, err := uc.reconcileDataVolumeReshape(ctx, ha)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if kube.stsDeletes != 1 {
		t.Fatalf("expected StatefulSet deleted once, got %d", kube.stsDeletes)
	}
	if result.RequeueAfter != 30*time.Second {
		t.Errorf("expected requeue after reshape, got %v", result)
	}

	// Next pass: StatefulSet gone, nothing left to delete — the zero result
	// lets reconcileStatefulSet recreate it with the volumeClaimTemplate.
	kube.sts = nil
	result, err = uc.reconcileDataVolumeReshape(ctx, ha)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsZero() {
		t.Errorf("expected zero result, got %v", result)
	}
	if kube.stsDeletes != 1 {
		t.Errorf("reshape must be one-shot, stsDeletes=%d", kube.stsDeletes)
	}
}

// TestReconcileDataVolumeReshape_ClaimToEmptyDir covers disabling
// persistence again: removing the volumeClaimTemplate is equally forbidden
// in place, so the gate must fire for the reverse transition too.
func TestReconcileDataVolumeReshape_ClaimToEmptyDir(t *testing.T) {
	ctx := context.Background()
	kube := newFakeRestoreKube()
	kube.sts = vctStatefulSet()
	uc := NewHermesAgentUseCase(kube, silentTelemetry{})
	ha := minimalHA()

	if _, err := uc.reconcileDataVolumeReshape(ctx, ha); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if kube.stsDeletes != 1 {
		t.Errorf("expected StatefulSet deleted once, got %d", kube.stsDeletes)
	}
}

// TestReconcileDataVolumeReshape_EmptyDirToExplicit covers switching from
// persistence-by-template to existingClaim: the template must be removed.
func TestReconcileDataVolumeReshape_EmptyDirToExplicit(t *testing.T) {
	ctx := context.Background()
	kube := newFakeRestoreKube()
	kube.sts = emptyDirStatefulSet()
	uc := NewHermesAgentUseCase(kube, silentTelemetry{})
	ha := restoreHA("snap-1") // existingSnapshot → explicit-volume shape

	if _, err := uc.reconcileDataVolumeReshape(ctx, ha); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if kube.stsDeletes != 1 {
		t.Errorf("expected StatefulSet deleted once, got %d", kube.stsDeletes)
	}
}

// TestReconcileDataVolumeReshape_SameShapeIsNoOp pins the identity: a
// StatefulSet already provisioning the desired shape must never be deleted.
func TestReconcileDataVolumeReshape_SameShapeIsNoOp(t *testing.T) {
	ctx := context.Background()

	t.Run("claim on claim", func(t *testing.T) {
		kube := newFakeRestoreKube()
		kube.sts = vctStatefulSet()
		uc := NewHermesAgentUseCase(kube, silentTelemetry{})

		if _, err := uc.reconcileDataVolumeReshape(ctx, persistenceHA("")); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if kube.stsDeletes != 0 {
			t.Errorf("matching shapes must not delete the StatefulSet, got %d deletes", kube.stsDeletes)
		}
	})
	t.Run("emptyDir on emptyDir", func(t *testing.T) {
		kube := newFakeRestoreKube()
		kube.sts = emptyDirStatefulSet()
		uc := NewHermesAgentUseCase(kube, silentTelemetry{})

		if _, err := uc.reconcileDataVolumeReshape(ctx, minimalHA()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if kube.stsDeletes != 0 {
			t.Errorf("matching shapes must not delete the StatefulSet, got %d deletes", kube.stsDeletes)
		}
	})
}

// TestReconcileDataVolumeReshape_MissingStatefulSetIsNoOp guards the create
// path: no live StatefulSet, nothing to reshape.
func TestReconcileDataVolumeReshape_MissingStatefulSetIsNoOp(t *testing.T) {
	ctx := context.Background()
	kube := newFakeRestoreKube()
	uc := NewHermesAgentUseCase(kube, silentTelemetry{})

	result, err := uc.reconcileDataVolumeReshape(ctx, persistenceHA(""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsZero() {
		t.Errorf("expected zero result, got %v", result)
	}
	if kube.stsDeletes != 0 {
		t.Errorf("nothing to delete without a StatefulSet, got %d deletes", kube.stsDeletes)
	}
}
