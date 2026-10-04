/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	"maps"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// HermesAgentPhase represents the lifecycle phase of a HermesAgent, mirroring Pod phase with an added Suspended state.
type HermesAgentPhase string

const (
	// PhasePending means the pod has not been scheduled yet, or is waiting to start.
	PhasePending HermesAgentPhase = "Pending"
	// PhaseRunning means the pod is running.
	PhaseRunning HermesAgentPhase = "Running"
	// PhaseSucceeded means the pod has terminated successfully.
	PhaseSucceeded HermesAgentPhase = "Succeeded"
	// PhaseFailed means the pod has terminated with a failure.
	PhaseFailed HermesAgentPhase = "Failed"
	// PhaseUnknown means the pod phase cannot be determined.
	PhaseUnknown HermesAgentPhase = "Unknown"
	// PhaseSuspended means the agent has been suspended via Spec.Suspend.
	PhaseSuspended HermesAgentPhase = "Suspended"
)

// HermesAgentConditionType is a type of HermesAgent condition.
type HermesAgentConditionType string

const (
	// ConditionReady indicates whether the operator reconciled all managed
	// resources successfully in the most recent reconcile pass. It reflects
	// controller reconciliation, not workload health (see status.phase).
	ConditionReady HermesAgentConditionType = "Ready"
	// ConditionSnapshotUnsupported indicates that periodic snapshots are
	// configured but the cluster cannot support them (e.g. the VolumeSnapshot
	// CRD or snapshot-controller is not installed).
	ConditionSnapshotUnsupported HermesAgentConditionType = "SnapshotUnsupported"
	// ConditionRestoreFailed indicates that the data volume configured via
	// persistence.existingSnapshot cannot be provisioned (e.g. the referenced
	// VolumeSnapshot does not exist or is not ReadyToUse). The condition is
	// cleared once the restored PVC is provisioned or the field is unset.
	ConditionRestoreFailed HermesAgentConditionType = "RestoreFailed"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// HermesAgentSpec defines the desired state of HermesAgent
type HermesAgentSpec struct {
	// suspend pauses the agent by scaling its StatefulSet to 0 replicas.
	// Set to true to pause; false or omit to run normally.
	// +optional
	Suspend *bool `json:"suspend,omitempty"`

	// hermes defines the Hermes agent configuration.
	// +optional
	Hermes *Hermes `json:"hermes,omitempty"`

	// security configures the pod and container security contexts.
	// +optional
	Security *HermesSecurity `json:"security,omitempty"`

	// networking configures the Service and Ingress.
	// +optional
	Networking *Networking `json:"networking,omitempty"`

	// InitContainers is a list of additional init containers to run before the main container.
	// They run after the operator-managed init-hermes and init-profile-<name> containers.
	// +kubebuilder:validation:MaxItems=10
	// +optional
	InitContainers []corev1.Container `json:"initContainers,omitempty"`

	// Sidecars is a list of additional sidecar containers to inject into the pod.
	// Use this for custom sidecars like database proxies, log forwarders, or service meshes.
	// +optional
	Sidecars []corev1.Container `json:"sidecars,omitempty"`

	// ExtraVolumes is a list of additional volumes to make available to the pod's containers.
	// +optional
	ExtraVolumes []corev1.Volume `json:"extraVolumes,omitempty"`

	// ExtraVolumeMounts adds additional volume mounts to the main container.
	// Use with ExtraVolumes to mount ConfigMaps, Secrets, NFS shares, or CSI volumes.
	// +kubebuilder:validation:MaxItems=10
	// +optional
	ExtraVolumeMounts []corev1.VolumeMount `json:"extraVolumeMounts,omitempty"`

	// HostUsers selects the user namespace for the Hermes agent pod.  Set it
	// to false to give the pod its own user namespace, so that root inside
	// the container maps to an unprivileged UID on the node and a container
	// breakout does not land on the node as root.  Omit the field to share
	// the host user namespace, which is the Kubernetes default.
	// +optional
	HostUsers *bool `json:"hostUsers,omitempty"`

	// PodAnnotations adds custom annotations to the Hermes agent pod template.
	// Changing any key (e.g. a timestamp) triggers a rolling restart of the
	// StatefulSet's pods, mirroring `kubectl rollout restart statefulset`.
	// +optional
	PodAnnotations map[string]string `json:"podAnnotations,omitempty"`

	// PodLabels adds labels to the Hermes agent pod template.  The
	// operator-managed `app.kubernetes.io/name`, `app.kubernetes.io/instance`
	// and `app.kubernetes.io/managed-by` labels are applied last and always
	// win.  An entry here cannot shadow one of them, and cannot break the
	// `StatefulSet` pod selector.  A change to any key starts a rolling
	// restart of the pods.
	// +optional
	PodLabels map[string]string `json:"podLabels,omitempty"`

	// PriorityClassName sets the PriorityClass name for the Hermes agent pod.
	// The named PriorityClass must already exist in the cluster.  Omit this
	// field to use the cluster's globalDefault PriorityClass, if one is
	// configured.
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
	// +kubebuilder:validation:MaxLength=253
	// +optional
	PriorityClassName string `json:"priorityClassName,omitempty"`

	// RuntimeClassName sets the RuntimeClass name for the Hermes agent pod.
	// The named RuntimeClass must already exist in the cluster.  Omit this
	// field to run the pod on the cluster's default container runtime.
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
	// +kubebuilder:validation:MaxLength=253
	// +optional
	RuntimeClassName *string `json:"runtimeClassName,omitempty"`

	// SearXNG configures an optional SearXNG sidecar used by the web_search tool.
	// +optional
	SearXNG *SearXNG `json:"searxng,omitempty"`

	// Camofox configures an optional Camofox sidecar used by the browser automation tool.
	// +optional
	Camofox *Camofox `json:"camofox,omitempty"`
}

// ManagedResources lists the Kubernetes resources currently owned by this HermesAgent.
// Each field holds the name of the managed resource; omitted when the resource is not active.
// Rebuilt on every reconcile.
type ManagedResources struct {
	// hermesConfigMap is the name of the Hermes configuration ConfigMap.
	// +optional
	HermesConfigMap string `json:"hermesConfigMap,omitempty"`
	// hermesSecret is the name of the Hermes API key Secret.
	// +optional
	HermesSecret string `json:"hermesSecret,omitempty"`
	// searxngConfigMap is the name of the SearXNG configuration ConfigMap.
	// +optional
	SearXNGConfigMap string `json:"searxngConfigMap,omitempty"`
	// searxngSecret is the name of the SearXNG Secret.
	// +optional
	SearXNGSecret string `json:"searxngSecret,omitempty"`
	// serviceAccount is the name of the managed ServiceAccount.
	// +optional
	ServiceAccount string `json:"serviceAccount,omitempty"`
	// role is the name of the managed Role.
	// +optional
	Role string `json:"role,omitempty"`
	// roleBinding is the name of the managed RoleBinding.
	// +optional
	RoleBinding string `json:"roleBinding,omitempty"`
	// service is the name of the managed Service.
	// +optional
	Service string `json:"service,omitempty"`
	// ingress is the name of the managed Ingress.
	// +optional
	Ingress string `json:"ingress,omitempty"`
	// networkPolicy is the name of the managed NetworkPolicy.
	// +optional
	NetworkPolicy string `json:"networkPolicy,omitempty"`
	// statefulSet is the name of the managed StatefulSet.
	// +optional
	StatefulSet string `json:"statefulSet,omitempty"`
}

// HermesAgentStatus defines the observed state of HermesAgent.
type HermesAgentStatus struct {
	// phase is the current lifecycle phase of the HermesAgent, mirroring the underlying pod phase.
	// One of Pending, Running, Succeeded, Failed, Unknown, or Suspended.
	// +optional
	Phase HermesAgentPhase `json:"phase,omitempty"`

	// reason is a short, CamelCase code indicating why the agent is in its current phase.
	// Populated when the pod is pending (e.g. "Unschedulable") or unhealthy
	// (e.g. "CrashLoopBackOff", "OOMKilled"). Empty when the agent is running normally.
	// +optional
	Reason string `json:"reason,omitempty"`

	// conditions represent the current state of the HermesAgent resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Standard condition types include:
	// - "Available": the resource is fully functional
	// - "Progressing": the resource is being created or updated
	// - "Degraded": the resource failed to reach or maintain its desired state
	//
	// The status of each condition is one of True, False, or Unknown.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// managedResources describes the Kubernetes resources currently owned by this HermesAgent.
	// +optional
	ManagedResources ManagedResources `json:"managedResources,omitempty"`

	// snapshot tracks the state of periodic CSI volume snapshots of the
	// agent data PVC.
	// +optional
	Snapshot SnapshotStatus `json:"snapshot,omitempty"`
}

// SnapshotStatus tracks periodic CSI volume snapshot scheduling for the agent data PVC.
type SnapshotStatus struct {
	// lastScheduleTime is the scheduled time of the most recently taken (or
	// accounted for) snapshot, used to compute the next run.
	// +optional
	LastScheduleTime *metav1.Time `json:"lastScheduleTime,omitempty"`
	// snapshots lists the VolumeSnapshots currently retained for this agent,
	// newest first. Mirrors the retention policy: snapshots removed by
	// retention are also removed from this list.
	// +optional
	// +listType=map
	// +listMapKey=name
	Snapshots []SnapshotRef `json:"snapshots,omitempty"`
}

// SnapshotRef identifies a single VolumeSnapshot of the agent data PVC.
type SnapshotRef struct {
	// name is the VolumeSnapshot name.
	// +required
	Name string `json:"name"`
	// pvc is the source PersistentVolumeClaim the snapshot was taken from.
	// +required
	PVC string `json:"pvc"`
	// creationTime is when the VolumeSnapshot was created.
	// +required
	CreationTime metav1.Time `json:"creationTime"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// HermesAgent is the Schema for the hermesagents API
type HermesAgent struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of HermesAgent
	// +required
	Spec HermesAgentSpec `json:"spec"`

	// status defines the observed state of HermesAgent
	// +optional
	Status HermesAgentStatus `json:"status,omitzero"`
}

func (h *HermesAgent) IsSuspended() bool {
	return h.Spec.Suspend != nil && *h.Spec.Suspend
}

func (h *HermesAgent) GetHermesName() string {
	return h.Name + "-hermes"
}

func (h *HermesAgent) GetServiceAccountName() string {
	r := h.GetSecurity().GetRBAC()
	if r.ShouldCreateServiceAccount() {
		return h.Name
	}
	if r != nil {
		return r.ServiceAccountName
	}
	return ""
}

func (h *HermesAgent) GetHermes() *Hermes {
	return h.Spec.Hermes
}

// `GetConfigSourceConfigMapNames` returns the name of every `ConfigMap` the
// operator reads a config document from, for the default profile and for each
// named profile.  Names may repeat when profiles share a `ConfigMap`.
func (h *HermesAgent) GetConfigSourceConfigMapNames() []string {
	if h == nil {
		return nil
	}
	var names []string
	if ref := h.GetHermes().GetConfigMapRef(); ref != nil {
		names = append(names, ref.Name)
	}
	for _, profile := range h.GetHermes().GetProfiles() {
		if ref := profile.Config.GetConfigMapRef(); ref != nil {
			names = append(names, ref.Name)
		}
	}
	return names
}

func (h *HermesAgent) GetSecurity() *HermesSecurity {
	return h.Spec.Security
}

func (h *HermesAgent) GetNetworking() *Networking {
	return h.Spec.Networking
}

func (h *HermesAgent) GetInitContainers() []corev1.Container {
	return h.Spec.InitContainers
}

func (h *HermesAgent) GetSidecars() []corev1.Container {
	return h.Spec.Sidecars
}

func (h *HermesAgent) GetExtraVolumes() []corev1.Volume {
	return h.Spec.ExtraVolumes
}

func (h *HermesAgent) GetExtraVolumeMounts() []corev1.VolumeMount {
	return h.Spec.ExtraVolumeMounts
}

// GetHostUsers returns the user-namespace selection for the agent pod.  It
// returns nil when the field is unset, which leaves the pod in the host user
// namespace.
func (h *HermesAgent) GetHostUsers() *bool {
	return h.Spec.HostUsers
}

// GetPodAnnotations returns the custom pod template annotations, if any.
func (h *HermesAgent) GetPodAnnotations() map[string]string {
	return h.Spec.PodAnnotations
}

// GetPodLabels returns the custom pod template labels, if any.
func (h *HermesAgent) GetPodLabels() map[string]string {
	return h.Spec.PodLabels
}

// GetPriorityClassName returns the PriorityClass name for the agent pod.  It
// returns "" when the field is unset.
func (h *HermesAgent) GetPriorityClassName() string {
	return h.Spec.PriorityClassName
}

// GetRuntimeClassName returns the RuntimeClass name for the agent pod.  It
// returns nil when the field is unset.
func (h *HermesAgent) GetRuntimeClassName() *string {
	return h.Spec.RuntimeClassName
}

func (h *HermesAgent) GetSearXNG() *SearXNG {
	return h.Spec.SearXNG
}

// GetSearXNGName returns the name shared by the operator-managed SearXNG
// ConfigMap and Secret.
func (h *HermesAgent) GetSearXNGName() string {
	return h.Name + "-searxng"
}

// GetCamofox returns the Camofox sidecar configuration, if any.
func (h *HermesAgent) GetCamofox() *Camofox {
	return h.Spec.Camofox
}

// GetCamofoxName returns the name used for the Camofox PersistentVolumeClaim.
func (h *HermesAgent) GetCamofoxName() string {
	return h.Name + "-camofox"
}

// Standard resource labels applied to every resource the operator manages.
const (
	LabelName      = "app.kubernetes.io/name"
	LabelInstance  = "app.kubernetes.io/instance"
	LabelManagedBy = "app.kubernetes.io/managed-by"

	AppNameValue   = "hermes-agent"
	ManagedByValue = "hermes-agent-operator"
)

// Shared operator-managed values referenced by both the bootstrap ConfigMap
// data and the StatefulSet wiring.
const (
	// SearXNGURL is the in-pod URL the hermes-agent uses to reach the SearXNG sidecar.
	SearXNGURL = "http://localhost:8080"
	// CamofoxURL is the in-pod URL the hermes-agent uses to reach the Camofox sidecar.
	CamofoxURL = "http://localhost:9377"
	// HermesWorkspacePathSeparator replaces "/" in workspace-file ConfigMap keys.
	HermesWorkspacePathSeparator = "--"
)

// HermesDataVolumeName is the StatefulSet volumeClaimTemplate name for the
// agent data PVC, shared by the reconcilers that mount or snapshot it.
const HermesDataVolumeName = "hermes-data"

// ResourceLabels returns the labels applied to every resource the operator
// manages.
func (h *HermesAgent) ResourceLabels() map[string]string {
	return map[string]string{
		LabelName:      AppNameValue,
		LabelInstance:  h.Name,
		LabelManagedBy: ManagedByValue,
	}
}

// SelectorLabels returns the labels the StatefulSet selector matches. They
// must never be shadowable by user-provided pod labels.
func (h *HermesAgent) SelectorLabels() map[string]string {
	return map[string]string{
		LabelName:     AppNameValue,
		LabelInstance: h.Name,
	}
}

// PodTemplateLabels returns the labels for the agent pod template.  It copies
// `spec.podLabels` first, then the operator-managed labels over the top.  An
// operator-managed label always wins, so an entry in `spec.podLabels` cannot
// shadow a key that the `StatefulSet` pod selector matches.
func (h *HermesAgent) PodTemplateLabels() map[string]string {
	labels := make(map[string]string, len(h.GetPodLabels())+len(h.ResourceLabels()))
	maps.Copy(labels, h.GetPodLabels())
	maps.Copy(labels, h.ResourceLabels())
	return labels
}

// HermesConfigMapRef identifies the operator-managed bootstrap ConfigMap
// without building it: its data is assembled by the reconciler.
type HermesConfigMapRef struct {
	// Name is the ConfigMap name.
	Name string
	// Namespace is the ConfigMap namespace.
	Namespace string
	// Labels are the labels applied to the ConfigMap.
	Labels map[string]string
}

// GetHermesConfigMapRef returns the identifying reference (name, namespace,
// labels) of the operator-managed bootstrap ConfigMap.
func (h *HermesAgent) GetHermesConfigMapRef() *HermesConfigMapRef {
	return &HermesConfigMapRef{
		Name:      h.GetHermesName(),
		Namespace: h.Namespace,
		Labels:    h.ResourceLabels(),
	}
}

// +kubebuilder:object:root=true

// HermesAgentList contains a list of HermesAgent
type HermesAgentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []HermesAgent `json:"items"`
}

func init() {
	SchemeBuilder.Register(&HermesAgent{}, &HermesAgentList{})
}
