package usecase

import (
	"context"
	"fmt"
	agentsv1alpha1 "noahingh/hermes-agent-operator/api/v1alpha1"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	sigsyaml "sigs.k8s.io/yaml"
)

func (u *HermesAgentUseCase) reconcileConfigMap(ctx context.Context, ha *agentsv1alpha1.HermesAgent) error {
	cmName := ha.GetConfigMapName()
	cm, err := u.kube.GetConfigMap(ctx, GetConfigMapParam{
		NamespacedName: types.NamespacedName{Name: cmName, Namespace: ha.Namespace},
	})
	if err != nil {
		return err
	}

	desired, err := u.buildConfigMap(ha)
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

func (u *HermesAgentUseCase) buildConfigMap(ha *agentsv1alpha1.HermesAgent) (*corev1.ConfigMap, error) {
	data := map[string]string{}
	if hc := ha.GetHermes().GetConfig(); hc != nil {
		yamlBytes, err := sigsyaml.JSONToYAML(hc.Raw)
		if err != nil {
			return nil, fmt.Errorf("converting config to YAML: %w", err)
		}
		data["config.yaml"] = string(yamlBytes)
	}
	if hw := ha.GetHermes().GetWorkspace(); hw != nil {
		for path, content := range hw.Files {
			key := "workspace." + strings.ReplaceAll(path, "/", workspacePathSeparator)
			data[key] = content
		}
	}
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ha.GetConfigMapName(),
			Namespace: ha.Namespace,
		},
		Data: data,
	}, nil
}
