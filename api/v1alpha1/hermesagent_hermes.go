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
	"encoding/json"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

const defaultImageTag = "latest"

// Default image repositories for the hermes-agent and sidecar containers.
const (
	defaultHermesImageRepo  = "nousresearch/hermes-agent"
	defaultSearXNGImageRepo = "searxng/searxng"
	defaultCamofoxImageRepo = "ghcr.io/jo-inc/camofox-browser"
)

// legacyImageShape decodes the deprecated object form of an image override
// (e.g. `image: {repository: searxng/searxng, tag: latest}`).
// Deprecated: the next API version requires image to be a plain string.
type legacyImageShape struct {
	Repository string `json:"repository"`
	Tag        string `json:"tag"`
}

// usesLegacyImageForm reports whether the image override uses the deprecated
// {repository, tag} object form. Nil, empty, string, and other non-object
// values are not the legacy form.
func usesLegacyImageForm(raw *apiextensionsv1.JSON) bool {
	if raw == nil || len(raw.Raw) == 0 {
		return false
	}
	var obj map[string]any
	return json.Unmarshal(raw.Raw, &obj) == nil
}

// UsesLegacyImageForm reports whether any image override in the spec uses the
// deprecated {repository, tag} object form. Admins should migrate such specs
// to a plain string image reference before the next API version, which
// requires the string form.
func (h *HermesAgent) UsesLegacyImageForm() bool {
	if h == nil {
		return false
	}
	if h.Spec.Hermes != nil && usesLegacyImageForm(h.Spec.Hermes.Image) {
		return true
	}
	if h.Spec.SearXNG != nil && usesLegacyImageForm(h.Spec.SearXNG.Image) {
		return true
	}
	if h.Spec.Camofox != nil && usesLegacyImageForm(h.Spec.Camofox.Image) {
		return true
	}
	return false
}

// resolveImage returns a fully qualified image reference from an image
// override field that holds either a plain string (a full reference,
// including tag or digest pinning) or the legacy {repository, tag} object.
// Nil, empty, or unparseable values fall back to the default repository and
// tag so reconciliation stays deterministic.
func resolveImage(raw *apiextensionsv1.JSON, defaultRepo string) string {
	def := defaultRepo + ":" + defaultImageTag
	if raw == nil || len(raw.Raw) == 0 {
		return def
	}

	var ref string
	if err := json.Unmarshal(raw.Raw, &ref); err == nil {
		if strings.TrimSpace(ref) == "" {
			return def
		}
		// String form is a full image reference (tag or digest); used verbatim.
		return strings.TrimSpace(ref)
	}

	var legacy legacyImageShape
	if err := json.Unmarshal(raw.Raw, &legacy); err != nil {
		return def
	}
	repo, tag := defaultRepo, defaultImageTag
	if legacy.Repository != "" {
		repo = legacy.Repository
	}
	if legacy.Tag != "" {
		tag = legacy.Tag
	}
	return repo + ":" + tag
}

// DefaultSnapshotRetention is the number of newest snapshots kept when
// HermesSnapshot.Retention is unset.
const DefaultSnapshotRetention = 3

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
	// existingSnapshot mounts the agent data volume as a PersistentVolumeClaim
	// restored from the named VolumeSnapshot in the agent's namespace, instead
	// of provisioning a new empty PVC. The snapshot must be ReadyToUse; the
	// restored PVC is managed by the operator, sized from the snapshot's
	// restoreSize, and named <snapshot>-restore. When set, enabled and size
	// are ignored; storageClassName selects the restored PVC's storage class
	// (omit to use the cluster default). Changing the snapshot
	// re-provisions a new PVC and rolls the agent onto it. The snapshot
	// itself is never modified or deleted.
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	// +optional
	ExistingSnapshot *string `json:"existingSnapshot,omitempty"`
}

func (p *HermesPersistence) GetExistingClaim() string {
	if p != nil && p.ExistingClaim != nil {
		return *p.ExistingClaim
	}
	return ""
}

func (p *HermesPersistence) GetExistingSnapshot() string {
	if p != nil && p.ExistingSnapshot != nil {
		return *p.ExistingSnapshot
	}
	return ""
}

