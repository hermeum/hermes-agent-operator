package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	agentsv1alpha1 "hermeum/hermes-agent-operator/api/v1alpha1"
	"maps"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	sigsyaml "sigs.k8s.io/yaml"
)

func (u *HermesAgentUseCase) reconcileHermesConfigMap(ctx context.Context, ha *agentsv1alpha1.HermesAgent) (ctrl.Result, error) {
	cmName := ha.GetHermesName()
	cm, err := u.kube.GetConfigMap(ctx, GetConfigMapParam{
		NamespacedName: types.NamespacedName{Name: cmName, Namespace: ha.Namespace},
	})
	if err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}

	desired, err := buildHermesConfigMap(ha)
	if err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}

	if cm != nil {
		if configMapDataEqual(desired, cm) {
			return ctrl.Result{}, nil
		}
		desired.ResourceVersion = cm.ResourceVersion
		err := u.kube.UpdateConfigMapOwnedByHermesAgent(ctx, UpdateConfigMapParam{HermesAgent: ha, ConfigMap: desired})
		if err != nil {
			return ctrl.Result{RequeueAfter: 30 * time.Second}, err
		}
		u.tel.Debug(ctx, "Hermes ConfigMap updated")
		return ctrl.Result{}, nil
	}

	err = u.kube.CreateConfigMapOwnedByHermesAgent(ctx, CreateConfigMapOfHermesAgentParam{HermesAgent: ha, ConfigMap: desired})
	if err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}
	u.tel.Debug(ctx, "Hermes ConfigMap created")
	ha.Status.ManagedResources.HermesConfigMap = ha.GetHermesName()
	if err := u.kube.UpdateHermesAgentStatus(ctx, UpdateHermesAgentStatusParam{HermesAgent: ha}); err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}
	return ctrl.Result{}, nil
}

func configMapDataEqual(a, b *corev1.ConfigMap) bool {
	return maps.Equal(a.Data, b.Data)
}

// applySearXNGConfigDefaults applies default values to the SearXNG config if they are not set by the user.
func applySearXNGConfigDefaults(raw []byte) ([]byte, error) {
	cfg := map[string]any{}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshaling config: %w", err)
	}

	web, _ := cfg["web"].(map[string]any)
	if web == nil {
		web = map[string]any{}
		cfg["web"] = web
	}
	if _, ok := web["search_backend"]; !ok {
		web["search_backend"] = "searxng"
	}

	out, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshaling config: %w", err)
	}
	return out, nil
}

// applyMultiplexProfilesDefault injects gateway.multiplex_profiles: true into the Hermes
// config when named profiles are declared, unless already set.
func applyMultiplexProfilesDefault(raw []byte) ([]byte, error) {
	cfg := map[string]any{}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshaling config: %w", err)
	}

	gateway, _ := cfg["gateway"].(map[string]any)
	if gateway == nil {
		gateway = map[string]any{}
		cfg["gateway"] = gateway
	}
	if _, ok := gateway["multiplex_profiles"]; !ok {
		gateway["multiplex_profiles"] = true
	}

	out, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshaling config: %w", err)
	}
	return out, nil
}

// applyCamofoxConfigDefaults injects browser.camofox.managed_persistence: true into
// the Hermes config when Camofox managed persistence is active, unless already set.
func applyCamofoxConfigDefaults(raw []byte) ([]byte, error) {
	cfg := map[string]any{}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshaling config: %w", err)
	}

	browser, _ := cfg["browser"].(map[string]any)
	if browser == nil {
		browser = map[string]any{}
		cfg["browser"] = browser
	}
	camofox, _ := browser["camofox"].(map[string]any)
	if camofox == nil {
		camofox = map[string]any{}
		browser["camofox"] = camofox
	}
	if _, ok := camofox["managed_persistence"]; !ok {
		camofox["managed_persistence"] = true
	}

	out, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshaling config: %w", err)
	}
	return out, nil
}

func buildHermesConfigMap(ha *agentsv1alpha1.HermesAgent) (*corev1.ConfigMap, error) {
	data := map[string]string{}

	// Collect default profile raw config; SearXNG and Camofox defaults only apply when
	// the user has provided a config, but multiplex_profiles must also be set when
	// profiles exist even if no explicit config was given.
	var defaultRaw []byte
	if hc := ha.GetHermes().GetConfig(); hc != nil {
		defaultRaw = hc.Raw
		if ha.GetSearXNG().IsEnabled() {
			var err error
			defaultRaw, err = applySearXNGConfigDefaults(defaultRaw)
			if err != nil {
				return nil, err
			}
		}
		cx := ha.GetCamofox()
		if cx.IsEnabled() && cx.GetPersistence().IsEnabled() && cx.GetPersistence().GetExistingClaim() == "" {
			var err error
			defaultRaw, err = applyCamofoxConfigDefaults(defaultRaw)
			if err != nil {
				return nil, err
			}
		}
	}

	if len(ha.GetHermes().GetProfiles()) > 0 {
		if defaultRaw == nil {
			defaultRaw = []byte("{}")
		}
		var err error
		defaultRaw, err = applyMultiplexProfilesDefault(defaultRaw)
		if err != nil {
			return nil, err
		}
	}

	if defaultRaw != nil {
		yamlBytes, err := sigsyaml.JSONToYAML(defaultRaw)
		if err != nil {
			return nil, err
		}
		data["profile.default.config.yaml"] = string(yamlBytes)
	}

	if hw := ha.GetHermes().GetWorkspace(); hw != nil {
		for path, content := range hw.Files {
			key := "profile.default.workspace." + strings.ReplaceAll(path, "/", hermesWorkspacePathSeparator)
			data[key] = content
		}
	}

	for name, profile := range ha.GetHermes().GetProfiles() {
		if raw := profile.Config.GetRaw(); raw != nil {
			yamlBytes, err := sigsyaml.JSONToYAML(raw.Raw)
			if err != nil {
				return nil, err
			}
			data["profile."+name+".config.yaml"] = string(yamlBytes)
		}
		if profile.Workspace != nil {
			for path, content := range profile.Workspace.Files {
				key := "profile." + name + ".workspace." + strings.ReplaceAll(path, "/", hermesWorkspacePathSeparator)
				data[key] = content
			}
		}
	}

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ha.GetHermesName(),
			Namespace: ha.Namespace,
			Labels:    resourceLabels(ha),
		},
		Data: data,
	}, nil
}
