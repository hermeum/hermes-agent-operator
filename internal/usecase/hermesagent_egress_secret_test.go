package usecase

import (
	"context"
	"slices"
	"strings"
	"testing"

	agentsv1alpha1 "hermeum/hermes-agent-operator/api/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"
)

const egressCredsSecret = "llm-keys"

// egressHA returns an agent with egress enabled, reading credentials from
// egressCredsSecret, with the given config.
func egressHA(cfg *agentsv1alpha1.EgressConfig) *agentsv1alpha1.HermesAgent {
	ha := minimalHA()
	ha.Spec.Egress = &agentsv1alpha1.Egress{
		Enabled:     true,
		Credentials: &agentsv1alpha1.EgressCredentials{SecretRef: agentsv1alpha1.EgressSecretRef{Name: egressCredsSecret}},
		Config:      cfg,
	}
	return ha
}

// fakeEgressKube serves Secrets from memory. Only the methods the egress
// reconcilers touch are implemented; the rest panic through the nil embedded
// interface.
type fakeEgressKube struct {
	Kubernetes

	secrets       map[string]*corev1.Secret
	deleted       []string
	statusUpdates int
}

func newFakeEgressKube(secrets map[string]*corev1.Secret) *fakeEgressKube {
	if secrets == nil {
		secrets = map[string]*corev1.Secret{}
	}
	return &fakeEgressKube{secrets: secrets}
}

func (f *fakeEgressKube) GetSecret(_ context.Context, param GetSecretParam) (*corev1.Secret, error) {
	return f.secrets[param.NamespacedName.Name], nil
}
func (f *fakeEgressKube) CreateSecretOwnedByHermesAgent(_ context.Context, param CreateSecretOfHermesAgentParam) error {
	f.secrets[param.Secret.Name] = param.Secret
	return nil
}
func (f *fakeEgressKube) UpdateSecretOwnedByHermesAgent(_ context.Context, param UpdateSecretOfHermesAgentParam) error {
	f.secrets[param.Secret.Name] = param.Secret
	return nil
}
func (f *fakeEgressKube) DeleteSecret(_ context.Context, param DeleteSecretParam) error {
	f.deleted = append(f.deleted, param.NamespacedName.Name)
	delete(f.secrets, param.NamespacedName.Name)
	return nil
}
func (f *fakeEgressKube) UpdateHermesAgentStatus(context.Context, UpdateHermesAgentStatusParam) error {
	f.statusUpdates++
	return nil
}

func credsSecret(keys ...string) *corev1.Secret {
	data := map[string][]byte{}
	for _, k := range keys {
		data[k] = []byte("real-" + k)
	}
	return &corev1.Secret{Data: data}
}

func TestReconcileEgressSecret(t *testing.T) {
	t.Run("creates the secret from the credentials secret keys", func(t *testing.T) {
		kube := newFakeEgressKube(map[string]*corev1.Secret{egressCredsSecret: credsSecret("OPENROUTER_API_KEY")})
		ha := egressHA(nil)
		if _, err := NewHermesAgentUseCase(kube, silentTelemetry{}).reconcileEgressSecret(context.Background(), ha); err != nil {
			t.Fatal(err)
		}
		got := kube.secrets[ha.GetEgressSecretName()]
		if got == nil || len(got.Data["OPENROUTER_API_KEY"]) == 0 {
			t.Fatalf("expected the egress secret with an OPENROUTER_API_KEY token, got %+v", got)
		}
		if string(got.Data["OPENROUTER_API_KEY"]) == "real-OPENROUTER_API_KEY" {
			t.Error("the egress secret must hold a proxy token, never the real key")
		}
		if ha.Status.ManagedResources.EgressSecret != ha.GetEgressSecretName() {
			t.Errorf("expected status to record the egress secret, got %q", ha.Status.ManagedResources.EgressSecret)
		}
	})

	t.Run("writes the status only when it changes", func(t *testing.T) {
		kube := newFakeEgressKube(map[string]*corev1.Secret{egressCredsSecret: credsSecret("OPENROUTER_API_KEY")})
		uc := NewHermesAgentUseCase(kube, silentTelemetry{})
		ha := egressHA(nil)
		for range 3 {
			if _, err := uc.reconcileEgressSecret(context.Background(), ha); err != nil {
				t.Fatal(err)
			}
		}
		off := minimalHA()
		for range 3 {
			if _, err := uc.reconcileEgressSecret(context.Background(), off); err != nil {
				t.Fatal(err)
			}
		}
		if kube.statusUpdates != 1 {
			t.Errorf("expected a single status write, got %d", kube.statusUpdates)
		}
	})

	t.Run("refuses a tunnel port another listener in the pod uses", func(t *testing.T) {
		port := int32(8080)
		ha := egressHA(&agentsv1alpha1.EgressConfig{TunnelPort: &port})
		ha.Spec.SearXNG = &agentsv1alpha1.SearXNG{Enabled: true}
		kube := newFakeEgressKube(map[string]*corev1.Secret{egressCredsSecret: credsSecret("OPENROUTER_API_KEY")})
		if _, err := NewHermesAgentUseCase(kube, silentTelemetry{}).reconcileEgressSecret(context.Background(), ha); err == nil {
			t.Fatal("expected an error")
		}
		if kube.secrets[ha.GetEgressSecretName()] != nil {
			t.Error("expected no egress secret before the port is fixed")
		}
	})

	t.Run("fails while the credentials secret is missing", func(t *testing.T) {
		kube := newFakeEgressKube(nil)
		if _, err := NewHermesAgentUseCase(kube, silentTelemetry{}).reconcileEgressSecret(context.Background(), egressHA(nil)); err == nil {
			t.Error("expected an error")
		}
	})

	t.Run("refuses a credentials secret named like the egress secret", func(t *testing.T) {
		ha := egressHA(nil)
		ha.Spec.Egress.Credentials.SecretRef.Name = ha.GetEgressSecretName()
		user := credsSecret("OPENROUTER_API_KEY")
		kube := newFakeEgressKube(map[string]*corev1.Secret{ha.GetEgressSecretName(): user})
		if _, err := NewHermesAgentUseCase(kube, silentTelemetry{}).reconcileEgressSecret(context.Background(), ha); err == nil {
			t.Fatal("expected an error")
		}
		if kube.secrets[ha.GetEgressSecretName()] != user || string(user.Data["OPENROUTER_API_KEY"]) != "real-OPENROUTER_API_KEY" {
			t.Error("the user's secret must be left untouched")
		}
	})

	t.Run("deletes the secret once egress is disabled", func(t *testing.T) {
		ha := egressHA(nil)
		kube := newFakeEgressKube(map[string]*corev1.Secret{ha.GetEgressSecretName(): {}})
		ha.Spec.Egress.Enabled = false
		if _, err := NewHermesAgentUseCase(kube, silentTelemetry{}).reconcileEgressSecret(context.Background(), ha); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(kube.deleted, []string{ha.GetEgressSecretName()}) {
			t.Errorf("expected the egress secret deleted, got %v", kube.deleted)
		}
	})
}

