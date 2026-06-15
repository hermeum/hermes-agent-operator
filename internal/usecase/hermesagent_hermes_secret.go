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
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"maps"
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

	desired, err := buildHermesSecret(ha, existing)
	if err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}

	created := existing == nil
	if created {
		err = u.kube.CreateSecretOwnedByHermesAgent(ctx, CreateSecretOfHermesAgentParam{HermesAgent: ha, Secret: desired})
		if err != nil {
			return ctrl.Result{RequeueAfter: 30 * time.Second}, err
		}
		u.tel.Debug(ctx, "Hermes Secret created", "namespacedName", nsName)
	} else if !maps.EqualFunc(existing.Data, desired.Data, bytes.Equal) {
		existing.Data = desired.Data
		if err := u.kube.UpdateSecretOwnedByHermesAgent(ctx, UpdateSecretOfHermesAgentParam{HermesAgent: ha, Secret: existing}); err != nil {
			return ctrl.Result{RequeueAfter: 30 * time.Second}, err
		}
		u.tel.Debug(ctx, "Hermes Secret updated", "namespacedName", nsName)
	}

	if created {
		ha.Status.ManagedResources.HermesSecret = ha.GetHermesName()
		if err := u.kube.UpdateHermesAgentStatus(ctx, UpdateHermesAgentStatusParam{HermesAgent: ha}); err != nil {
			return ctrl.Result{RequeueAfter: 30 * time.Second}, err
		}
	}
	return ctrl.Result{}, nil
}

// buildHermesSecret computes the desired Secret data. When existing is non-nil,
// stable keys (API_SERVER_KEY, WEBHOOK_SECRET) are preserved to avoid
// rotating credentials on every reconcile.
func buildHermesSecret(ha *agentsv1alpha1.HermesAgent, existing *corev1.Secret) (*corev1.Secret, error) {
	data := map[string][]byte{}

	if ha.GetHermes().GetAPIServer().IsEnabled() {
		// Preserve API_SERVER_KEY; generate once when API server is first enabled.
		if existing != nil {
			data["API_SERVER_KEY"] = existing.Data["API_SERVER_KEY"]
		}
		if len(data["API_SERVER_KEY"]) == 0 {
			raw := make([]byte, 32)
			if _, err := rand.Read(raw); err != nil {
				return nil, err
			}
			data["API_SERVER_KEY"] = []byte(fmt.Sprintf("%x", raw))
		}
	}
	// When API server is disabled, API_SERVER_KEY is intentionally omitted.

	if ha.GetHermes().GetWebhook().IsEnabled() {
		// Preserve WEBHOOK_SECRET; generate once when webhook is first enabled.
		if existing != nil {
			data["WEBHOOK_SECRET"] = existing.Data["WEBHOOK_SECRET"]
		}
		if len(data["WEBHOOK_SECRET"]) == 0 {
			raw := make([]byte, 32)
			if _, err := rand.Read(raw); err != nil {
				return nil, err
			}
			data["WEBHOOK_SECRET"] = []byte(fmt.Sprintf("%x", raw))
		}
	}
	// When webhook is disabled, WEBHOOK_SECRET is intentionally omitted.

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ha.GetHermesName(),
			Namespace: ha.Namespace,
			Labels:    resourceLabels(ha),
		},
		Data: data,
	}, nil
}
