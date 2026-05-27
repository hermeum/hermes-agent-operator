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

// HermesPlugin defines a plugin to install in the Hermes agent.
type HermesPlugin struct {
	// identifier is the Git URL or owner/repo shorthand
	// (e.g. "anpicasso/hermes-plugin-chrome-profiles").
	// +kubebuilder:validation:Required
	Identifier string `json:"identifier"`
	// enable controls whether the plugin is auto-enabled after install.
	// Defaults to true (--enable). Set to false to install disabled (--no-enable).
	// +optional
	Enable *bool `json:"enable,omitempty"`
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

// HermesCron defines a scheduled job managed via hermes cron.
type HermesCron struct {
	// name is the human-friendly job name and the reconciliation key.
	// +kubebuilder:validation:Required
	Name string `json:"name"`
	// schedule is the cron schedule (e.g. "30m", "every 2h", "0 9 * * *").
	// +kubebuilder:validation:Required
	Schedule string `json:"schedule"`
	// prompt is an optional self-contained prompt or task instruction.
	// +optional
	Prompt string `json:"prompt,omitempty"`
	// deliver is the delivery target: origin, local, telegram, discord, signal, or platform:chat_id.
	// +optional
	Deliver string `json:"deliver,omitempty"`
	// repeat is the optional repeat count.
	// +optional
	Repeat *int `json:"repeat,omitempty"`
	// skills attaches skills to the job (--skill, repeatable).
	// +optional
	Skills []string `json:"skills,omitempty"`
	// script is a path to a script under ~/.hermes/scripts/.
	// +optional
	Script string `json:"script,omitempty"`
	// noAgent skips the LLM entirely — runs --script on schedule and delivers stdout directly.
	// +optional
	NoAgent bool `json:"noAgent,omitempty"`
	// workdir is the absolute path for the job to run from.
	// +optional
	Workdir string `json:"workdir,omitempty"`
	// profile is the hermes profile name to run the job under.
	// +optional
	Profile string `json:"profile,omitempty"`
}

// HermesImage specifies the container image repository and tag.
type HermesImage struct {
	// repository is the image repository (e.g. "nousresearch/hermes-agent").
	// Defaults to "nousresearch/hermes-agent".
	// +optional
	Repository string `json:"repository,omitempty"`
	// tag is the image tag. Defaults to "latest".
	// +optional
	Tag string `json:"tag,omitempty"`
}

// Hermes defines the hermes-specific section of the spec.
type Hermes struct {
	// image overrides the container image used for the hermes-agent container
	// and all init containers.
	// +optional
	Image *HermesImage `json:"image,omitempty"`
	// config holds the Hermes agent config.yml configuration.
	// +optional
	Config *apiextensionsv1.JSON `json:"config,omitempty"`
	// storage configures persistent storage for the agent.
	// +optional
	Storage *HermesStorage `json:"storage,omitempty"`
	// workspace defines files to seed in the agent's home directory.
	// +optional
	Workspace *HermesWorkspace `json:"workspace,omitempty"`
	// plugins is a list of plugins to install in the Hermes agent.
	// +optional
	Plugins []HermesPlugin `json:"plugins,omitempty"`
	// skills is a list of skills to install via hermes skills install.
	// +optional
	Skills []HermesSkill `json:"skills,omitempty"`
	// crons is a list of scheduled jobs to manage via hermes cron.
	// +optional
	Crons []HermesCron `json:"crons,omitempty"`
	// env is a list of environment variables to inject into the hermes-agent container.
	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`
	// envFrom injects all keys from a ConfigMap or Secret as environment variables.
	// +optional
	EnvFrom []corev1.EnvFromSource `json:"envFrom,omitempty"`
	// resources overrides the resource requests and limits for the hermes-agent container.
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
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

func (h *Hermes) GetPlugins() []HermesPlugin {
	if h == nil {
		return nil
	}
	return h.Plugins
}

func (h *Hermes) GetSkills() []HermesSkill {
	if h == nil {
		return nil
	}
	return h.Skills
}

func (h *Hermes) GetCrons() []HermesCron {
	if h == nil {
		return nil
	}
	return h.Crons
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

func (h *Hermes) GetResources() corev1.ResourceRequirements {
	if h != nil && h.Resources != nil {
		return *h.Resources
	}
	return corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("2"),
			corev1.ResourceMemory: resource.MustParse("4Gi"),
		},
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("1Gi"),
		},
	}
}

func (h *Hermes) GetImage() string {
	repo := "nousresearch/hermes-agent"
	tag := "latest"
	if h != nil && h.Image != nil {
		if h.Image.Repository != "" {
			repo = h.Image.Repository
		}
		if h.Image.Tag != "" {
			tag = h.Image.Tag
		}
	}
	return repo + ":" + tag
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