// RestoredPVCName is the deterministic name of the PersistentVolumeClaim
// provisioned from a restore source snapshot: <snapshot>-restore.
func RestoredPVCName(snapshotName string) string {
	return snapshotName + "-restore"
}

func (p *HermesPersistence) GetSize() resource.Quantity {
	if p != nil && p.Size != nil {
		return *p.Size
	}
	return resource.MustParse("10Gi")
}

// HermesSnapshot configures periodic CSI volume snapshots of the agent data PVC.
//
// Scheduling is CronJob-style: the controller tracks status.snapshot.lastScheduleTime
// and takes one catch-up snapshot if runs were missed. Snapshots are never
// garbage-collected with the agent (no ownerReferences) — deleting the agent
// preserves its backups.
//
// Requires a CSI driver with snapshot support plus the cluster-level
// snapshot-controller (VolumeSnapshot CRD). When the CRD is absent the
// operator surfaces a SnapshotUnsupported condition instead of failing.
// +kubebuilder:validation:XValidation:rule="has(self.schedule) || !has(self.enabled) || !self.enabled",message="schedule is required when snapshot is enabled"
type HermesSnapshot struct {
	// enabled turns on periodic CSI volume snapshots of the agent data PVC.
	// +optional
	Enabled bool `json:"enabled,omitempty"`
	// schedule is the cron expression controlling snapshot times
	// (e.g. "0 3 * * *"). Required when enabled is true.
	// +optional
	Schedule string `json:"schedule,omitempty"`
	// retention is the number of newest snapshots to keep. Older snapshots
	// of this agent are deleted. Defaults to 3.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=3
	// +optional
	Retention *int `json:"retention,omitempty"`
	// volumeSnapshotClassName selects the VolumeSnapshotClass; omit to use
	// the cluster default.
	// +optional
	VolumeSnapshotClassName *string `json:"volumeSnapshotClassName,omitempty"`
}

// IsEnabled reports whether periodic snapshots should be taken.
func (s *HermesSnapshot) IsEnabled() bool {
	return s != nil && s.Enabled
}

// GetSchedule returns the cron schedule expression.
func (s *HermesSnapshot) GetSchedule() string {
	if s == nil {
		return ""
	}
	return s.Schedule
}

// GetRetention returns the number of newest snapshots to keep.
func (s *HermesSnapshot) GetRetention() int {
	if s == nil || s.Retention == nil {
		return DefaultSnapshotRetention
	}
	return *s.Retention
}

// GetVolumeSnapshotClassName returns the VolumeSnapshotClass name to use, if set.
func (s *HermesSnapshot) GetVolumeSnapshotClassName() *string {
	if s == nil {
		return nil
	}
	return s.VolumeSnapshotClassName
}

// HermesStorage defines storage options for the Hermes agent.
type HermesStorage struct {
	// persistence configures a PersistentVolumeClaim for agent data.
	// +optional
	Persistence *HermesPersistence `json:"persistence,omitempty"`
	// snapshot configures periodic CSI volume snapshots of the agent data PVC.
	// +optional
	Snapshot *HermesSnapshot `json:"snapshot,omitempty"`
}

