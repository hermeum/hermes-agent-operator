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

	if err := u.applySnapshotRetention(ctx, snapshots, snap.GetRetention()); err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
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

	ha.Status.Snapshot.LastScheduleTime = &metav1.Time{Time: now}
	ha.Status.Snapshot.Snapshots = mergeSnapshotRefs(snapshots, pvcName, name, now, snap.GetRetention())
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

// applySnapshotRetention deletes all but the newest retention snapshots of
// this agent, ordered by creation timestamp. Snapshots without the agent
// label are never touched.
func (u *HermesAgentUseCase) applySnapshotRetention(ctx context.Context, snapshots []VolumeSnapshot, retention int) error {
	sorted := make([]VolumeSnapshot, len(snapshots))
	copy(sorted, snapshots)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].CreationTimestamp.After(sorted[j].CreationTimestamp.Time)
	})
	if len(sorted) <= retention {
		return nil
	}

	for _, old := range sorted[retention:] {
		err := u.kube.DeleteVolumeSnapshot(ctx, DeleteVolumeSnapshotParam{
			NamespacedName: types.NamespacedName{Namespace: old.Namespace, Name: old.Name},
		})
		if err != nil {
			return err
		}
		u.tel.Info(ctx, "Deleted expired VolumeSnapshot", "name", old.Name)
	}
	return nil
}

// mergeSnapshotRefs rebuilds status.snapshot.snapshots from the live
// VolumeSnapshot list plus the snapshot just taken, sorted newest first and
// capped at retention. Snapshots that were deleted (by retention or
// externally) disappear from the list; the agent label scopes the input.
func mergeSnapshotRefs(live []VolumeSnapshot, pvcName, newName string, newTime time.Time, retention int) []agentsv1alpha1.SnapshotRef {
	refs := make([]agentsv1alpha1.SnapshotRef, 0, len(live)+1)
	seen := map[string]bool{}
	for _, snap := range live {
		if snap.Name == "" || seen[snap.Name] {
			continue
		}
		created := snap.CreationTimestamp.Time
		if created.IsZero() {
			continue
		}
		seen[snap.Name] = true
		source := snap.Spec.Source.PersistentVolumeClaimName
		if source == "" {
			source = pvcName
		}
		refs = append(refs, agentsv1alpha1.SnapshotRef{
			Name:         snap.Name,
			PVC:          source,
			CreationTime: metav1.Time{Time: created},
		})
	}
	if !seen[newName] {
		refs = append(refs, agentsv1alpha1.SnapshotRef{
			Name:         newName,
			PVC:          pvcName,
			CreationTime: metav1.Time{Time: newTime},
		})
	}
	sort.SliceStable(refs, func(i, j int) bool {
		return refs[i].CreationTime.After(refs[j].CreationTime.Time)
	})
	if len(refs) > retention {
		refs = refs[:retention]
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