func renderEgressConfig(t *testing.T, ha *agentsv1alpha1.HermesAgent, keys []string, existing *corev1.Secret) (*corev1.Secret, ironProxyConfig) {
	t.Helper()
	creds, err := ha.GetEgress().ResolveCredentials(keys)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := buildEgressSecret(ha, creds, existing)
	if err != nil {
		t.Fatal(err)
	}
	var cfg ironProxyConfig
	if err := yaml.Unmarshal(secret.Data[agentsv1alpha1.EgressProxyConfigKey], &cfg); err != nil {
		t.Fatal(err)
	}
	return secret, cfg
}

func TestBuildEgressSecret(t *testing.T) {
	t.Run("aliases get the canonical credential's token under their own key", func(t *testing.T) {
		secret, _ := renderEgressConfig(t, egressHA(nil), []string{"GOOGLE_API_KEY"}, nil)
		gemini, google := string(secret.Data["GEMINI_API_KEY"]), string(secret.Data["GOOGLE_API_KEY"])
		if gemini == "" || gemini != google {
			t.Errorf("expected one token under both names, got %q and %q", gemini, google)
		}
		raw := string(secret.Data[agentsv1alpha1.EgressProxyConfigKey])
		if !strings.Contains(raw, "var: GOOGLE_API_KEY") {
			t.Errorf("expected the real key read from the credentials secret key it is stored under:\n%s", raw)
		}
	})

	t.Run("tokens survive re-renders; dropped credentials lose theirs", func(t *testing.T) {
		ha := egressHA(nil)
		first, _ := renderEgressConfig(t, ha, []string{"OPENAI_API_KEY", "GROQ_API_KEY"}, nil)
		second, _ := renderEgressConfig(t, ha, []string{"OPENAI_API_KEY", "XAI_API_KEY"}, first)

		if string(second.Data["OPENAI_API_KEY"]) != string(first.Data["OPENAI_API_KEY"]) {
			t.Error("expected the OPENAI_API_KEY token to be preserved")
		}
		if _, ok := second.Data["GROQ_API_KEY"]; ok {
			t.Error("expected the GROQ_API_KEY token to be dropped")
		}
		if len(second.Data["XAI_API_KEY"]) == 0 {
			t.Error("expected a token minted for XAI_API_KEY")
		}

		again, _ := renderEgressConfig(t, ha, []string{"OPENAI_API_KEY", "XAI_API_KEY"}, second)
		if string(again.Data[agentsv1alpha1.EgressProxyConfigKey]) != string(second.Data[agentsv1alpha1.EgressProxyConfigKey]) {
			t.Error("expected proxy.yaml to be stable across reconciles")
		}
	})
}

