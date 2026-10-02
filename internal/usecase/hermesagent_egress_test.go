package usecase

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"

	agentsv1alpha1 "hermeum/hermes-agent-operator/api/v1alpha1"

	appsv1 "k8s.io/api/apps/v1"
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

func TestValidateEgressCASecret(t *testing.T) {
	caCert := certPEM(t, true)
	leafCert := certPEM(t, false)
	key := []byte("-----BEGIN PRIVATE KEY-----\nx\n-----END PRIVATE KEY-----")

	cases := []struct {
		name    string
		data    map[string][]byte
		wantErr bool
	}{
		{"valid CA", map[string][]byte{"tls.crt": caCert, "tls.key": key}, false},
		{"missing cert", map[string][]byte{"tls.key": key}, true},
		{"missing key", map[string][]byte{"tls.crt": caCert}, true},
		{"cert not PEM", map[string][]byte{"tls.crt": []byte("not a pem"), "tls.key": key}, true},
		{"cert is not a CA", map[string][]byte{"tls.crt": leafCert, "tls.key": key}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateEgressCASecret("my-ca", tc.data)
			if tc.wantErr && err == nil {
				t.Error("expected an error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("expected no error, got %v", err)
			}
		})
	}
}

func enabledEgressHA() *agentsv1alpha1.HermesAgent {
	ha := minimalHA()
	enabled := true
	ha.Spec.Egress = &agentsv1alpha1.Egress{
		Enabled:      &enabled,
		AllowedHosts: []string{"openrouter.ai"},
		Inject: []agentsv1alpha1.EgressInject{
			{
				Host: "api.example.com",
				ValueFrom: corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "mealie-secrets"},
						Key:                  "MEALIE_API_TOKEN",
					},
				},
			},
		},
	}
	return ha
}

func TestBuildEgressProxyYAML(t *testing.T) {
	t.Run("allowlist union and secrets/inject entry", func(t *testing.T) {
		ha := enabledEgressHA()

		out, err := buildEgressProxyYAML(ha)
		if err != nil {
			t.Fatal(err)
		}

		// DNS interception is off — we use the explicit HTTPS_PROXY tunnel.
		if !strings.Contains(out, "enabled: false") {
			t.Errorf("expected dns.enabled false, got:\n%s", out)
		}
		if !strings.Contains(out, "tunnel_listen: :8080") {
			t.Errorf("expected tunnel_listen :8080, got:\n%s", out)
		}
		// Allowlist must include both the explicit host and the injected host.
		if !strings.Contains(out, "openrouter.ai") {
			t.Errorf("expected allowlist domain openrouter.ai, got:\n%s", out)
		}
		if !strings.Contains(out, "api.example.com") {
			t.Errorf("expected injected host api.example.com in allowlist, got:\n%s", out)
		}
		// A secrets/inject entry for the rule, keyed by the derived env var.
		if !strings.Contains(out, "name: secrets") {
			t.Errorf("expected a secrets transform, got:\n%s", out)
		}
		if !strings.Contains(out, "var: EGRESS_CRED_0") {
			t.Errorf("expected source.var EGRESS_CRED_0, got:\n%s", out)
		}
		if !strings.Contains(out, "header: Authorization") {
			t.Errorf("expected default Authorization header, got:\n%s", out)
		}
		if !strings.Contains(out, "Bearer {{ .Value }}") {
			t.Errorf("expected default Bearer formatter, got:\n%s", out)
		}
		// MITM CA paths point at the mounted secret.
		if !strings.Contains(out, "ca_cert: /etc/iron-proxy/ca.crt") {
			t.Errorf("expected ca_cert path, got:\n%s", out)
		}
	})

	t.Run("deterministic across renders", func(t *testing.T) {
		ha := enabledEgressHA()
		a, err := buildEgressProxyYAML(ha)
		if err != nil {
			t.Fatal(err)
		}
		b, err := buildEgressProxyYAML(ha)
		if err != nil {
			t.Fatal(err)
		}
		if a != b {
			t.Errorf("proxy.yaml render must be deterministic:\n%s\n---\n%s", a, b)
		}
	})

	t.Run("no secrets transform when no inject rules", func(t *testing.T) {
		ha := minimalHA()
		enabled := true
		ha.Spec.Egress = &agentsv1alpha1.Egress{Enabled: &enabled, AllowedHosts: []string{"openrouter.ai"}}
		out, err := buildEgressProxyYAML(ha)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out, "name: secrets") {
			t.Errorf("did not expect a secrets transform, got:\n%s", out)
		}
	})
}

