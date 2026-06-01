package usecase

import agentsv1alpha1 "noahingh/hermes-agent-operator/api/v1alpha1"

const (
	labelName      = "app.kubernetes.io/name"
	labelInstance  = "app.kubernetes.io/instance"
	labelManagedBy = "app.kubernetes.io/managed-by"

	appNameValue   = "hermes-agent"
	managedByValue = "hermes-agent-operator"
)

func resourceLabels(ha *agentsv1alpha1.HermesAgent) map[string]string {
	return map[string]string{
		labelName:      appNameValue,
		labelInstance:  ha.Name,
		labelManagedBy: managedByValue,
	}
}

func selectorLabels(ha *agentsv1alpha1.HermesAgent) map[string]string {
	return map[string]string{
		labelName:     appNameValue,
		labelInstance: ha.Name,
	}
}
