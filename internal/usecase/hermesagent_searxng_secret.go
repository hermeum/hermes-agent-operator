package usecase

import (
	"context"
	"crypto/rand"
	"fmt"
	"time"

	agentsv1alpha1 "noahingh/hermes-agent-operator/api/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

func (u *HermesAgentUseCase) reconcileSearXNGSecret(ctx context.Context, ha *agentsv1alpha1.HermesAgent) (ctrl.Result, error) {
	nsName := types.NamespacedName{Namespace: ha.Namespace, Name: ha.Name}
	secretNsName := types.NamespacedName{Name: ha.GetSearXNGName(), Namespace: ha.Namespace}

	existing, err := u.kube.GetSecret(ctx, GetSecretParam{NamespacedName: secretNsName})
	if err != nil {
		u.tel.Error(ctx, err, "Failed to get SearXNG Secret", "namespacedName", nsName)
		u.tel.IncReconcile(ctx, IncReconcileParam{NamespacedName: nsName, Result: ResultError})
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}

	if !ha.GetSearXNG().IsEnabled() {
		if existing == nil {
			return ctrl.Result{}, nil
		}
		err := u.kube.DeleteSecret(ctx, DeleteSecretParam{NamespacedName: secretNsName})
		u.tel.IncSecretOperation(ctx, IncSecretOperationParam{NamespacedName: nsName, Operation: OperationDelete, Result: resultOf(err)})
		if err != nil {
			u.tel.Error(ctx, err, "Failed to delete SearXNG Secret", "namespacedName", nsName)
			u.tel.IncReconcile(ctx, IncReconcileParam{NamespacedName: nsName, Result: ResultError})
			return ctrl.Result{RequeueAfter: 30 * time.Second}, err
		}
		u.tel.Debug(ctx, "SearXNG Secret deleted", "namespacedName", nsName)
		return ctrl.Result{}, nil
	}

	// Never update — preserve the generated secret value across reconciles.
	if existing != nil {
		return ctrl.Result{}, nil
	}

	secret, err := buildSearXNGSecret(ha)
	if err != nil {
		u.tel.Error(ctx, err, "Failed to build SearXNG Secret", "namespacedName", nsName)
		u.tel.IncReconcile(ctx, IncReconcileParam{NamespacedName: nsName, Result: ResultError})
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}
	err = u.kube.CreateSecretOwnedByHermesAgent(ctx, CreateSecretOfHermesAgentParam{HermesAgent: ha, Secret: secret})
	u.tel.IncSecretOperation(ctx, IncSecretOperationParam{NamespacedName: nsName, Operation: OperationCreate, Result: resultOf(err)})
	if err != nil {
		u.tel.Error(ctx, err, "Failed to create SearXNG Secret", "namespacedName", nsName)
		u.tel.IncReconcile(ctx, IncReconcileParam{NamespacedName: nsName, Result: ResultError})
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}
	u.tel.Debug(ctx, "SearXNG Secret created", "namespacedName", nsName)
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
