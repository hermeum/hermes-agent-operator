package usecase

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/robfig/cron/v3"

	agentsv1alpha1 "hermeum/hermes-agent-operator/api/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	snapshotTimestampFormat = "20060102150405"

	condReasonCRDAbsent       = "VolumeSnapshotsNotInstalled"
	condReasonInvalidSchedule = "InvalidSchedule"
)

// VolumeSnapshot is a minimal typed representation of the CSI
// snapshot.storage.k8s.io/v1 VolumeSnapshot resource, covering the fields
// this operator reads and writes. Undeclared fields (e.g. status, added by
// the snapshot-controller) are ignored during typed/unstructured conversion.
type VolumeSnapshot struct {
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              VolumeSnapshotSpec `json:"spec"`
}

// VolumeSnapshotSpec is the spec of a CSI VolumeSnapshot.
type VolumeSnapshotSpec struct {
	Source                  VolumeSnapshotSource `json:"source"`
	VolumeSnapshotClassName *string              `json:"volumeSnapshotClassName,omitempty"`
}

// VolumeSnapshotSource identifies the source PVC of a VolumeSnapshot.
type VolumeSnapshotSource struct {
	PersistentVolumeClaimName string `json:"persistentVolumeClaimName"`
}

// reconcileSnapshot takes periodic CSI volume snapshots of the agent data PVC
// on a cron schedule. Scheduling is CronJob-style: the next run is computed
// from status.snapshot.lastScheduleTime, with a single catch-up snapshot when
// runs were missed. Snapshots are not owned by the agent, so deleting the
// agent never garbage-collects its backups.
func (u *HermesAgentUseCase) reconcileSnapshot(ctx context.Context, ha *agentsv1alpha1.HermesAgent) (ctrl.Result, error) {
	snap := ha.GetHermes().GetSnapshot()
	if !snap.IsEnabled() {
		if u.clearSnapshotUnsupported(ctx, ha) {
			if err := u.kube.UpdateHermesAgentStatus(ctx, UpdateHermesAgentStatusParam{HermesAgent: ha}); err != nil {
				return ctrl.Result{RequeueAfter: 30 * time.Second}, err
			}
		}
		return ctrl.Result{}, nil
	}

	if ha.IsSuspended() {
		u.tel.Debug(ctx, "Skipping snapshot because HermesAgent is suspended")
		return ctrl.Result{}, nil
	}

	pvcName := buildDataPVCName(ha)

	pvc, err := u.kube.GetPersistentVolumeClaim(ctx, GetPersistentVolumeClaimParam{
		NamespacedName: types.NamespacedName{Namespace: ha.Namespace, Name: pvcName},
	})
	if err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}
	if pvc == nil || pvc.Status.Phase != corev1.ClaimBound {
		u.tel.Debug(ctx, "Data PVC is not bound yet, skipping snapshot", "pvc", pvcName)
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	schedule, err := cron.ParseStandard(snap.GetSchedule())
	if err != nil {
		u.tel.Error(ctx, err, "Invalid snapshot schedule")
		u.setSnapshotUnsupported(ctx, ha, condReasonInvalidSchedule, fmt.Sprintf("Invalid schedule %q: %v", snap.GetSchedule(), err))
		if err := u.kube.UpdateHermesAgentStatus(ctx, UpdateHermesAgentStatusParam{HermesAgent: ha}); err != nil {
			return ctrl.Result{RequeueAfter: 30 * time.Second}, err
		}
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	snapshots, err := u.kube.ListVolumeSnapshotsOwnedByAgent(ctx, ListVolumeSnapshotsOwnedByAgentParam{
		Namespace: ha.Namespace,
		AgentName: ha.Name,
	})
	if err != nil {
		if meta.IsNoMatchError(err) {
			u.tel.Info(ctx, "VolumeSnapshot API is not available in the cluster")
			u.setSnapshotUnsupported(ctx, ha, condReasonCRDAbsent, "The VolumeSnapshot CRD (snapshot.storage.k8s.io/v1) is not installed; install the snapshot-controller to enable periodic backups")
			if err := u.kube.UpdateHermesAgentStatus(ctx, UpdateHermesAgentStatusParam{HermesAgent: ha}); err != nil {
				return ctrl.Result{RequeueAfter: 30 * time.Second}, err
			}
			return ctrl.Result{}, nil
		}
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}

	// The API is available; clear a stale unsupported condition.
	if u.clearSnapshotUnsupported(ctx, ha) {
		if err := u.kube.UpdateHermesAgentStatus(ctx, UpdateHermesAgentStatusParam{HermesAgent: ha}); err != nil {
			return ctrl.Result{RequeueAfter: 30 * time.Second}, err
		}
	}

	_, deprecated := splitSnapshots(snapshots, snap.GetRetention())

	// Delete outdated snapshots beyond the retention window.
	for _, old := range deprecated {
		if err := u.kube.DeleteVolumeSnapshot(ctx, DeleteVolumeSnapshotParam{
			NamespacedName: types.NamespacedName{Namespace: old.Namespace, Name: old.Name},
		}); err != nil {
			return ctrl.Result{RequeueAfter: 30 * time.Second}, err
		}
		u.tel.Info(ctx, "Deleted expired VolumeSnapshot", "name", old.Name)
	}

	now := time.Now()
	// Anchor the first run to the agent's creation so the schedule is
	// deterministic across restarts.
	last := ha.CreationTimestamp.Time
	if ha.Status.Snapshot.LastScheduleTime != nil {
		last = ha.Status.Snapshot.LastScheduleTime.Time
	}
	next := schedule.Next(last)
	if next.IsZero() {
		u.tel.Error(ctx, fmt.Errorf("schedule %q has no next run time", snap.GetSchedule()), "Could not compute next snapshot time")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}
	if next.After(now) {
		return ctrl.Result{RequeueAfter: next.Sub(now)}, nil
	}

	// The schedule is due (or was missed): take exactly one catch-up snapshot.
	// lastScheduleTime is advanced to now (not the missed slot) so a long
	// outage triggers a single catch-up snapshot rather than one per missed run.
	name := buildSnapshotName(pvcName, now)
	desired := buildSnapshot(ha, name)
	if err := u.kube.CreateVolumeSnapshotOwnedByHermesAgent(ctx, CreateVolumeSnapshotOfHermesAgentParam{HermesAgent: ha, VolumeSnapshot: desired}); err != nil && !apierrors.IsAlreadyExists(err) {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	} else if err == nil {
		u.tel.Info(ctx, "Created VolumeSnapshot", "name", name, "pvc", pvcName)
	}

	// Re-list so the status reflects the actual cluster state, including the
	// snapshot just created; splitSnapshots caps the retained set again (a
	// freshly deleted snapshot lingers in Terminating state until the
	// snapshot-controller clears its finalizer, but it is the oldest entry
	// and therefore falls out of the live bucket).
	snapshots, err = u.kube.ListVolumeSnapshotsOwnedByAgent(ctx, ListVolumeSnapshotsOwnedByAgentParam{
		Namespace: ha.Namespace,
		AgentName: ha.Name,
	})
	if err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}
	live, _ := splitSnapshots(snapshots, snap.GetRetention())

	ha.Status.Snapshot.LastScheduleTime = &metav1.Time{Time: now}
	ha.Status.Snapshot.Snapshots = snapshotRefs(live)
	if err := u.kube.UpdateHermesAgentStatus(ctx, UpdateHermesAgentStatusParam{HermesAgent: ha}); err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}

	nextRun := schedule.Next(now)
	if nextRun.IsZero() {
		return ctrl.Result{}, nil
	}
	return ctrl.Result{RequeueAfter: nextRun.Sub(now)}, nil
}

