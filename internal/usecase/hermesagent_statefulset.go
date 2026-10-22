package usecase

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"sort"
	"time"

	agentsv1alpha1 "hermeum/hermes-agent-operator/api/v1alpha1"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	hermesContainerName = "hermes-agent"
	// hermesHomeVolume is the StatefulSet volumeClaimTemplate name for the agent
	// data PVC. Package-level so other reconcilers (e.g. snapshots) can share it.
	hermesHomeVolume          = "hermes-data"
	hermesDefaultProfile      = "default"
	annotationDesiredSpecHash = domain + "/desired-spec-hash"
	// searxngUID/searxngGID are the uid/gid of the searxng image user
	// (upstream container/dist.dockerfile: COPY --chown=977:977).
	searxngUID = int64(977)
	searxngGID = int64(977)
)

// hermesHealthCheckCommand reports the gateway state regardless of which
// ports the gateway listens on, so probes keep working when the API server
// is disabled or moved to another port.
var hermesHealthCheckCommand = []string{"hermes", "gateway", "status"}

func (u *HermesAgentUseCase) reconcileStatefulSet(ctx context.Context, ha *agentsv1alpha1.HermesAgent) (result ctrl.Result, err error) {
	defer func() {
		if err != nil {
			err = u.markReconcileFailed(ctx, ha, condReasonStatefulSetFailed, err)
		}
	}()

	nsName := types.NamespacedName{Namespace: ha.Namespace, Name: ha.Name}

	if ha.UsesLegacyImageForm() {
		u.tel.Warn(ctx, "Image override uses the deprecated object form; set a plain string image reference (required in the next API version)",
			"HermesAgent", ha.Name, "Namespace", ha.Namespace)
	}

	sts, err := u.kube.GetStatefulSet(ctx, GetStatefulSetParam{
		NamespacedName: nsName,
	})
	if err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}

	// The bootstrap ConfigMap was reconciled before this loop, so the live
	// data matches the desired data; hashing the live object — the one the
	// pod actually mounts — keeps the config-hash annotation truthful.
	cmRef := ha.GetHermesConfigMapRef()
	cm, err := u.kube.GetConfigMap(ctx, GetConfigMapParam{
		NamespacedName: types.NamespacedName{Name: cmRef.Name, Namespace: cmRef.Namespace},
	})
	if err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}
	var cmData map[string]string
	if cm != nil {
		cmData = cm.Data
	}
	configHash := configMapDataHash(cmData)

	in := statefulSetInputs{configHash: configHash}
	if ha.GetEgress().IsEnabled() {
		in.egressTokenNames, in.egressHash, err = u.egressPodInputs(ctx, ha)
		if errors.Is(err, errEgressInputsPending) {
			u.tel.Debug(ctx, "Waiting for egress Secrets", "reason", err.Error())
			return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
		}
		if err != nil {
			return ctrl.Result{RequeueAfter: 30 * time.Second}, err
		}
	}

	desired := buildStatefulSet(ha, in)
	hash := desiredSpecHash(desired)
	if desired.Annotations == nil {
		desired.Annotations = map[string]string{}
	}
	// Store the hash of the desired spec as an annotation so the next reconcile
	// can compare against it instead of the live object. Comparing against the
	// live object causes spurious updates because Kubernetes defaults fields
	// (PodManagementPolicy, UpdateStrategy, etc.) that the operator never sets.
	desired.Annotations[annotationDesiredSpecHash] = hash

	if sts != nil {
		// volumeClaimTemplates are immutable on an existing StatefulSet, so a
		// change in their name set — adding, removing, or renaming storage —
		// cannot be applied by an update: the API server rejects the whole
		// object, including its pod-template changes, and nothing happens.
		// Delete the StatefulSet once; the next reconcile recreates it in the
		// desired shape. An emptyDir→PVC reshape loses nothing (the data was
		// ephemeral); a PVC→emptyDir reshape leaves the claim in place, as the
		// restore flow does.
		if !sameVolumeClaimTemplateNames(sts, desired) {
			if err := u.kube.DeleteStatefulSet(ctx, DeleteStatefulSetParam{NamespacedName: nsName}); err != nil {
				return ctrl.Result{RequeueAfter: 30 * time.Second}, err
			}
			u.tel.Info(ctx, "Deleted StatefulSet to reshape storage", "HermesAgent", ha.Name, "Namespace", ha.Namespace)
			// Requeue so the deletion settles before the StatefulSet is recreated.
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}
		if sts.Annotations[annotationDesiredSpecHash] != hash {
			pod, err := u.kube.GetPod(ctx, GetPodParam{
				NamespacedName: types.NamespacedName{Name: ha.Name + "-0", Namespace: ha.Namespace},
			})
			if err != nil {
				return ctrl.Result{RequeueAfter: 30 * time.Second}, err
			}
			podWasRunning := pod != nil && pod.Status.Phase == corev1.PodRunning

			desired.ResourceVersion = sts.ResourceVersion
			if err := u.kube.UpdateStatefulSetOwnedByHermesAgent(ctx, UpdateStatefulSetParam{HermesAgent: ha, StatefulSet: desired}); err != nil {
				return ctrl.Result{RequeueAfter: 30 * time.Second}, err
			}
			// If the pod was not running before the update, the StatefulSet controller
			// will not replace it on its own — the unavailability budget is already
			// exhausted. Delete it so it is recreated immediately at the new revision.
			if !podWasRunning {
				if err := u.kube.DeletePod(ctx, DeletePodParam{
					NamespacedName: types.NamespacedName{Name: ha.Name + "-0", Namespace: ha.Namespace},
				}); err != nil {
					return ctrl.Result{RequeueAfter: 30 * time.Second}, err
				}
			}
			u.tel.Debug(ctx, "StatefulSet updated", "phase", ha.Status.Phase)
		}
	} else {
		err = u.kube.CreateStatefulSetOwnedByHermesAgent(ctx, CreateStatefulSetOfHermesAgentParam{HermesAgent: ha, StatefulSet: desired})
		if err != nil {
			return ctrl.Result{RequeueAfter: 30 * time.Second}, err
		}
		u.tel.Debug(ctx, "StatefulSet created", "phase", ha.Status.Phase)
	}

	ha.Status.ManagedResources.StatefulSet = ha.Name
	phase, pod := u.deriveStatus(ctx, ha)
	initFailure := initContainerFailure(pod)
	ha.Status.Phase, ha.Status.Reason = phase, podStatusReason(pod, initFailure)
	u.applyInitFailedCondition(ctx, ha, initFailure)
	// The StatefulSet loop runs after every other resource loop, so reaching
	// this point means all managed resources reconciled. Workload readiness
	// itself is tracked by status.phase, not by this condition.
	u.markReady(ctx, ha)
	if err := u.kube.UpdateHermesAgentStatus(ctx, UpdateHermesAgentStatusParam{HermesAgent: ha}); err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}

	// if the StatefulSet is not ready, requeue to check again after a short delay.
	if ha.Status.Phase == agentsv1alpha1.PhasePending || ha.Status.Phase == agentsv1alpha1.PhaseUnknown {
		u.tel.Debug(ctx, "StatefulSet not ready", "phase", ha.Status.Phase)
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}
	return ctrl.Result{}, nil
}

