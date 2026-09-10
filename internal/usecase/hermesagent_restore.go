package usecase

import (
	"context"
	"fmt"
	"time"

	agentsv1alpha1 "hermeum/hermes-agent-operator/api/v1alpha1"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	// condReasonSnapshotNotReady covers a restore source snapshot that is
	// missing or not ReadyToUse.
	condReasonSnapshotNotReady = "SnapshotNotReady"
	// condReasonSnapshotCRDAbsent covers a missing VolumeSnapshot CRD.
	condReasonSnapshotCRDAbsent = "VolumeSnapshotsNotInstalled"
)

// volumeSnapshotAPIGroupPtr points at the API group of the VolumeSnapshot
// kind referenced by PVC spec.dataSource.
var volumeSnapshotAPIGroupPtr = func() *string {
	g := "snapshot.storage.k8s.io"
	return &g
}()

// reconcileRestore provisions the agent data volume from the VolumeSnapshot
// named by persistence.existingSnapshot (issue #82). Unlike a one-shot
// restore, the field is a standing declaration parallel to existingClaim:
// the desired state is "the data volume is the PVC restored from this
// snapshot", and every reconcile converges to it.
//
//   - the snapshot must exist in the agent's namespace and be ReadyToUse;
//     otherwise a RestoreFailed condition is surfaced and the reconcile
//     requeues, leaving the agent on its current volume;
//   - the restored PVC is named <snapshot>-restore, created with the
//     snapshot as its dataSource, sized from the snapshot's restoreSize,
//     and owned by the agent. Provisioning happens while the agent keeps
//     running on its current volume;
//   - the StatefulSet volumeClaimTemplate is immutable, so an
//     empty-PVC-shaped StatefulSet is deleted once (a rolling update then
//     cannot reshape it); the next reconcile recreates it mounting the
//     restored PVC via an explicit volume;
//   - previous volumes are left in place; cleaning them up is the user's
//     responsibility. The snapshot itself is never modified or deleted.
func (u *HermesAgentUseCase) reconcileRestore(ctx context.Context, ha *agentsv1alpha1.HermesAgent) (ctrl.Result, error) {
	hp := ha.GetHermes().GetPersistence()
	source := hp.GetExistingSnapshot()
	if source == "" {
		if u.clearRestoreFailed(ctx, ha) {
			if err := u.kube.UpdateHermesAgentStatus(ctx, UpdateHermesAgentStatusParam{HermesAgent: ha}); err != nil {
				return ctrl.Result{RequeueAfter: 30 * time.Second}, err
			}
		}
		return ctrl.Result{}, nil
	}

	nsName := types.NamespacedName{Namespace: ha.Namespace, Name: ha.Name}
	sourceSnapshot, err := u.getReadySnapshot(ctx, ha, source)
	if err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}
	if sourceSnapshot == nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	restoredPVCName := buildRestoredPVCName(source)

	// Provision the restore PVC from the snapshot while the agent keeps
	// running on its current volume.
	restored, err := u.kube.GetPersistentVolumeClaim(ctx, GetPersistentVolumeClaimParam{
		NamespacedName: types.NamespacedName{Namespace: ha.Namespace, Name: restoredPVCName},
	})
	if err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}
	if restored == nil {
		desired := buildRestoredPVC(ha, sourceSnapshot)
		if err := u.kube.CreatePersistentVolumeClaimOwnedByHermesAgent(ctx, CreatePersistentVolumeClaimOfHermesAgentParam{
			HermesAgent:           ha,
			PersistentVolumeClaim: desired,
		}); err != nil && !apierrors.IsAlreadyExists(err) {
			return ctrl.Result{RequeueAfter: 30 * time.Second}, err
		}
		u.tel.Info(ctx, "Created restore PVC from VolumeSnapshot", "name", restoredPVCName, "snapshot", source)
		restored, err = u.kube.GetPersistentVolumeClaim(ctx, GetPersistentVolumeClaimParam{
			NamespacedName: types.NamespacedName{Namespace: ha.Namespace, Name: restoredPVCName},
		})
		if err != nil {
			return ctrl.Result{RequeueAfter: 30 * time.Second}, err
		}
		if restored == nil {
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}
	}
	if restored.Status.Phase != corev1.ClaimBound {
		u.tel.Info(ctx, "Waiting for restore PVC to bind", "name", restoredPVCName)
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	// Reshape gate (forward-only): an existing StatefulSet carries the
	// hermes-data volumeClaimTemplate, which is immutable — a plain update
	// cannot replace it with an explicit volume. Delete it once; the
	// StatefulSet reconcile recreates it mounting the restored PVC.
	sts, err := u.kube.GetStatefulSet(ctx, GetStatefulSetParam{NamespacedName: nsName})
	if err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}
	if sts != nil && statefulSetHasVolumeClaimTemplate(sts, hermesHomeVolume) {
		if err := u.kube.DeleteStatefulSet(ctx, DeleteStatefulSetParam{NamespacedName: nsName}); err != nil {
			return ctrl.Result{RequeueAfter: 30 * time.Second}, err
		}
		u.tel.Info(ctx, "Deleted StatefulSet to mount restore PVC", "name", ha.Name, "pvc", restoredPVCName)
		// Requeue so the deletion settles before the StatefulSet is recreated.
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	// The restored PVC is provisioned and the StatefulSet is out of the way
	// (or already mounts the restored volume): reconcileStatefulSet now
	// builds a StatefulSet with an explicit hermes-data volume, and the
	// agent rolls onto the restored data.
	if u.clearRestoreFailed(ctx, ha) {
		if err := u.kube.UpdateHermesAgentStatus(ctx, UpdateHermesAgentStatusParam{HermesAgent: ha}); err != nil {
			return ctrl.Result{RequeueAfter: 30 * time.Second}, err
		}
	}
	return ctrl.Result{}, nil
}