// HermesDotEnv configures generation of a $HERMES_HOME/.env file from a
// Kubernetes Secret and/or ConfigMap. Both singular (secretRef/configMapRef)
// and plural (secretRefs/configMapRefs) forms are supported and may be combined.
//
// Precedence on key collision (last-wins):
//  1. configMapRef (singular)
//  2. configMapRefs (plural, in order)
//  3. secretRef (singular)
//  4. secretRefs (plural, in order)
//
// Secrets override ConfigMaps, and later entries override earlier ones within
// the same type.
// +kubebuilder:validation:XValidation:rule="has(self.secretRef) || has(self.configMapRef) || has(self.secretRefs) || has(self.configMapRefs)",message="at least one of secretRef, configMapRef, secretRefs, or configMapRefs must be set"
type HermesDotEnv struct {
	// secretRef references a Kubernetes Secret whose keys and values are
	// written as KEY=VALUE lines to $HERMES_HOME/.env. Secrets override
	// ConfigMap keys of the same name. See HermesDotEnv for the full
	// precedence order on key collisions.
	//
	// Deprecated: Use secretRefs for new fields; singular forms will be
	// removed in the v1 type.
	// +optional
	SecretRef *corev1.LocalObjectReference `json:"secretRef,omitempty"`
	// configMapRef references a Kubernetes ConfigMap whose keys and values are
	// written as KEY=VALUE lines to $HERMES_HOME/.env. See HermesDotEnv for
	// the full precedence order on key collisions.
	//
	// Deprecated: Use configMapRefs for new fields; singular forms will be
	// removed in the v1 type.
	// +optional
	ConfigMapRef *corev1.LocalObjectReference `json:"configMapRef,omitempty"`
	// secretRefs references Kubernetes Secrets whose keys and values are
	// written as KEY=VALUE lines to $HERMES_HOME/.env. Entries are applied
	// in order; later entries override earlier ones on key collision, and
	// all Secrets override all ConfigMaps. See HermesDotEnv for the full
	// precedence order.
	// +optional
	// +kubebuilder:validation:MaxItems=64
	SecretRefs []corev1.LocalObjectReference `json:"secretRefs,omitempty"`
	// configMapRefs references Kubernetes ConfigMaps whose keys and values
	// are written as KEY=VALUE lines to $HERMES_HOME/.env. Entries are
	// applied in order; later entries override earlier ones on key
	// collision. See HermesDotEnv for the full precedence order.
	// +optional
	// +kubebuilder:validation:MaxItems=64
	ConfigMapRefs []corev1.LocalObjectReference `json:"configMapRefs,omitempty"`
}

// HermesWorkspace defines files to seed in the agent workspace.
type HermesWorkspace struct {
	// files is a map of file path to content.
	// Paths may contain "/" for subdirectories (e.g. "skills/test/SKILL.md").
	// +optional
	Files map[string]string `json:"files,omitempty"`
	// dotEnv generates a $HERMES_HOME/.env file from Kubernetes Secrets
	// and/or ConfigMaps. Each key in the referenced Secret/ConfigMap becomes a
	// KEY=VALUE line in the file. Both singular (secretRef/configMapRef) and
	// plural (secretRefs/configMapRefs) forms are supported and may be
	// combined; see HermesDotEnv for the precedence order on key collisions.
	// +optional
	DotEnv *HermesDotEnv `json:"dotEnv,omitempty"`
}

func (w *HermesWorkspace) GetDotEnv() *HermesDotEnv {
	if w == nil {
		return nil
	}
	return w.DotEnv
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
	// ref is an optional Git commit SHA to install exactly one immutable
	// revision of the plugin (e.g. "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0").
	// +optional
	Ref string `json:"ref,omitempty"`
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
	// monitorScript is the monitor mode: path to a cheap source script under
	// ~/.hermes/scripts/ that runs each tick BEFORE the agent. Unchanged output
	// (exact-bytes hash) suppresses the agent run entirely; changed output injects
	// a MONITOR CHANGE DETECTED diff into the prompt. Script output must be stable
	// (no timestamps). Mutually exclusive with monitorURL; incompatible with noAgent.
	// +optional
	MonitorScript string `json:"monitorScript,omitempty"`
	// monitorURL is the monitor mode: http(s) URL fetched with a bounded GET each
	// tick instead of a script. Same hash-suppression semantics as monitorScript.
	// Mutually exclusive with monitorScript; incompatible with noAgent.
	// +optional
	MonitorURL string `json:"monitorURL,omitempty"`
	// model pins this job to a specific inference model. Omit to follow
	// cron.model / model.default from hermes config.yaml.
	// +optional
	Model string `json:"model,omitempty"`
	// provider is the inference provider paired with model (e.g. 'openrouter', 'nous').
	// +optional
	Provider string `json:"provider,omitempty"`
	// reasoningEffort pins this job's reasoning (thinking) effort. Overrides
	// agent.reasoning_effort and agent.reasoning_overrides for this job;
	// unsupported levels are clamped by the provider at request time. Omit to
	// follow hermes config.
	// +kubebuilder:validation:Enum="none";"minimal";"low";"medium";"high";"xhigh";"max";"ultra"
	// +optional
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
	// continuity injects the job's own previous output into each run's prompt,
	// so it can dedupe against what was already reported and continue where the
	// last run left off (scouts, monitors, incremental digests). First run is
	// unchanged.
	// +optional
	Continuity bool `json:"continuity,omitempty"`
	// profile is the hermes profile name to run the job under.
	// +optional
	Profile string `json:"profile,omitempty"`
}

