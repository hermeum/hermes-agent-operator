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

func (u *HermesAgentUseCase) reconcileHermesSecret(ctx context.Context, ha *agentsv1alpha1.HermesAgent) (ctrl.Result, error) {
	nsName := types.NamespacedName{Namespace: ha.Namespace, Name: ha.Name}
	secretNsName := types.NamespacedName{Name: ha.GetHermesName(), Namespace: ha.Namespace}

	existing, err := u.kube.GetSecret(ctx, GetSecretParam{NamespacedName: secretNsName})
	if err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}

	if existing != nil {
		// One-time migration: add WEBHOOK_SECRET if the secret predates this feature.
		if _, ok := existing.Data["WEBHOOK_SECRET"]; !ok {
			raw := make([]byte, 32)
			if _, err := rand.Read(raw); err != nil {
				return ctrl.Result{RequeueAfter: 30 * time.Second}, err
			}
			existing.Data["WEBHOOK_SECRET"] = []byte(fmt.Sprintf("%x", raw))
			if err := u.kube.UpdateSecretOwnedByHermesAgent(ctx, UpdateSecretOfHermesAgentParam{HermesAgent: ha, Secret: existing}); err != nil {
				return ctrl.Result{RequeueAfter: 30 * time.Second}, err
			}
			u.tel.Debug(ctx, "Hermes Secret patched with WEBHOOK_SECRET", "namespacedName", nsName)
		}
		ha.Status.ManagedResources.HermesSecret = ha.GetHermesName()
		return ctrl.Result{}, nil
	}

	secret, err := buildHermesSecret(ha)
	if err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}
	err = u.kube.CreateSecretOwnedByHermesAgent(ctx, CreateSecretOfHermesAgentParam{HermesAgent: ha, Secret: secret})
	if err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}
	u.tel.Debug(ctx, "Hermes Secret created", "namespacedName", nsName)
	ha.Status.ManagedResources.HermesSecret = ha.GetHermesName()
	return ctrl.Result{}, nil
}

func buildHermesSecret(ha *agentsv1alpha1.HermesAgent) (*corev1.Secret, error) {
	apiKey := make([]byte, 32)
	webhookSecret := make([]byte, 32)
	if _, err := rand.Read(apiKey); err != nil {
		return nil, err
	}
	if _, err := rand.Read(webhookSecret); err != nil {
		return nil, err
	}
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ha.GetHermesName(),
			Namespace: ha.Namespace,
			Labels:    resourceLabels(ha),
		},
		Data: map[string][]byte{
			"API_SERVER_KEY": []byte(fmt.Sprintf("%x", apiKey)),
			"WEBHOOK_SECRET": []byte(fmt.Sprintf("%x", webhookSecret)),
		},
	}, nil
}