// getReadySnapshot returns the source VolumeSnapshot if it exists and is
// ReadyToUse. A missing or not-yet-ready snapshot sets the RestoreFailed
// condition and reports no error: existingSnapshot is a declaration, so the
// reconcile keeps requeueing until the snapshot becomes ready.
func (u *HermesAgentUseCase) getReadySnapshot(ctx context.Context, ha *agentsv1alpha1.HermesAgent, name string) (*VolumeSnapshot, error) {
	snapshot, err := u.kube.GetVolumeSnapshot(ctx, GetVolumeSnapshotParam{
		NamespacedName: types.NamespacedName{Namespace: ha.Namespace, Name: name},
	})
	if err != nil {
		if meta.IsNoMatchError(err) {
			u.setRestoreFailed(ctx, ha, condReasonSnapshotCRDAbsent, "The VolumeSnapshot CRD (snapshot.storage.k8s.io/v1) is not installed; install the snapshot-controller to enable restores")
			if err := u.kube.UpdateHermesAgentStatus(ctx, UpdateHermesAgentStatusParam{HermesAgent: ha}); err != nil {
				return nil, err
			}
			return nil, nil
		}
		return nil, err
	}
	if snapshot == nil || snapshot.Status == nil || snapshot.Status.ReadyToUse == nil || !*snapshot.Status.ReadyToUse {
		u.setRestoreFailed(ctx, ha, condReasonSnapshotNotReady, fmt.Sprintf("VolumeSnapshot %q not found or not ReadyToUse in namespace %q", name, ha.Namespace))
		if err := u.kube.UpdateHermesAgentStatus(ctx, UpdateHermesAgentStatusParam{HermesAgent: ha}); err != nil {
			return nil, err
		}
		return nil, nil
	}
	return snapshot, nil
}

// statefulSetHasVolumeClaimTemplate reports whether the live StatefulSet
// still provisions the named volume via a volumeClaimTemplate, i.e. cannot
// be updated in place to mount an explicit volume instead.
func statefulSetHasVolumeClaimTemplate(sts *appsv1.StatefulSet, name string) bool {
	for i := range sts.Spec.VolumeClaimTemplates {
		if sts.Spec.VolumeClaimTemplates[i].Name == name {
			return true
		}
	}
	return false
}

// buildRestoredPVCName returns the deterministic name of the PVC provisioned
// from a restore source snapshot: <snapshot>-restore.
func buildRestoredPVCName(snapshotName string) string {
	return fmt.Sprintf("%s-restore", snapshotName)
}

// buildRestoredPVC constructs the PVC provisioned from the source snapshot.
// The name derives from the snapshot; the size from the snapshot's
// restoreSize (no user-supplied size needed); the storage class comes from
// the spec's storageClassName, falling back to the cluster default.
// Ownership is applied by the infra layer, so the PVC is garbage-collected
// with the agent — unlike snapshots, a restored volume is reproducible data.
func buildRestoredPVC(ha *agentsv1alpha1.HermesAgent, snapshot *VolumeSnapshot) *corev1.PersistentVolumeClaim {
	hp := ha.GetHermes().GetPersistence()

	size := resource.MustParse("10Gi")
	if snapshot.Status != nil && snapshot.Status.RestoreSize != nil {
		size = *snapshot.Status.RestoreSize
	}

	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      buildRestoredPVCName(snapshot.Name),
			Namespace: ha.Namespace,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: size,
				},
			},
			StorageClassName: hp.StorageClassName,
			DataSource: &corev1.TypedLocalObjectReference{
				APIGroup: volumeSnapshotAPIGroupPtr,
				Kind:     "VolumeSnapshot",
				Name:     snapshot.Name,
			},
		},
	}
}

// setRestoreFailed sets (or updates) the RestoreFailed condition.
func (u *HermesAgentUseCase) setRestoreFailed(ctx context.Context, ha *agentsv1alpha1.HermesAgent, reason, message string) {
	meta.SetStatusCondition(&ha.Status.Conditions, metav1.Condition{
		Type:               string(agentsv1alpha1.ConditionRestoreFailed),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: ha.Generation,
		Reason:             reason,
		Message:            message,
	})
	u.tel.Info(ctx, "Restore failed for HermesAgent", "reason", reason)
}

// clearRestoreFailed removes the RestoreFailed condition if set and reports
// whether the status changed.
func (u *HermesAgentUseCase) clearRestoreFailed(ctx context.Context, ha *agentsv1alpha1.HermesAgent) bool {
	changed := meta.RemoveStatusCondition(&ha.Status.Conditions, string(agentsv1alpha1.ConditionRestoreFailed))
	if changed {
		u.tel.Info(ctx, "Cleared RestoreFailed condition for HermesAgent")
	}
	return changed
}