// HermesBundle defines a bundle (slash command) managed via hermes bundles.
type HermesBundle struct {
	// name is the bundle name and becomes the /slash command; the reconciliation key.
	// +kubebuilder:validation:Required
	Name string `json:"name"`
	// skills are the skill names to include in the bundle (--skill, repeatable).
	// +optional
	Skills []string `json:"skills,omitempty"`
	// description is the human-readable description shown in /help and bundles list.
	// +optional
	Description string `json:"description,omitempty"`
	// instruction is extra guidance prepended to the loaded skill content.
	// +optional
	Instruction string `json:"instruction,omitempty"`
	// force overwrites an existing bundle with the same name.
	// +optional
	Force bool `json:"force,omitempty"`
}

// HermesPackages configures language-specific package managers for pre-installing packages.
type HermesPackages struct {
	// pip configures Python packages to pre-install via `uv pip install`.
	// +optional
	Pip *HermesPipPackages `json:"pip,omitempty"`
	// npm configures npm packages to pre-install via `npm install`.
	// +optional
	Npm *HermesNpmPackages `json:"npm,omitempty"`
}

// HermesPipPackages configures Python packages to pre-install via `uv pip install`.
type HermesPipPackages struct {
	// install is a list of Python package specifiers to install
	// (e.g. "requests", "pandas==2.1.0").
	// +optional
	Install []string `json:"install,omitempty"`
	// extraArgs is a list of additional arguments appended to the `uv pip install` command
	// (e.g. "--index-url=https://...", "--extra-index-url=https://...").
	// +optional
	ExtraArgs []string `json:"extraArgs,omitempty"`
}

// HermesNpmPackages configures npm packages to pre-install via `npm install`.
type HermesNpmPackages struct {
	// install is a list of npm package specifiers to install
	// (e.g. "@anthropic-ai/sdk", "typescript@^5.0.0").
	// +optional
	Install []string `json:"install,omitempty"`
}

// DefaultAPIServerPort is the default port the gateway API server listens on.
const DefaultAPIServerPort = int32(8642)

// DefaultWebhookPort is the default port the webhook listener binds on.
const DefaultWebhookPort = int32(8644)

// HermesAPIServer configures the gateway API server.
type HermesAPIServer struct {
	// enabled turns on the gateway API server (sets API_SERVER_ENABLED=true)
	// bound to all interfaces (sets API_SERVER_HOST=0.0.0.0) so that the
	// Service can route to it.
	// The operator always generates an API key Secret automatically.
	// +optional
	Enabled bool `json:"enabled,omitempty"`
	// port is the port the API server listens on (sets API_SERVER_PORT when enabled).
	// The container port, the Service port, and the NetworkPolicy ingress rule
	// follow this value.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +kubebuilder:default=8642
	// +optional
	Port *int32 `json:"port,omitempty"`
	// corsOrigins lists the browser origins allowed to call the API server
	// (sets API_SERVER_CORS_ORIGINS as a comma-separated list when enabled).
	// CORS stays disabled when empty. Keep this narrow: the API grants access
	// to the agent's full toolset.
	// +optional
	CORSOrigins []string `json:"corsOrigins,omitempty"`
}

func (a *HermesAPIServer) IsEnabled() bool {
	return a != nil && a.Enabled
}

func (a *HermesAPIServer) GetPort() int32 {
	if a == nil || a.Port == nil {
		return DefaultAPIServerPort
	}
	return *a.Port
}

// GetPortName returns the name of the container port the API server listens on.
func (a *HermesAPIServer) GetPortName() string {
	return "api-server"
}

