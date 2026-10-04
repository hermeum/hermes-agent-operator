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
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// defaultSearXNGSettings is the settings.yml mounted at /etc/searxng when the
// user does not provide their own. It enables the JSON response format that
// Hermes requires to consume SearXNG results.
// See https://docs.searxng.org/admin/installation-searxng.html#use-default-settings-yml
const defaultSearXNGSettings = `# settings.yml
use_default_settings: true

search:
  formats:
    - html
    - json
`

// SearXNG configures an optional SearXNG sidecar that backs the Hermes
// web_search tool. When enabled, the operator runs SearXNG alongside the
// hermes-agent container, sets web.search_backend to "searxng" in the generated
// Hermes config.yaml (unless already set), and injects the SEARXNG_URL
// environment variable into the hermes-agent container.
type SearXNG struct {
	// Enabled enables the SearXNG sidecar that is used by the web_search tool.
	// +kubebuilder:default=false
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// Image configures the SearXNG container image. Accepts either a full
	// image reference string (e.g. "searxng/searxng:latest" or a digest
	// reference) or, for backward compatibility, an object with repository
	// and tag fields.
	// The object form is deprecated; the next API version requires image to be
	// a plain string.
	// +optional
	Image *apiextensionsv1.JSON `json:"image,omitempty"`

	// Resources specifies compute resources for the SearXNG container.
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// ConfigFiles mounts configuration files at /etc/searxng. Each entry's key
	// is the file name and its value is the file contents. When "settings.yml"
	// is not provided, a default that enables the JSON response format (required
	// by Hermes) is used.
	// +optional
	ConfigFiles map[string]string `json:"configFiles,omitempty"`

	// Persistence configures persistent storage located at /var/cache/searxng.
	// When enabled, container state (faviconcache.db, etc.) survives pod
	// restarts. When disabled (default), an emptyDir is used and all state is
	// lost on restart.
	// +optional
	Persistence *SearXNGPersistence `json:"persistence,omitempty"`

	// Env specifies additional environment variables for the SearXNG
	// sidecar container, merged with the operator-managed variables.
	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`
}

// GetResources returns the SearXNG container resource requirements.
func (s *SearXNG) GetResources() corev1.ResourceRequirements {
	if s != nil && s.Resources != nil {
		return *s.Resources
	}
	return corev1.ResourceRequirements{}
}

// GetEnv returns the additional environment variables for the SearXNG container.
func (s *SearXNG) GetEnv() []corev1.EnvVar {
	if s == nil {
		return nil
	}
	return s.Env
}

// GetPersistence returns the SearXNG persistence configuration, if any.
func (s *SearXNG) GetPersistence() *SearXNGPersistence {
	if s == nil {
		return nil
	}
	return s.Persistence
}

// GetConfigFiles returns the configured files to mount at /etc/searxng, with a
// default settings.yml injected when the user has not supplied one.
func (s *SearXNG) GetConfigFiles() map[string]string {
	files := map[string]string{}
	if s != nil {
		maps.Copy(files, s.ConfigFiles)
	}
	if _, ok := files["settings.yml"]; !ok {
		files["settings.yml"] = defaultSearXNGSettings
	}
	return files
}

// IsEnabled reports whether the SearXNG sidecar should be created.
func (s *SearXNG) IsEnabled() bool {
	return s != nil && s.Enabled
}

// GetImage returns the fully qualified SearXNG image reference.
func (s *SearXNG) GetImage() string {
	return resolveImage(s.Image, defaultSearXNGImageRepo)
}

// SearXNGPersistence configures a PersistentVolumeClaim for the SearXNG cache.
type SearXNGPersistence struct {
	// enabled turns on a PersistentVolumeClaim for /var/cache/searxng.
	// +optional
	Enabled bool `json:"enabled,omitempty"`
	// size is the storage request for the PVC (e.g. "1Gi"). Defaults to 1Gi.
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

func (p *SearXNGPersistence) IsEnabled() bool {
	return p != nil && p.Enabled
}

// GetExistingClaim returns the name of a pre-existing PVC to mount, if set.
func (p *SearXNGPersistence) GetExistingClaim() string {
	if p != nil && p.ExistingClaim != nil {
		return *p.ExistingClaim
	}
	return ""
}

// GetSize returns the storage request for the SearXNG cache PVC.
func (p *SearXNGPersistence) GetSize() resource.Quantity {
	if p != nil && p.Size != nil {
		return *p.Size
	}
	return resource.MustParse("1Gi")
}

// Camofox configures an optional Camofox sidecar that backs the Hermes browser
// automation tool. When enabled, the operator runs Camofox alongside the
// hermes-agent container and injects the CAMOFOX_URL environment variable into
// the hermes-agent container.
type Camofox struct {
	// Enabled enables the Camofox sidecar for browser automation
	// +kubebuilder:default=false
	// +optional
	Enabled bool `json:"enabled,omitempty"`
	// Image configures the Camofox container image. Accepts either a full
	// image reference string (e.g. "ghcr.io/jo-inc/camofox-browser:latest"
	// or a digest reference) or, for backward compatibility, an object with
	// repository and tag fields.
	// The object form is deprecated; the next API version requires image to be
	// a plain string.
	// +optional
	Image *apiextensionsv1.JSON `json:"image,omitempty"`
	// Resources specifies compute resources for the Camofox container
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
	// Persistence configures persistent storage located at /root/.camofox.
	// When enabled, container state (cookies, etc.) survives
	// pod restarts.
	// When disabled (default), an emptyDir is used and all browser
	// state is lost on restart.
	// +optional
	Persistence CamofoxPersistenceSpec `json:"persistence,omitempty"`
	// Env specifies additional environment variables for the Camofox
	// sidecar container, merged with the operator-managed variables.
	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`
}