// `deriveStatus` reports the agent's phase along with the `Pod` it was derived
// from, so that a caller needing more than the phase reads one `Pod` rather
// than fetching it again.  The `Pod` is nil when the agent is suspended, when
// it cannot be read, and before it exists.
func (u *HermesAgentUseCase) deriveStatus(ctx context.Context, ha *agentsv1alpha1.HermesAgent) (agentsv1alpha1.HermesAgentPhase, *corev1.Pod) {
	if ha.IsSuspended() {
		return agentsv1alpha1.PhaseSuspended, nil
	}
	pod, err := u.kube.GetPod(ctx, GetPodParam{
		NamespacedName: types.NamespacedName{Name: ha.Name + "-0", Namespace: ha.Namespace},
	})
	if err != nil {
		return agentsv1alpha1.PhaseUnknown, nil
	}
	if pod == nil {
		return agentsv1alpha1.PhasePending, nil
	}

	return hermesAgentPhase(pod), pod
}

func configMapDataHash(data map[string]string) string {
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, k := range keys {
		_, _ = fmt.Fprintf(h, "%s\x00%s\x00", k, data[k])
	}
	return fmt.Sprintf("%x", h.Sum(nil))[:16]
}

// desiredSpecHash hashes the operator-managed portions of a StatefulSet spec so that
// reconcile can detect changes without comparing against the live object (which carries
// Kubernetes-defaulted fields that buildStatefulSet does not set).
func desiredSpecHash(sts *appsv1.StatefulSet) string {
	data, _ := json.Marshal(struct {
		Replicas             *int32                         `json:"replicas"`
		Template             corev1.PodTemplateSpec         `json:"template"`
		VolumeClaimTemplates []corev1.PersistentVolumeClaim `json:"volumeClaimTemplates"`
	}{
		Replicas:             sts.Spec.Replicas,
		Template:             sts.Spec.Template,
		VolumeClaimTemplates: sts.Spec.VolumeClaimTemplates,
	})
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h[:])[:16]
}