func (a *HermesAPIServer) GetCORSOrigins() []string {
	if a == nil {
		return nil
	}
	return a.CORSOrigins
}

// HermesWebhook configures the webhook ingress.
type HermesWebhook struct {
	// enabled activates the webhook listener (sets WEBHOOK_ENABLED=true).
	// WEBHOOK_SECRET is injected from the operator-managed hermes Secret.
	// +optional
	Enabled bool `json:"enabled,omitempty"`
	// port is the port the webhook listener binds on (sets WEBHOOK_PORT when enabled).
	// The container port, the Service port, and the NetworkPolicy ingress rule
	// follow this value.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +kubebuilder:default=8644
	// +optional
	Port *int32 `json:"port,omitempty"`
}

func (w *HermesWebhook) IsEnabled() bool {
	return w != nil && w.Enabled
}

func (w *HermesWebhook) GetPort() int32 {
	if w == nil || w.Port == nil {
		return DefaultWebhookPort
	}
	return *w.Port
}

func (w *HermesWebhook) GetPortName() string {
	return "webhook"
}

// `HermesConfigMapKeyRef` references a single key of a `ConfigMap` in the
// HermesAgent's namespace.
type HermesConfigMapKeyRef struct {
	// name is the `ConfigMap` name.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// key is the `ConfigMap` key holding the document.  Defaults to "config.yaml".
	// +kubebuilder:default="config.yaml"
	// +optional
	Key string `json:"key,omitempty"`
}

// `GetKey` returns the `ConfigMap` key holding the document.
func (r *HermesConfigMapKeyRef) GetKey() string {
	if r == nil || r.Key == "" {
		return "config.yaml"
	}
	return r.Key
}

// HermesConfig holds the Hermes agent config.yml and related configuration.
type HermesConfig struct {
	// raw holds the verbatim Hermes agent config.yml as free-form YAML/JSON.
	// Mutually exclusive with configMapRef.
	// +optional
	Raw *apiextensionsv1.JSON `json:"raw,omitempty"`
	// `configMapRef` reads the Hermes agent `config.yml` from a key of a
	// `ConfigMap` in the same namespace, instead of holding it inline in raw.  The
	// referenced document is treated exactly as raw is: the operator applies
	// the same defaults to it and copies the result into its own bootstrap
	// `ConfigMap`, so a change to the referenced `ConfigMap` rolls the agent.
	// Reconciliation fails while the `ConfigMap` or the key is missing, and fails
	// when raw is set as well: raw is free-form, so the API server cannot
	// reject the pair at admission time.
	// +optional
	ConfigMapRef *HermesConfigMapKeyRef `json:"configMapRef,omitempty"`
	// apiServer configures the gateway API server. For convenience, the operator
	// automatically generates an API key Secret internally — no manual secret
	// management required. The Secret is persisted across reconciles; enabling
	// the server injects it into the container.
	// +optional
	APIServer *HermesAPIServer `json:"apiServer,omitempty"`
	// webhook configures the webhook ingress.
	// +optional
	Webhook *HermesWebhook `json:"webhook,omitempty"`
}

func (c *HermesConfig) GetRaw() *apiextensionsv1.JSON {
	if c == nil {
		return nil
	}
	return c.Raw
}

// `GetConfigMapRef` returns the `ConfigMap` reference holding `config.yml`, if
// any.
func (c *HermesConfig) GetConfigMapRef() *HermesConfigMapKeyRef {
	if c == nil {
		return nil
	}
	return c.ConfigMapRef
}

// `HasDocument` reports whether a `config.yml` is declared, inline or by
// reference.
func (c *HermesConfig) HasDocument() bool {
	return c.GetRaw() != nil || c.GetConfigMapRef() != nil
}

func (c *HermesConfig) GetAPIServer() *HermesAPIServer {
	if c == nil {
		return nil
	}
	return c.APIServer
}

func (c *HermesConfig) GetWebhook() *HermesWebhook {
	if c == nil {
		return nil
	}
	return c.Webhook
}