// IsEnabled reports whether the Camofox sidecar should be created.
func (c *Camofox) IsEnabled() bool {
	return c != nil && c.Enabled
}

// GetImage returns the fully qualified Camofox image reference.
func (c *Camofox) GetImage() string {
	return resolveImage(c.Image, defaultCamofoxImageRepo)
}

// GetResources returns the Camofox container resource requirements.
func (c *Camofox) GetResources() corev1.ResourceRequirements {
	if c != nil && c.Resources != nil {
		return *c.Resources
	}
	return corev1.ResourceRequirements{}
}

// GetEnv returns the additional environment variables for the Camofox container.
func (c *Camofox) GetEnv() []corev1.EnvVar {
	if c == nil {
		return nil
	}
	return c.Env
}

// GetPersistence returns a pointer to the Camofox persistence configuration.
func (c *Camofox) GetPersistence() *CamofoxPersistenceSpec {
	if c == nil {
		return nil
	}
	return &c.Persistence
}

// CamofoxPersistenceSpec configures a PersistentVolumeClaim for the Camofox data directory.
type CamofoxPersistenceSpec struct {
	// enabled turns on a PersistentVolumeClaim for /root/.camofox.
	// +optional
	Enabled bool `json:"enabled,omitempty"`
	// size is the storage request for the PVC (e.g. "1Gi"). Defaults to 1Gi.
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

// IsEnabled reports whether the Camofox PVC should be provisioned.
func (p *CamofoxPersistenceSpec) IsEnabled() bool {
	return p != nil && p.Enabled
}

// GetExistingClaim returns the name of a pre-existing PVC to mount, if set.
func (p *CamofoxPersistenceSpec) GetExistingClaim() string {
	if p != nil && p.ExistingClaim != nil {
		return *p.ExistingClaim
	}
	return ""
}

// GetSize returns the storage request for the Camofox data PVC.
func (p *CamofoxPersistenceSpec) GetSize() resource.Quantity {
	if p != nil && p.Size != nil {
		return *p.Size
	}
	return resource.MustParse("1Gi")
}