func TestBuildEgressCASecret(t *testing.T) {
	ha := enabledEgressHA()
	secret, err := buildEgressCASecret(ha)
	if err != nil {
		t.Fatal(err)
	}
	if secret.Name != ha.GetEgressCASecretName() {
		t.Errorf("expected name %q, got %q", ha.GetEgressCASecretName(), secret.Name)
	}
	if len(secret.Data[egressCACertKey]) == 0 {
		t.Error("expected a ca.crt in the CA secret")
	}
	if len(secret.Data[egressCAKeyKey]) == 0 {
		t.Error("expected a ca.key in the CA secret")
	}
	if !strings.Contains(string(secret.Data[egressCACertKey]), "BEGIN CERTIFICATE") {
		t.Error("expected PEM-encoded certificate")
	}
}

func TestBuildEgressContainer(t *testing.T) {
	t.Run("enabled: iron-proxy sidecar and agent proxy env", func(t *testing.T) {
		ha := enabledEgressHA()
		sts := buildStatefulSet(ha)

		proxy := findContainer(sts, "iron-proxy")
		if proxy == nil {
			t.Fatal("expected iron-proxy sidecar container")
		}
		if !hasEnvVar(proxy.Env, "EGRESS_CRED_0") {
			t.Error("expected EGRESS_CRED_0 env var on the sidecar sourced from the user secret")
		}

		c := findHermesContainer(sts)
		if c == nil {
			t.Fatal("expected hermes-agent container")
		}
		for _, name := range []string{"HTTPS_PROXY", "HTTP_PROXY", "NO_PROXY", "SSL_CERT_FILE", "REQUESTS_CA_BUNDLE", "NODE_EXTRA_CA_CERTS", "GIT_SSL_CAINFO"} {
			if !hasEnvVar(c.Env, name) {
				t.Errorf("expected %s on the hermes-agent container", name)
			}
		}
		if !hasVolumeMount(c.VolumeMounts, "egress-agent-ca") {
			t.Error("expected the CA cert mounted into the agent container")
		}
	})

	t.Run("disabled: no sidecar and no proxy env", func(t *testing.T) {
		ha := minimalHA()
		sts := buildStatefulSet(ha)

		if findContainer(sts, "iron-proxy") != nil {
			t.Error("did not expect an iron-proxy sidecar when egress is disabled")
		}
		c := findHermesContainer(sts)
		if c == nil {
			t.Fatal("expected hermes-agent container")
		}
		if hasEnvVar(c.Env, "HTTPS_PROXY") {
			t.Error("did not expect HTTPS_PROXY when egress is disabled")
		}
	})
}

// userCASecret is a sample user-supplied (e.g. cert-manager) egress CA Secret.
const userCASecret = "cm-egress-ca"

func TestGetEgressCASource(t *testing.T) {
	t.Run("managed CA by default", func(t *testing.T) {
		ha := enabledEgressHA()
		name, cert, key := ha.GetEgressCASource()
		if name != ha.GetEgressCASecretName() {
			t.Errorf("expected managed CA secret %q, got %q", ha.GetEgressCASecretName(), name)
		}
		if cert != agentsv1alpha1.EgressCAManagedCertKey || key != agentsv1alpha1.EgressCAManagedKeyKey {
			t.Errorf("expected managed keys ca.crt/ca.key, got %q/%q", cert, key)
		}
		if !ha.GetEgress().UsesManagedCA() {
			t.Error("UsesManagedCA should be true without a secretRef")
		}
	})

	t.Run("user-supplied CA via secretRef", func(t *testing.T) {
		ha := enabledEgressHA()
		ha.Spec.Egress.CA = &agentsv1alpha1.EgressCA{
			SecretRef: &agentsv1alpha1.EgressCASecretRef{Name: userCASecret},
		}
		name, cert, key := ha.GetEgressCASource()
		if name != userCASecret {
			t.Errorf("expected user CA secret %q, got %q", userCASecret, name)
		}
		if cert != agentsv1alpha1.EgressCATLSCertKey || key != agentsv1alpha1.EgressCATLSKeyKey {
			t.Errorf("expected TLS keys tls.crt/tls.key, got %q/%q", cert, key)
		}
		if ha.GetEgress().UsesManagedCA() {
			t.Error("UsesManagedCA should be false when secretRef is set")
		}
	})
}