// buildDataPVCName resolves the name of the agent data PVC. A user-supplied
// existingClaim wins; otherwise the PVC is provisioned by the StatefulSet
// volumeClaimTemplate (hermesHomeVolume) and named <template-name>-<pod-name>,
// where the data pod is <agent>-0.
func buildDataPVCName(ha *agentsv1alpha1.HermesAgent) string {
	if ec := ha.GetHermes().GetPersistence().GetExistingClaim(); ec != "" {
		return ec
	}
	return fmt.Sprintf("%s-%s-0", hermesHomeVolume, ha.Name)
}

// buildSnapshotName returns the VolumeSnapshot name for a source PVC at the
// given time: <pvc>-<yyyymmddhhmmss> (UTC).
func buildSnapshotName(pvcName string, t time.Time) string {
	return fmt.Sprintf("%s-%s", pvcName, t.UTC().Format(snapshotTimestampFormat))
}

// buildSnapshot constructs the desired VolumeSnapshot object for the agent
// data PVC. The agent attribution label is applied by the infra layer
// (CreateVolumeSnapshotOwnedByHermesAgent); snapshots are intentionally NOT
// owner-referenced so deleting the agent never garbage-collects its backups.
func buildSnapshot(ha *agentsv1alpha1.HermesAgent, name string) *VolumeSnapshot {
	snapshot := &VolumeSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ha.Namespace,
		},
		Spec: VolumeSnapshotSpec{
			Source: VolumeSnapshotSource{
				PersistentVolumeClaimName: buildDataPVCName(ha),
			},
		},
	}
	if className := ha.GetHermes().GetSnapshot().GetVolumeSnapshotClassName(); className != nil && *className != "" {
		snapshot.Spec.VolumeSnapshotClassName = className
	}
	return snapshot
}

