package usecase

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"slices"
	"strings"
	"testing"
	"time"

	agentsv1alpha1 "hermeum/hermes-agent-operator/api/v1alpha1"

	corev1 "k8s.io/api/core/v1"
)

// certPEM generates a self-signed certificate PEM for tests. isCA controls the
// CA basic constraint and, when true, the certSign key usage.
func certPEM(t *testing.T, isCA bool) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  isCA,
		BasicConstraintsValid: true,
	}
	if isCA {
		tmpl.KeyUsage = x509.KeyUsageCertSign | x509.KeyUsageCRLSign
	} else {
		tmpl.KeyUsage = x509.KeyUsageDigitalSignature
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func TestReconcileEgressCA(t *testing.T) {
	const userCA = "cm-ca"
	ha := egressHA(nil)
	managed := ha.GetEgressCASecretName()
	userCASecret := &corev1.Secret{Data: map[string][]byte{
		agentsv1alpha1.EgressCATLSCertKey: certPEM(t, true),
		agentsv1alpha1.EgressCATLSKeyKey:  []byte("key"),
	}}

	tests := []struct {
		name          string
		enabled       bool
		caRef         string
		secrets       map[string]*corev1.Secret
		expectErr     bool
		expectDeleted []string
		expectStatus  string
		expectManaged bool
	}{
		{
			name:          "generates the managed ca",
			enabled:       true,
			expectStatus:  managed,
			expectManaged: true,
		},
		{
			name:          "keeps an existing managed ca",
			enabled:       true,
			secrets:       map[string]*corev1.Secret{managed: {Data: map[string][]byte{"ca.crt": []byte("kept")}}},
			expectStatus:  managed,
			expectManaged: true,
		},
		{
			name:          "switching to a user ca deletes the managed one",
			enabled:       true,
			caRef:         userCA,
			secrets:       map[string]*corev1.Secret{managed: {}, userCA: userCASecret},
			expectDeleted: []string{managed},
		},
		{
			name:    "a user ca named like the managed one is never deleted",
			enabled: true,
			caRef:   managed,
			secrets: map[string]*corev1.Secret{managed: userCASecret},
		},
		{
			name:      "a missing user ca fails",
			enabled:   true,
			caRef:     userCA,
			expectErr: true,
		},
		{
			name:      "a user ca that is not a ca fails",
			enabled:   true,
			caRef:     userCA,
			secrets:   map[string]*corev1.Secret{userCA: {Data: map[string][]byte{"tls.crt": certPEM(t, false), "tls.key": []byte("key")}}},
			expectErr: true,
		},
		{
			name:          "disabling deletes the managed ca",
			secrets:       map[string]*corev1.Secret{managed: {}},
			expectDeleted: []string{managed},
		},
		{
			name:    "disabling never deletes a user ca named like the managed one",
			caRef:   managed,
			secrets: map[string]*corev1.Secret{managed: userCASecret},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := ha.DeepCopy()
			agent.Spec.Egress.Enabled = tt.enabled
			if tt.caRef != "" {
				agent.Spec.Egress.CA = &agentsv1alpha1.EgressCA{SecretRef: &agentsv1alpha1.EgressSecretRef{Name: tt.caRef}}
			}
			secrets := map[string]*corev1.Secret{}
			for k, v := range tt.secrets {
				secrets[k] = v.DeepCopy()
			}
			kube := newFakeEgressKube(secrets)

			_, err := NewHermesAgentUseCase(kube, silentTelemetry{}).reconcileEgressCA(context.Background(), agent)
			if (err != nil) != tt.expectErr {
				t.Fatalf("got error %v, expected error: %v", err, tt.expectErr)
			}
			if tt.expectErr {
				return
			}
			if !slices.Equal(kube.deleted, tt.expectDeleted) {
				t.Errorf("deleted %v, expected %v", kube.deleted, tt.expectDeleted)
			}
			if got := agent.Status.ManagedResources.EgressCASecret; got != tt.expectStatus {
				t.Errorf("status records %q, expected %q", got, tt.expectStatus)
			}
			if tt.expectManaged && len(kube.secrets[managed].Data[agentsv1alpha1.EgressCAManagedCertKey]) == 0 {
				t.Error("expected the managed ca secret to hold a certificate")
			}
			if tt.caRef == managed && kube.secrets[managed] == nil {
				t.Error("the user's ca must survive")
			}
		})
	}

	t.Run("an existing managed ca is never regenerated", func(t *testing.T) {
		kube := newFakeEgressKube(map[string]*corev1.Secret{managed: {Data: map[string][]byte{"ca.crt": []byte("kept")}}})
		if _, err := NewHermesAgentUseCase(kube, silentTelemetry{}).reconcileEgressCA(context.Background(), ha.DeepCopy()); err != nil {
			t.Fatal(err)
		}
		if string(kube.secrets[managed].Data["ca.crt"]) != "kept" {
			t.Error("expected the existing ca to be kept")
		}
	})
}

func TestValidateEgressCASecret(t *testing.T) {
	caCert := certPEM(t, true)
	leafCert := certPEM(t, false)
	key := []byte("-----BEGIN PRIVATE KEY-----\nx\n-----END PRIVATE KEY-----")

	tests := []struct {
		name    string
		data    map[string][]byte
		wantErr bool
	}{
		{"valid ca", map[string][]byte{"tls.crt": caCert, "tls.key": key}, false},
		{"missing cert", map[string][]byte{"tls.key": key}, true},
		{"missing key", map[string][]byte{"tls.crt": caCert}, true},
		{"cert not pem", map[string][]byte{"tls.crt": []byte("not a pem"), "tls.key": key}, true},
		{"cert is not a ca", map[string][]byte{"tls.crt": leafCert, "tls.key": key}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateEgressCASecret("my-ca", tt.data)
			if (err != nil) != tt.wantErr {
				t.Errorf("got error %v, expected error: %v", err, tt.wantErr)
			}
		})
	}
}

func TestBuildEgressCASecret(t *testing.T) {
	ha := egressHA(nil)
	secret, err := buildEgressCASecret(ha)
	if err != nil {
		t.Fatal(err)
	}
	if secret.Name != ha.GetEgressCASecretName() {
		t.Errorf("expected name %q, got %q", ha.GetEgressCASecretName(), secret.Name)
	}
	if err := validateEgressCASecret("managed", map[string][]byte{
		agentsv1alpha1.EgressCATLSCertKey: secret.Data[agentsv1alpha1.EgressCAManagedCertKey],
		agentsv1alpha1.EgressCATLSKeyKey:  secret.Data[agentsv1alpha1.EgressCAManagedKeyKey],
	}); err != nil {
		t.Errorf("the generated ca must pass the user ca validation: %v", err)
	}
	if !strings.Contains(string(secret.Data[agentsv1alpha1.EgressCAManagedKeyKey]), "PRIVATE KEY") {
		t.Error("expected a pem private key under ca.key")
	}
}