func TestBuildEgressProxyYAML(t *testing.T) {
	t.Run("proxy.yaml binds loopback and keeps every iron-proxy default port off", func(t *testing.T) {
		_, cfg := renderEgressConfig(t, egressHA(nil), nil, nil)
		if cfg.DNS.Enabled {
			t.Error("expected dns interception off")
		}
		for name, got := range map[string]string{
			"tunnel_listen": cfg.Proxy.TunnelListen,
			"http_listen":   cfg.Proxy.HTTPListen,
			"https_listen":  cfg.Proxy.HTTPSListen,
			"metrics":       cfg.Metrics.Listen,
		} {
			if !strings.HasPrefix(got, "127.0.0.1:") {
				t.Errorf("%s must bind loopback, got %q", name, got)
			}
		}
		if cfg.Proxy.TunnelListen != "127.0.0.1:9090" || cfg.Proxy.HTTPListen != "127.0.0.1:9091" {
			t.Errorf("unexpected listeners %q / %q", cfg.Proxy.TunnelListen, cfg.Proxy.HTTPListen)
		}
		if !slices.Equal(cfg.Proxy.UpstreamDenyCIDRs, defaultEgressUpstreamDenyCIDRs) {
			t.Errorf("expected the default deny list, got %v", cfg.Proxy.UpstreamDenyCIDRs)
		}
		if cfg.TLS.CACert != "/etc/iron-proxy/ca.crt" || cfg.TLS.CAKey != "/etc/iron-proxy/ca.key" {
			t.Errorf("unexpected ca paths %q / %q", cfg.TLS.CACert, cfg.TLS.CAKey)
		}
		if len(cfg.Transforms) != 1 || cfg.Transforms[0].Name != "allowlist" {
			t.Errorf("expected only the allowlist transform without credentials, got %+v", cfg.Transforms)
		}
	})

	t.Run("config fields flow into proxy.yaml", func(t *testing.T) {
		port := int32(18080)
		ha := egressHA(&agentsv1alpha1.EgressConfig{
			TunnelPort:        &port,
			UpstreamDenyCIDRs: []string{"169.254.0.0/16"},
			ExtraAllowedHosts: []string{"api.github.com", "openrouter.ai"},
			ExtraSecrets:      []agentsv1alpha1.EgressSecret{{Name: "API_TOKEN", Hosts: []string{"api.example.com"}}},
		})
		secret, cfg := renderEgressConfig(t, ha, []string{"API_TOKEN", "ANTHROPIC_API_KEY"}, nil)

		if cfg.Proxy.TunnelListen != "127.0.0.1:18080" || cfg.Proxy.HTTPListen != "127.0.0.1:18081" {
			t.Errorf("unexpected listeners %q / %q", cfg.Proxy.TunnelListen, cfg.Proxy.HTTPListen)
		}
		if !slices.Equal(cfg.Proxy.UpstreamDenyCIDRs, []string{"169.254.0.0/16"}) {
			t.Errorf("unexpected deny list %v", cfg.Proxy.UpstreamDenyCIDRs)
		}

		raw := string(secret.Data[agentsv1alpha1.EgressProxyConfigKey])
		for _, host := range []string{"api.anthropic.com", "api.github.com", "api.example.com"} {
			if !strings.Contains(raw, "- "+host+"\n") {
				t.Errorf("expected %s in the allowlist:\n%s", host, raw)
			}
		}
		if strings.Count(raw, "- openrouter.ai\n") != 1 {
			t.Errorf("expected openrouter.ai allowlisted once:\n%s", raw)
		}
		for _, name := range []string{"ANTHROPIC_API_KEY", "API_TOKEN"} {
			token := string(secret.Data[name])
			if token == "" {
				t.Fatalf("expected a proxy token for %s", name)
			}
			if !strings.Contains(raw, "proxy_value: "+token) || !strings.Contains(raw, "var: "+name) {
				t.Errorf("expected a secrets rule swapping %s:\n%s", name, raw)
			}
		}
		if !strings.HasPrefix(string(secret.Data["ANTHROPIC_API_KEY"]), "anthropic-") {
			t.Errorf("expected a hermes-style token prefix, got %q", secret.Data["ANTHROPIC_API_KEY"])
		}
		if strings.Count(raw, "require: true") != 2 || strings.Count(raw, "match_query: true") != 2 {
			t.Errorf("expected every rule to fail closed and to search the query:\n%s", raw)
		}
	})

	t.Run("secrets rules never match the tunnel connect", func(t *testing.T) {
		_, cfg := renderEgressConfig(t, egressHA(nil), []string{"OPENROUTER_API_KEY"}, nil)
		raw, err := yaml.Marshal(cfg.Transforms[1].Config)
		if err != nil {
			t.Fatal(err)
		}
		var secrets ironProxySecrets
		if err := yaml.Unmarshal(raw, &secrets); err != nil {
			t.Fatal(err)
		}
		for _, rule := range secrets.Secrets[0].Rules {
			if len(rule.Methods) == 0 || slices.Contains(rule.Methods, "CONNECT") {
				t.Errorf("rule %q must list methods without CONNECT, got %v", rule.Host, rule.Methods)
			}
		}
	})

}