// splitSnapshots partitions snapshots into live (the newest retention,
// newest first) and deprecated (the rest, oldest last) by creation timestamp.
// The input must carry the agent attribution label, which scopes it to this
// agent's snapshots; everything else is never touched.
func splitSnapshots(snapshots []VolumeSnapshot, retention int) (live, deprecated []VolumeSnapshot) {
	sorted := make([]VolumeSnapshot, len(snapshots))
	copy(sorted, snapshots)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].CreationTimestamp.After(sorted[j].CreationTimestamp.Time)
	})
	if len(sorted) <= retention {
		return sorted, nil
	}
	return sorted[:retention], sorted[retention:]
}

// snapshotRef maps a VolumeSnapshot into its status representation.
func snapshotRef(snap VolumeSnapshot) agentsv1alpha1.SnapshotRef {
	return agentsv1alpha1.SnapshotRef{
		Name:         snap.Name,
		PVC:          snap.Spec.Source.PersistentVolumeClaimName,
		CreationTime: snap.CreationTimestamp,
	}
}

// snapshotRefs maps snapshots into status refs, preserving input order
// (newest first). Entries without a name or creation timestamp are skipped,
// as are snapshots being deleted (deletionTimestamp set): they are no longer
// retained backups.
func snapshotRefs(snapshots []VolumeSnapshot) []agentsv1alpha1.SnapshotRef {
	refs := make([]agentsv1alpha1.SnapshotRef, 0, len(snapshots))
	for _, snap := range snapshots {
		if snap.Name == "" || snap.CreationTimestamp.IsZero() || snap.DeletionTimestamp != nil {
			continue
		}
		refs = append(refs, snapshotRef(snap))
	}
	return refs
}

// setSnapshotUnsupported sets (or updates) the SnapshotUnsupported condition.
func (u *HermesAgentUseCase) setSnapshotUnsupported(ctx context.Context, ha *agentsv1alpha1.HermesAgent, reason, message string) {
	meta.SetStatusCondition(&ha.Status.Conditions, metav1.Condition{
		Type:               string(agentsv1alpha1.ConditionSnapshotUnsupported),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: ha.Generation,
		Reason:             reason,
		Message:            message,
	})
	u.tel.Info(ctx, "Snapshotting unsupported for HermesAgent", "reason", reason)
}

// clearSnapshotUnsupported removes the SnapshotUnsupported condition if set
// and reports whether the status changed.
func (u *HermesAgentUseCase) clearSnapshotUnsupported(ctx context.Context, ha *agentsv1alpha1.HermesAgent) bool {
	changed := meta.RemoveStatusCondition(&ha.Status.Conditions, string(agentsv1alpha1.ConditionSnapshotUnsupported))
	if changed {
		u.tel.Info(ctx, "Cleared SnapshotUnsupported condition for HermesAgent")
	}
	return changed
}