// sameVolumeClaimTemplateNames reports whether the live and desired
// StatefulSets provision their data claims through the same set of
// volumeClaimTemplate names. The templates themselves are immutable on an
// existing StatefulSet, so only a change in that name set requires the
// delete-and-recreate reshape; a size change on a kept template flows through
// the normal update path instead, where the API server applies it on clusters
// with PVC auto-resize.
func sameVolumeClaimTemplateNames(live, desired *appsv1.StatefulSet) bool {
	if len(live.Spec.VolumeClaimTemplates) != len(desired.Spec.VolumeClaimTemplates) {
		return false
	}
	names := make(map[string]struct{}, len(live.Spec.VolumeClaimTemplates))
	for _, vct := range live.Spec.VolumeClaimTemplates {
		names[vct.Name] = struct{}{}
	}
	for _, vct := range desired.Spec.VolumeClaimTemplates {
		if _, ok := names[vct.Name]; !ok {
			return false
		}
	}
	return true
}

// statefulSetInputs carries what `buildStatefulSet` needs from live objects.
// `reconcileStatefulSet` reads them so that the builder stays pure.
type statefulSetInputs struct {
	// configHash is the hash of the bootstrap `ConfigMap` data the pod mounts,
	// injected as a pod template annotation so that editing the config
	// restarts the `Pod`: the init containers read the config once, at start.
	configHash string
	// egressTokenNames are the env var names the egress Secret holds proxy
	// tokens for.
	egressTokenNames []string
	// egressHash covers the egress inputs the pod reads only at start.
	egressHash string
}

// `buildStatefulSet` renders the desired `StatefulSet`.
func buildStatefulSet(ha *agentsv1alpha1.HermesAgent, in statefulSetInputs) *appsv1.StatefulSet {
	replicas := int32(1)
	if ha.IsSuspended() {
		replicas = int32(0)
	}

	// The config hash annotation is used to trigger a rolling update of the StatefulSet when the config changes.
	maxUnavailable := intstr.FromInt32(1)
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ha.Name,
			Namespace: ha.Namespace,
			Labels:    ha.ResourceLabels(),
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: ha.SelectorLabels(),
			},
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: appsv1.RollingUpdateStatefulSetStrategyType,
				RollingUpdate: &appsv1.RollingUpdateStatefulSetStrategy{
					MaxUnavailable: &maxUnavailable,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: ha.PodTemplateLabels(),
					Annotations: map[string]string{
						domain + "/config-hash": in.configHash,
					},
				},
				Spec: corev1.PodSpec{
					HostUsers:         ha.GetHostUsers(),
					PriorityClassName: ha.GetPriorityClassName(),
					RuntimeClassName:  ha.GetRuntimeClassName(),
					SecurityContext: &corev1.PodSecurityContext{
						SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
					},
					ServiceAccountName: ha.GetServiceAccountName(),
				},
			},
		},
	}

	maps.Copy(sts.Spec.Template.Annotations, ha.GetPodAnnotations())

	sts = buildHermesContainer(ha, sts)
	sts = buildSearXNGContainer(ha, sts)
	sts = buildCamofoxContainer(ha, sts)
	sts = buildEgressContainer(ha, sts, in.egressTokenNames, in.egressHash)

	// additional user-provided init containers run after the operator-managed ones.
	sts.Spec.Template.Spec.InitContainers = append(sts.Spec.Template.Spec.InitContainers, ha.GetInitContainers()...)

	// additional user-provided sidecar containers run alongside the hermes-agent container.
	sts.Spec.Template.Spec.Containers = append(sts.Spec.Template.Spec.Containers, ha.GetSidecars()...)

	// additional user-provided volumes.
	sts.Spec.Template.Spec.Volumes = append(sts.Spec.Template.Spec.Volumes, ha.GetExtraVolumes()...)

	return sts
}

func findContainer(sts *appsv1.StatefulSet, name string) *corev1.Container {
	for i := range sts.Spec.Template.Spec.Containers {
		if sts.Spec.Template.Spec.Containers[i].Name == name {
			return &sts.Spec.Template.Spec.Containers[i]
		}
	}
	return nil
}
