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
	nsName := types.NamespacedName{Namespace: ha.Namespace, Name: ha.Name}

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
			ha.Status.ManagedResources.HermesConfigMap = ha.GetHermesName()
			return ctrl.Result{}, nil
		}
		desired.ResourceVersion = cm.ResourceVersion
		err := u.kube.UpdateConfigMapOwnedByHermesAgent(ctx, UpdateConfigMapParam{HermesAgent: ha, ConfigMap: desired})
		if err != nil {
			return ctrl.Result{RequeueAfter: 30 * time.Second}, err
		}
		u.tel.Debug(ctx, "Hermes ConfigMap updated", "namespacedName", nsName)
		ha.Status.ManagedResources.HermesConfigMap = ha.GetHermesName()
		return ctrl.Result{}, nil
	}

	err = u.kube.CreateConfigMapOwnedByHermesAgent(ctx, CreateConfigMapOfHermesAgentParam{HermesAgent: ha, ConfigMap: desired})
	if err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}
	u.tel.Debug(ctx, "Hermes ConfigMap created", "namespacedName", nsName)
	ha.Status.ManagedResources.HermesConfigMap = ha.GetHermesName()
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
	if hc := ha.GetHermes().GetConfig(); hc != nil {
		raw := hc.Raw
		if ha.GetSearXNG().IsEnabled() {
			var err error
			raw, err = applySearXNGConfigDefaults(raw)
			if err != nil {
				return nil, err
			}
		}
		cx := ha.GetCamofox()
		if cx.IsEnabled() && cx.GetPersistence().IsEnabled() && cx.GetPersistence().GetExistingClaim() == "" {
			var err error
			raw, err = applyCamofoxConfigDefaults(raw)
			if err != nil {
				return nil, err
			}
		}

		yamlBytes, err := sigsyaml.JSONToYAML(raw)
		if err != nil {
			return nil, err
		}
		data["config.yaml"] = string(yamlBytes)
	}

	if hw := ha.GetHermes().GetWorkspace(); hw != nil {
		for path, content := range hw.Files {
			key := "workspace." + strings.ReplaceAll(path, "/", hermesWorkspacePathSeparator)
			data[key] = content
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