func TestBuildEgressStatefulSetCASource(t *testing.T) {
	findVol := func(sts *appsv1.StatefulSet, name string) *corev1.Volume {
		for i := range sts.Spec.Template.Spec.Volumes {
			if sts.Spec.Template.Spec.Volumes[i].Name == name {
				return &sts.Spec.Template.Spec.Volumes[i]
			}
		}
		return nil
	}
	caSubPath := func(c *corev1.Container, mountKey string) string {
		for _, m := range c.VolumeMounts {
			if m.Name == "egress-ca" && strings.HasSuffix(m.MountPath, "/"+mountKey) {
				return m.SubPath
			}
		}
		return ""
	}

	t.Run("managed CA: operator secret + ca.crt/ca.key subpaths", func(t *testing.T) {
		ha := enabledEgressHA()
		sts := buildStatefulSet(ha)

		v := findVol(sts, "egress-ca")
		if v == nil || v.Secret == nil {
			t.Fatal("expected egress-ca secret volume")
		}
		if v.Secret.SecretName != ha.GetEgressCASecretName() {
			t.Errorf("expected managed CA secret %q, got %q", ha.GetEgressCASecretName(), v.Secret.SecretName)
		}
		proxy := findContainer(sts, "iron-proxy")
		if got := caSubPath(proxy, agentsv1alpha1.EgressCAManagedCertKey); got != agentsv1alpha1.EgressCAManagedCertKey {
			t.Errorf("expected ca.crt subpath, got %q", got)
		}
		if got := caSubPath(proxy, agentsv1alpha1.EgressCAManagedKeyKey); got != agentsv1alpha1.EgressCAManagedKeyKey {
			t.Errorf("expected ca.key subpath, got %q", got)
		}
	})

	t.Run("user CA: referenced secret + tls.crt/tls.key subpaths, constant mount names", func(t *testing.T) {
		ha := enabledEgressHA()
		ha.Spec.Egress.CA = &agentsv1alpha1.EgressCA{
			SecretRef: &agentsv1alpha1.EgressCASecretRef{Name: userCASecret},
		}
		sts := buildStatefulSet(ha)

		v := findVol(sts, "egress-ca")
		if v == nil || v.Secret == nil || v.Secret.SecretName != userCASecret {
			t.Fatalf("expected egress-ca volume from user secret %q, got %+v", userCASecret, v)
		}
		proxy := findContainer(sts, "iron-proxy")
		// Mount file name stays ca.crt/ca.key; only the source SubPath changes.
		if got := caSubPath(proxy, agentsv1alpha1.EgressCAManagedCertKey); got != agentsv1alpha1.EgressCATLSCertKey {
			t.Errorf("expected tls.crt subpath mounted at ca.crt, got %q", got)
		}
		if got := caSubPath(proxy, agentsv1alpha1.EgressCAManagedKeyKey); got != agentsv1alpha1.EgressCATLSKeyKey {
			t.Errorf("expected tls.key subpath mounted at ca.key, got %q", got)
		}

		// Agent-side: cert only, from the user secret, keyed tls.crt -> ca.crt.
		av := findVol(sts, "egress-agent-ca")
		if av == nil || av.Secret == nil || av.Secret.SecretName != userCASecret {
			t.Fatalf("expected agent CA volume from user secret, got %+v", av)
		}
		if len(av.Secret.Items) != 1 ||
			av.Secret.Items[0].Key != agentsv1alpha1.EgressCATLSCertKey ||
			av.Secret.Items[0].Path != agentsv1alpha1.EgressCAManagedCertKey {
			t.Errorf("expected agent CA item tls.crt->ca.crt, got %+v", av.Secret.Items)
		}
	})
}
