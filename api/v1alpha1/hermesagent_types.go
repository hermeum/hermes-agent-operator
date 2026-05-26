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
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// HermesPersistence configures persistent volume claims for the Hermes agent.
type HermesPersistence struct {
	// enabled turns on a PersistentVolumeClaim for /opt/data.
	// +optional
	Enabled bool `json:"enabled,omitempty"`
	// size is the storage request for the PVC (e.g. "10Gi"). Defaults to 10Gi.
	// +optional
	Size *resource.Quantity `json:"size,omitempty"`
	// storageClassName selects the StorageClass; omit to use the cluster default.
	// +optional
	StorageClassName *string `json:"storageClassName,omitempty"`
	// existingClaim mounts a pre-existing PVC by name instead of provisioning a new one.
	// When set, enabled/size/storageClassName are ignored.
	// +optional
	ExistingClaim *string `json:"existingClaim,omitempty"`
}

func (p *HermesPersistence) GetExistingClaim() string {
	if p != nil && p.ExistingClaim != nil {
		return *p.ExistingClaim
	}
	return ""
}

func (p *HermesPersistence) GetSize() resource.Quantity {
	if p != nil && p.Size != nil {
		return *p.Size
	}
	return resource.MustParse("10Gi")
}

// HermesStorage defines storage options for the Hermes agent.
type HermesStorage struct {
	// persistence configures a PersistentVolumeClaim for agent data.
	// +optional
	Persistence *HermesPersistence `json:"persistence,omitempty"`
}

// HermesWorkspace defines files to seed in the agent workspace.
type HermesWorkspace struct {
	// files is a map of file path to content.
	// Paths may contain "/" for subdirectories (e.g. "skills/test/SKILL.md").
	// +optional
	Files map[string]string `json:"files,omitempty"`
}

// HermesSkill defines a skill to install via hermes skills install.
type HermesSkill struct {
	// identifier is the skill identifier (e.g. openai/skills/skill-creator) or HTTP(S) URL to a SKILL.md file.
	// +required
	Identifier string `json:"identifier"`
	// category is the category folder to install into.
	// +optional
	Category string `json:"category,omitempty"`
	// name overrides the skill name (useful when the SKILL.md has no name frontmatter).
	// +optional
	Name string `json:"name,omitempty"`
	// force installs despite a blocked scan verdict.
	// +optional
	Force bool `json:"force,omitempty"`
}

// Hermes defines the hermes-specific section of the spec.
type Hermes struct {
	// config holds the Hermes agent config.yml configuration.
	// +optional
	Config *apiextensionsv1.JSON `json:"config,omitempty"`
	// storage configures persistent storage for the agent.
	// +optional
	Storage *HermesStorage `json:"storage,omitempty"`
	// workspace defines files to seed in the agent's home directory.
	// +optional
	Workspace *HermesWorkspace `json:"workspace,omitempty"`
	// skills is a list of skills to install via hermes skills install.
	// +optional
	Skills []HermesSkill `json:"skills,omitempty"`
	// env is a list of environment variables to inject into the hermes-agent container.
	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`
	// envFrom injects all keys from a ConfigMap or Secret as environment variables.
	// +optional
	EnvFrom []corev1.EnvFromSource `json:"envFrom,omitempty"`
}

// HermesAgentSpec defines the desired state of HermesAgent
type HermesAgentSpec struct {
	// hermes defines the Hermes agent configuration.
	// +optional
	Hermes *Hermes `json:"hermes,omitempty"`
}

// HermesAgentStatus defines the observed state of HermesAgent.
type HermesAgentStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// For Kubernetes API conventions, see:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

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
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

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

func (h *HermesAgent) GetConfigMapName() string {
	return h.Name + "-config"
}

func (h *HermesAgent) GetHermes() *Hermes {
	return h.Spec.Hermes
}

func (h *Hermes) GetConfig() *apiextensionsv1.JSON {
	if h == nil {
		return nil
	}
	return h.Config
}

func (h *Hermes) GetPersistence() *HermesPersistence {
	if h == nil || h.Storage == nil {
		return nil
	}
	return h.Storage.Persistence
}

func (h *Hermes) GetWorkspace() *HermesWorkspace {
	if h == nil {
		return nil
	}
	return h.Workspace
}

func (h *Hermes) GetSkills() []HermesSkill {
	if h == nil {
		return nil
	}
	return h.Skills
}

func (h *Hermes) GetEnv() []corev1.EnvVar {
	if h == nil {
		return nil
	}
	return h.Env
}

func (h *Hermes) GetEnvFrom() []corev1.EnvFromSource {
	if h == nil {
		return nil
	}
	return h.EnvFrom
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