// HermesInitScript defines a simple init container that runs a shell script.
type HermesInitScript struct {
	// name is the name of the init container. Must be unique within the pod.
	// +kubebuilder:validation:Required
	Name string `json:"name"`
	// script is the shell script body to execute via /bin/sh -ec.
	// The hermes-agent environment (HERMES_HOME, HOME, user env) is available.
	// +kubebuilder:validation:Required
	Script string `json:"script"`
}

// `HermesProfileConfig` holds the `config.yaml` for a named profile.
// apiServer and webhook are excluded — profiles share a single multiplexed gateway.
type HermesProfileConfig struct {
	// raw is the profile config as a JSON-serialized object.
	// Mutually exclusive with configMapRef.
	// +optional
	Raw *apiextensionsv1.JSON `json:"raw,omitempty"`
	// `configMapRef` reads the profile config from a key of a `ConfigMap` in the
	// same namespace, instead of holding it inline in raw.  See
	// HermesConfig.configMapRef, including why setting both fails at
	// reconcile time rather than at admission time.
	// +optional
	ConfigMapRef *HermesConfigMapKeyRef `json:"configMapRef,omitempty"`
}

func (c *HermesProfileConfig) GetRaw() *apiextensionsv1.JSON {
	if c == nil {
		return nil
	}
	return c.Raw
}

// `GetConfigMapRef` returns the `ConfigMap` reference holding the profile
// config, if any.
func (c *HermesProfileConfig) GetConfigMapRef() *HermesConfigMapKeyRef {
	if c == nil {
		return nil
	}
	return c.ConfigMapRef
}

// `HasDocument` reports whether a config is declared, inline or by reference.
func (c *HermesProfileConfig) HasDocument() bool {
	return c.GetRaw() != nil || c.GetConfigMapRef() != nil
}

// HermesProfile defines a named Hermes profile to create and configure.
type HermesProfile struct {
	// clone copies config.yaml, .env, SOUL.md, and skills from the default profile
	// at creation time (--clone flag). Source is always the default profile.
	// +optional
	Clone bool `json:"clone,omitempty"`
	// config holds the raw config.yaml for this profile.
	// +optional
	Config *HermesProfileConfig `json:"config,omitempty"`
	// workspace defines files and dotEnv for this profile.
	// +optional
	Workspace *HermesWorkspace `json:"workspace,omitempty"`
	// plugins to install in this profile.
	// +optional
	Plugins []HermesPlugin `json:"plugins,omitempty"`
	// skills to install in this profile.
	// +optional
	Skills []HermesSkill `json:"skills,omitempty"`
	// crons for this profile.
	// +optional
	Crons []HermesCron `json:"crons,omitempty"`
	// bundles for this profile.
	// +optional
	Bundles []HermesBundle `json:"bundles,omitempty"`
}

