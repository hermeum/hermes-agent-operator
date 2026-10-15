package usecase

import (
	"context"
	"time"

	agentsv1alpha1 "hermeum/hermes-agent-operator/api/v1alpha1"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

// Data-volume reshape (issue #133): a StatefulSet's volumeClaimTemplate is
// immutable, so any change to how the agent data volume (hermes-data) is
// provisioned must be applied by deleting the StatefulSet once and letting
// the StatefulSet reconcile recreate it with the desired shape — a plain
// update is rejected by the API server ("Forbidden: updates to statefulset
// spec"). The gate here covers every shape change; the restore loop
// (existingSnapshot) performs the same delete inline ahead of this gate, so
// no reconcile pass ever deletes the StatefulSet twice.
//
// PVCs provisioned through the template survive the delete (they are named
// hermes-data-<agent>-0 and outlive the StatefulSet), so enabling
// persistence keeps nothing but the emptyDir's ephemeral contents behind;
// disabling it leaves the provisioned PVC in place, unused but not lost.

// dataVolumeShape classifies how the hermes-data volume is provisioned by a
// StatefulSet shape — one value per buildHermesContainer branch.
type dataVolumeShape int

const (
	dataVolumeEmptyDir dataVolumeShape = iota // persistence off or unset: emptyDir volume
	dataVolumeClaim                           // persistence enabled: volumeClaimTemplate
	dataVolumeExplicit                        // existingClaim / existingSnapshot: explicit PVC volume
	dataVolumeNone                            // shape with no hermes-data volume at all (unexpected / hand-edited)
)

// dataVolumeShapeOf reports which branch buildHermesContainer renders for ha.
// The branch does not depend on suspension (a suspended agent still carries
// the shape in its template, at 0 replicas), so neither does the gate.
func dataVolumeShapeOf(ha *agentsv1alpha1.HermesAgent) dataVolumeShape {
	hp := ha.GetHermes().GetPersistence()
	if ec := hp.GetExistingClaim(); ec != "" {
		return dataVolumeExplicit
	}
	if es := hp.GetExistingSnapshot(); es != "" {
		return dataVolumeExplicit
	}
	if hp != nil && hp.Enabled {
		return dataVolumeClaim
	}
	return dataVolumeEmptyDir
}

// liveDataVolumeShape classifies how the StatefulSet currently provisions
// hermes-data, mirroring the buildHermesContainer branches.
func liveDataVolumeShape(sts *appsv1.StatefulSet) dataVolumeShape {
	if statefulSetHasVolumeClaimTemplate(sts) {
		return dataVolumeClaim
	}
	for i := range sts.Spec.Template.Spec.Volumes {
		if sts.Spec.Template.Spec.Volumes[i].Name == hermesHomeVolume {
			if sts.Spec.Template.Spec.Volumes[i].EmptyDir != nil {
				return dataVolumeEmptyDir
			}
			return dataVolumeExplicit
		}
	}
	return dataVolumeNone
}

// dataVolumeShapeNeedsReshape reports whether a StatefulSet currently
// provisioning hermes-data as live must be deleted before the desired shape
// can be applied: the API server only accepts updates that keep the
// volumeClaimTemplate set identical, i.e. every shape change but the
// identity. An emptyDir↔emptyDir pair (suspend/resume, config edits) is the
// identity and stays out of the gate.
func dataVolumeShapeNeedsReshape(live, desired dataVolumeShape) bool {
	return live != desired
}

// reconcileDataVolumeReshape deletes the live StatefulSet when its hermes-data
// volume cannot be reshaped in place to the desired provisioning. The
// non-zero result makes the whole reconcile return, so the next pass finds
// no StatefulSet and reconcileStatefulSet recreates it with the desired
// shape. The requeue between delete and recreate lets the deletion settle
// and old pods terminate, mirroring the restore reshape.
func (u *HermesAgentUseCase) reconcileDataVolumeReshape(
	ctx context.Context,
	ha *agentsv1alpha1.HermesAgent,
) (ctrl.Result, error) {
	nsName := types.NamespacedName{Namespace: ha.Namespace, Name: ha.Name}
	desired := dataVolumeShapeOf(ha)

	sts, err := u.kube.GetStatefulSet(ctx, GetStatefulSetParam{NamespacedName: nsName})
	if err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}
	if sts == nil {
		return ctrl.Result{}, nil
	}
	live := liveDataVolumeShape(sts)
	if !dataVolumeShapeNeedsReshape(live, desired) {
		return ctrl.Result{}, nil
	}

	if err := u.kube.DeleteStatefulSet(ctx, DeleteStatefulSetParam{NamespacedName: nsName}); err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}
	u.tel.Info(ctx, "Deleted StatefulSet to reshape the agent data volume",
		"name", ha.Name, "namespace", ha.Namespace,
		"from", dataVolumeShapeName(live), "to", dataVolumeShapeName(desired))
	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

// dataVolumeShapeName names a shape for logs — never part of the API.
func dataVolumeShapeName(s dataVolumeShape) string {
	switch s {
	case dataVolumeEmptyDir:
		return "emptyDir"
	case dataVolumeClaim:
		return "volumeClaimTemplate"
	case dataVolumeExplicit:
		return "explicitVolume"
	}
	return "none"
}
