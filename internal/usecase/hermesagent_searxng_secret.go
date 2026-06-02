package usecase

import (
	"context"
	"crypto/rand"
	"fmt"
	agentsv1alpha1 "noahingh/hermes-agent-operator/api/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func (u *HermesAgentUseCase) reconcileSearXNGSecret(ctx context.Context, ha *agentsv1alpha1.HermesAgent) error {
	nsName := types.NamespacedName{Name: ha.GetSearXNGName(), Namespace: ha.Namespace}

	existing, err := u.kube.GetSecret(ctx, GetSecretParam{NamespacedName: nsName})
	if err != nil {
		return err
	}

	if !ha.GetSearXNG().IsEnabled() {
		if existing == nil {
			return nil
		}
		err := u.kube.DeleteSecret(ctx, DeleteSecretParam{NamespacedName: nsName})
		u.tel.IncSecretOperation(ctx, IncSecretOperationParam{NamespacedName: types.NamespacedName{Namespace: ha.Namespace, Name: ha.Name}, Operation: OperationDelete, Result: resultOf(err)})
		return err
	}

	// Never update — preserve the generated secret value across reconciles.
	if existing != nil {
		return nil
	}

	secret, err := buildSearXNGSecret(ha)
	if err != nil {
		return err
	}
	err = u.kube.CreateSecretOwnedByHermesAgent(ctx, CreateSecretOfHermesAgentParam{HermesAgent: ha, Secret: secret})
	u.tel.IncSecretOperation(ctx, IncSecretOperationParam{NamespacedName: types.NamespacedName{Namespace: ha.Namespace, Name: ha.Name}, Operation: OperationCreate, Result: resultOf(err)})
	return err
}

func buildSearXNGSecret(ha *agentsv1alpha1.HermesAgent) (*corev1.Secret, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ha.GetSearXNGName(),
			Namespace: ha.Namespace,
			Labels:    resourceLabels(ha),
		},
		Data: map[string][]byte{
			"SEARXNG_SECRET": []byte(fmt.Sprintf("%x", raw)),
		},
	}, nil
}
