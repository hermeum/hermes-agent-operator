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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"

	agentsv1alpha1 "hermeum/hermes-agent-operator/api/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	// egressCACertKey and egressCAKeyKey are the Secret data keys holding the
	// iron-proxy MITM CA certificate and private key.
	egressCACertKey = "ca.crt"
	egressCAKeyKey  = "ca.key"
)

// reconcileEgressCA manages *only* the operator's own self-signed iron-proxy MITM
// CA Secret. It never creates or deletes a Secret the user supplied through
// spec.egress.ca.secretRef, even when that Secret is named exactly like the
// managed one. Concretely:
//
//   - enabled, no secretRef: generate the managed CA once. It is preserved across
//     reconciles (like the Hermes API server key) so the agent's trusted CA and the
//     proxy's signing CA never drift apart, which would break TLS for every
//     existing connection.
//   - enabled with a secretRef: validate the referenced Secret and leave it alone.
//     A previously operator-managed CA is deleted, unless its name collides with
//     the reference.
//   - disabled: delete the managed CA, again unless the name collides with a
//     secretRef.
func (u *HermesAgentUseCase) reconcileEgressCA(ctx context.Context, ha *agentsv1alpha1.HermesAgent) (result ctrl.Result, err error) {
	defer func() {
		if err != nil {
			err = u.markReconcileFailed(ctx, ha, condReasonEgressCAFailed, err)
		}
	}()

	secretNsName := types.NamespacedName{Name: ha.GetEgressCASecretName(), Namespace: ha.Namespace}

	existing, err := u.kube.GetSecret(ctx, GetSecretParam{NamespacedName: secretNsName})
	if err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}

	if !ha.GetEgress().IsEnabled() {
		// Same managed-name collision guard as the secretRef path below: a Secret the
		// user pointed us at is never ours to delete, disabled or not.
		userOwned := ha.GetEgress().GetCASecretRefName() == ha.GetEgressCASecretName()
		if existing != nil && !userOwned {
			if err := u.kube.DeleteSecret(ctx, DeleteSecretParam{NamespacedName: secretNsName}); err != nil {
				return ctrl.Result{RequeueAfter: 30 * time.Second}, err
			}
			u.tel.Debug(ctx, "Egress CA Secret deleted")
		}
		ha.Status.ManagedResources.EgressCASecret = ""
		if err := u.kube.UpdateHermesAgentStatus(ctx, UpdateHermesAgentStatusParam{HermesAgent: ha}); err != nil {
			return ctrl.Result{RequeueAfter: 30 * time.Second}, err
		}
		return ctrl.Result{}, nil
	}

	// User-supplied CA: validate the referenced Secret, drop any previously
	// operator-managed CA, and record that none is operator-owned.
	if refName := ha.GetEgress().GetCASecretRefName(); refName != "" {
		userNsName := types.NamespacedName{Name: refName, Namespace: ha.Namespace}
		userSecret, err := u.kube.GetSecret(ctx, GetSecretParam{NamespacedName: userNsName})
		if err != nil {
			return ctrl.Result{RequeueAfter: 30 * time.Second}, err
		}
		if userSecret == nil {
			return ctrl.Result{RequeueAfter: 30 * time.Second}, fmt.Errorf("egress CA Secret %q not found", refName)
		}
		if err := validateEgressCASecret(refName, userSecret.Data); err != nil {
			return ctrl.Result{RequeueAfter: 30 * time.Second}, err
		}
		// Drop a previously operator-managed CA — but never the referenced Secret
		// itself, in case the user named it the same as the managed one.
		if existing != nil && refName != ha.GetEgressCASecretName() {
			if err := u.kube.DeleteSecret(ctx, DeleteSecretParam{NamespacedName: secretNsName}); err != nil {
				return ctrl.Result{RequeueAfter: 30 * time.Second}, err
			}
			u.tel.Debug(ctx, "operator-managed Egress CA Secret deleted (switched to user-supplied CA)")
		}
		ha.Status.ManagedResources.EgressCASecret = ""
		if err := u.kube.UpdateHermesAgentStatus(ctx, UpdateHermesAgentStatusParam{HermesAgent: ha}); err != nil {
			return ctrl.Result{RequeueAfter: 30 * time.Second}, err
		}
		return ctrl.Result{}, nil
	}

	// Never regenerate — preserve the CA across reconciles.
	if existing != nil {
		return ctrl.Result{}, nil
	}

	secret, err := buildEgressCASecret(ha)
	if err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}
	if err := u.kube.CreateSecretOwnedByHermesAgent(ctx, CreateSecretOfHermesAgentParam{HermesAgent: ha, Secret: secret}); err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}
	u.tel.Debug(ctx, "Egress CA Secret created")
	ha.Status.ManagedResources.EgressCASecret = ha.GetEgressCASecretName()
	if err := u.kube.UpdateHermesAgentStatus(ctx, UpdateHermesAgentStatusParam{HermesAgent: ha}); err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}
	return ctrl.Result{}, nil
}

// validateEgressCASecret checks the user-supplied Secret has a TLS cert + key
// and that the cert is a CA — iron-proxy signs leaf certs, so a non-CA cert
// would pass a presence check but break the MITM at runtime.
func validateEgressCASecret(refName string, data map[string][]byte) error {
	certPEM := data[agentsv1alpha1.EgressCATLSCertKey]
	if len(certPEM) == 0 {
		return fmt.Errorf("egress CA Secret %q missing key %q", refName, agentsv1alpha1.EgressCATLSCertKey)
	}
	if len(data[agentsv1alpha1.EgressCATLSKeyKey]) == 0 {
		return fmt.Errorf("egress CA Secret %q missing key %q", refName, agentsv1alpha1.EgressCATLSKeyKey)
	}

	block, _ := pem.Decode(certPEM)
	if block == nil {
		return fmt.Errorf("egress CA Secret %q key %q is not PEM-encoded", refName, agentsv1alpha1.EgressCATLSCertKey)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return fmt.Errorf("egress CA Secret %q: parsing certificate: %w", refName, err)
	}
	if !cert.IsCA {
		return fmt.Errorf("egress CA Secret %q certificate is not a CA (isCA=false); iron-proxy needs a CA to sign leaf certs — issue it with cert-manager isCA: true", refName)
	}
	// KeyUsage is optional in X.509; when set it must permit certificate signing.
	if cert.KeyUsage != 0 && cert.KeyUsage&x509.KeyUsageCertSign == 0 {
		return fmt.Errorf("egress CA Secret %q certificate lacks the certSign key usage", refName)
	}
	return nil
}

// buildEgressCASecret generates a self-signed CA (ECDSA P-256, ~10 year
// validity) whose certificate and key iron-proxy uses to mint leaf certs for
// its MITM proxy. The certificate is a CA with cert-signing key usage.
func buildEgressCASecret(ha *agentsv1alpha1.HermesAgent) (*corev1.Secret, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generating egress CA key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("generating egress CA serial: %w", err)
	}

	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "hermes-agent iron-proxy CA"},
		NotBefore:             now.Add(-1 * time.Hour),
		NotAfter:              now.AddDate(10, 0, 0),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("creating egress CA certificate: %w", err)
	}

	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("marshaling egress CA key: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ha.GetEgressCASecretName(),
			Namespace: ha.Namespace,
			Labels:    resourceLabels(ha),
		},
		Data: map[string][]byte{
			egressCACertKey: certPEM,
			egressCAKeyKey:  keyPEM,
		},
	}, nil
}