// Hermes defines the hermes-specific section of the spec.
type Hermes struct {
	// image overrides the container image used for the hermes-agent container
	// and all init containers. Accepts either a full image reference string
	// (e.g. "nousresearch/hermes-agent:v1.2.3" or a digest reference
	// "nousresearch/hermes-agent@sha256:...") or, for backward compatibility,
	// an object with repository and tag fields.
	// The object form is deprecated; the next API version requires image to be
	// a plain string.
	// +optional
	Image *apiextensionsv1.JSON `json:"image,omitempty"`
	// config holds the Hermes agent config.yml configuration.
	// +optional
	Config *HermesConfig `json:"config,omitempty"`
	// storage configures persistent storage for the agent.
	// +optional
	Storage *HermesStorage `json:"storage,omitempty"`
	// workspace defines files to seed in the agent's home directory.
	// +optional
	Workspace *HermesWorkspace `json:"workspace,omitempty"`
	// packages configures language-specific package managers for pre-installing packages before the agent starts.
	// +optional
	Packages *HermesPackages `json:"packages,omitempty"`
	// plugins is a list of plugins to install in the Hermes agent.
	// +optional
	Plugins []HermesPlugin `json:"plugins,omitempty"`
	// skills is a list of skills to install via hermes skills install.
	// +optional
	Skills []HermesSkill `json:"skills,omitempty"`
	// crons is a list of scheduled jobs to manage via hermes cron.
	// +optional
	Crons []HermesCron `json:"crons,omitempty"`
	// bundles is a list of bundles to manage via hermes bundles.
	// +optional
	Bundles []HermesBundle `json:"bundles,omitempty"`
	// env is a list of environment variables to inject into the hermes-agent container.
	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`
	// envFrom injects all keys from a ConfigMap or Secret as environment variables.
	// +optional
	EnvFrom []corev1.EnvFromSource `json:"envFrom,omitempty"`
	// resources overrides the resource requests and limits for the hermes-agent container.
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
	// probes overrides the health probe configuration for the hermes-agent container.
	// Probes exec `hermes gateway status` inside the container, which works
	// regardless of which ports the gateway listens on.
	// +optional
	Probes *Probes `json:"probes,omitempty"`
	// ports declares additional container ports on the hermes-agent container.
	// The API server port (config.apiServer.port, default 8642) is always
	// included and should not be repeated here.
	// +optional
	Ports []corev1.ContainerPort `json:"ports,omitempty"`
	// initChownData runs an init container that chowns /opt/data to the hermes
	// user (10000:10000) before the agent starts. Enable this when the data
	// volume is provisioned with root ownership (e.g. most cloud block-storage
	// provisioners) and the agent would otherwise fail to write to it.
	// +optional
	InitChownData bool `json:"initChownData,omitempty"`
	// initScripts is a list of simple init containers that run shell scripts before
	// the agent starts. Unlike spec.initContainers, these only require a name and
	// script body — the image, volumes, env, and security context are inherited
	// from the hermes-agent configuration automatically.
	// +optional
	// +kubebuilder:validation:MaxItems=10
	InitScripts []HermesInitScript `json:"initScripts,omitempty"`
	// profiles is a map of named Hermes profiles to create and configure.
	// Each profile is set up via its own init-profile-<name> init container,
	// which runs after the consolidated init-hermes container that configures
	// the default profile.
	// +optional
	Profiles map[string]HermesProfile `json:"profiles,omitempty"`
}

// Probes defines health probe configuration for the hermes-agent container.
type Probes struct {
	// liveness configures the liveness probe. Disabled unless enabled.
	// +optional
	Liveness *Probe `json:"liveness,omitempty"`
	// readiness configures the readiness probe. Disabled unless enabled.
	// +optional
	Readiness *Probe `json:"readiness,omitempty"`
	// startup configures the startup probe. Disabled unless enabled.
	// +optional
	Startup *Probe `json:"startup,omitempty"`
}

// Probe defines a single health probe's tunable parameters. The probe action
// (exec `hermes gateway status`) is fixed by the operator.
type Probe struct {
	// enabled enables the probe.
	// +kubebuilder:default=false
	// +optional
	Enabled *bool `json:"enabled,omitempty"`
	// initialDelaySeconds is the seconds after container start before probing.
	// +optional
	InitialDelaySeconds *int32 `json:"initialDelaySeconds,omitempty"`
	// periodSeconds is how often (in seconds) to perform the probe.
	// +optional
	PeriodSeconds *int32 `json:"periodSeconds,omitempty"`
	// timeoutSeconds is the seconds after which the probe times out.
	// +optional
	TimeoutSeconds *int32 `json:"timeoutSeconds,omitempty"`
	// failureThreshold is the number of retries before giving up.
	// +optional
	FailureThreshold *int32 `json:"failureThreshold,omitempty"`
}

func (h *Hermes) GetConfig() *apiextensionsv1.JSON {
	if h == nil {
		return nil
	}
	return h.Config.GetRaw()
}

// `GetConfigMapRef` returns the `ConfigMap` reference holding the default
// profile's `config.yml`, if any.
func (h *Hermes) GetConfigMapRef() *HermesConfigMapKeyRef {
	if h == nil {
		return nil
	}
	return h.Config.GetConfigMapRef()
}

// `HasConfigDocument` reports whether the default profile declares a
// `config.yml`, inline or by reference.
func (h *Hermes) HasConfigDocument() bool {
	if h == nil {
		return false
	}
	return h.Config.HasDocument()
}

func (h *Hermes) GetAPIServer() *HermesAPIServer {
	if h == nil {
		return nil
	}
	return h.Config.GetAPIServer()
}

func (h *Hermes) GetWebhook() *HermesWebhook {
	if h == nil {
		return nil
	}
	return h.Config.GetWebhook()
}

func (h *Hermes) GetPersistence() *HermesPersistence {
	if h == nil || h.Storage == nil {
		return nil
	}
	return h.Storage.Persistence
}

// GetSnapshot returns the snapshot configuration, if any.
func (h *Hermes) GetSnapshot() *HermesSnapshot {
	if h == nil || h.Storage == nil {
		return nil
	}
	return h.Storage.Snapshot
}

func (h *Hermes) GetWorkspace() *HermesWorkspace {
	if h == nil {
		return nil
	}
	return h.Workspace
}

func (h *Hermes) GetPackages() *HermesPackages {
	if h == nil {
		return nil
	}
	return h.Packages
}

func (p *HermesPackages) GetPip() *HermesPipPackages {
	if p == nil {
		return nil
	}
	return p.Pip
}

func (p *HermesPackages) GetNpm() *HermesNpmPackages {
	if p == nil {
		return nil
	}
	return p.Npm
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

func (h *Hermes) GetBundles() []HermesBundle {
	if h == nil {
		return nil
	}
	return h.Bundles
}

func (h *Hermes) GetProfiles() map[string]HermesProfile {
	if h == nil {
		return nil
	}
	return h.Profiles
}

// GetSortedProfileNames returns the profile names in a deterministic order, so
// reconcilers iterate the spec's profiles without map-order nondeterminism.
func (h *Hermes) GetSortedProfileNames() []string {
	if h == nil {
		return nil
	}
	names := make([]string, 0, len(h.Profiles))
	for name := range h.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
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

func (h *Hermes) GetPorts() []corev1.ContainerPort {
	if h == nil {
		return nil
	}
	return h.Ports
}

func (h *Hermes) GetProbes() *Probes {
	if h == nil {
		return nil
	}
	return h.Probes
}

func (p *Probes) GetLiveness() *Probe {
	if p == nil {
		return nil
	}
	return p.Liveness
}

func (p *Probes) GetReadiness() *Probe {
	if p == nil {
		return nil
	}
	return p.Readiness
}

func (p *Probes) GetStartup() *Probe {
	if p == nil {
		return nil
	}
	return p.Startup
}

// IsEnabled reports whether the probe is enabled (default false when unset).
func (p *Probe) IsEnabled() bool {
	return p != nil && p.Enabled != nil && *p.Enabled
}

// GetProbe returns a configured corev1.Probe from the spec, applying overrides on top of defaults.
// Returns nil if the probe is disabled or the spec is nil.
func (p *Probe) GetProbe(command []string, defaults corev1.Probe) *corev1.Probe {
	if p == nil || !p.IsEnabled() {
		return nil
	}
	probe := defaults
	probe.ProbeHandler = corev1.ProbeHandler{
		Exec: &corev1.ExecAction{Command: command},
	}
	if p.InitialDelaySeconds != nil {
		probe.InitialDelaySeconds = *p.InitialDelaySeconds
	}
	if p.PeriodSeconds != nil {
		probe.PeriodSeconds = *p.PeriodSeconds
	}
	if p.TimeoutSeconds != nil {
		probe.TimeoutSeconds = *p.TimeoutSeconds
	}
	if p.FailureThreshold != nil {
		probe.FailureThreshold = *p.FailureThreshold
	}
	return &probe
}

func (h *Hermes) ShouldInitChownData() bool {
	return h != nil && h.InitChownData
}

func (h *Hermes) GetInitScripts() []HermesInitScript {
	if h == nil {
		return nil
	}
	return h.InitScripts
}

func (h *Hermes) GetImage() string {
	if h == nil {
		return defaultHermesImageRepo + ":" + defaultImageTag
	}
	return resolveImage(h.Image, defaultHermesImageRepo)
}
