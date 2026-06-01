package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	agentsv1alpha1 "noahingh/hermes-agent-operator/api/v1alpha1"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	sigsyaml "sigs.k8s.io/yaml"
)

func (u *HermesAgentUseCase) reconcileHermesConfigMap(ctx context.Context, ha *agentsv1alpha1.HermesAgent) error {
	cmName := ha.GetHermesName()
	cm, err := u.kube.GetConfigMap(ctx, GetConfigMapParam{
		NamespacedName: types.NamespacedName{Name: cmName, Namespace: ha.Namespace},
	})
	if err != nil {
		return err
	}

	desired, err := u.buildHermesConfigMap(ha)
	if err != nil {
		return err
	}

	if cm != nil {
		desired.ResourceVersion = cm.ResourceVersion
		err := u.kube.UpdateConfigMapOwnedByHermesAgent(ctx, UpdateConfigMapParam{HermesAgent: ha, ConfigMap: desired})
		u.tel.IncConfigMapOperation(ctx, IncConfigMapOperationParam{Operation: OperationUpdate, Result: resultOf(err)})
		return err
	}

	err = u.kube.CreateConfigMapOwnedByHermesAgent(ctx, CreateConfigMapOfHermesAgentParam{
		HermesAgent: ha,
		ConfigMap:   desired,
	})
	u.tel.IncConfigMapOperation(ctx, IncConfigMapOperationParam{Operation: OperationCreate, Result: resultOf(err)})
	return err
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

func (u *HermesAgentUseCase) buildHermesConfigMap(ha *agentsv1alpha1.HermesAgent) (*corev1.ConfigMap, error) {
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
		},
		Data: data,
	}, nil
}
