package usecase

import (
	"context"
	"crypto/rand"
	"fmt"
	"time"

	agentsv1alpha1 "hermeum/hermes-agent-operator/api/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

func (u *HermesAgentUseCase) reconcileSearXNGSecret(ctx context.Context, ha *agentsv1alpha1.HermesAgent) (ctrl.Result, error) {
	secretNsName := types.NamespacedName{Name: ha.GetSearXNGName(), Namespace: ha.Namespace}

	existing, err := u.kube.GetSecret(ctx, GetSecretParam{NamespacedName: secretNsName})
	if err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}

	if !ha.GetSearXNG().IsEnabled() {
		if existing != nil {
			err := u.kube.DeleteSecret(ctx, DeleteSecretParam{NamespacedName: secretNsName})
			if err != nil {
				return ctrl.Result{RequeueAfter: 30 * time.Second}, err
			}
			u.tel.Debug(ctx, "SearXNG Secret deleted")
		}
		ha.Status.ManagedResources.SearXNGSecret = ""
		if err := u.kube.UpdateHermesAgentStatus(ctx, UpdateHermesAgentStatusParam{HermesAgent: ha}); err != nil {
			return ctrl.Result{RequeueAfter: 30 * time.Second}, err
		}
		return ctrl.Result{}, nil
	}

	// Never update — preserve the generated secret value across reconciles.
	if existing != nil {
		return ctrl.Result{}, nil
	}

	secret, err := buildSearXNGSecret(ha)
	if err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}
	err = u.kube.CreateSecretOwnedByHermesAgent(ctx, CreateSecretOfHermesAgentParam{HermesAgent: ha, Secret: secret})
	if err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}
	u.tel.Debug(ctx, "SearXNG Secret created")
	ha.Status.ManagedResources.SearXNGSecret = ha.GetSearXNGName()
	if err := u.kube.UpdateHermesAgentStatus(ctx, UpdateHermesAgentStatusParam{HermesAgent: ha}); err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}
	return ctrl.Result{}, nil
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
