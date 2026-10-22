package v1alpha1

import (
	"slices"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const testCredsSecret = "llm-keys"

// egressAgent returns an agent named "agent" with egress enabled, reading
// credentials from testCredsSecret, with the given extra secrets.
func egressAgent(extra ...EgressSecret) *HermesAgent {
	return &HermesAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "agent", Namespace: "ns"},
		Spec: HermesAgentSpec{Egress: &Egress{
			Enabled:     true,
			Credentials: &EgressCredentials{SecretRef: EgressSecretRef{Name: testCredsSecret}},
			Config:      &EgressConfig{ExtraSecrets: extra},
		}},
	}
}

// credentialSources renders each credential as "NAME<-SOURCE_KEY".
func credentialSources(creds []EgressCredential) []string {
	out := make([]string, 0, len(creds))
	for _, c := range creds {
		out = append(out, c.Name+"<-"+c.SourceKey)
	}
	return out
}

func TestEgress_ResolveCredentials(t *testing.T) {
	tests := []struct {
		name     string
		extra    []EgressSecret
		keys     []string
		expected []string
		errPart  string
	}{
		{
			name:     "bundled providers present in the secret, in hermes-agent order",
			keys:     []string{"ANTHROPIC_API_KEY", "UNRELATED", "OPENROUTER_API_KEY"},
			expected: []string{"OPENROUTER_API_KEY<-OPENROUTER_API_KEY", "ANTHROPIC_API_KEY<-ANTHROPIC_API_KEY"},
		},
		{
			name:     "an alias satisfies a provider and is its source key",
			keys:     []string{"GOOGLE_API_KEY"},
			expected: []string{"GEMINI_API_KEY<-GOOGLE_API_KEY"},
		},
		{
			name: "extra secrets replace a bundled provider in place and append the rest",
			extra: []EgressSecret{
				{Name: "API_TOKEN", Hosts: []string{"api.example.com"}},
				{Name: "OPENAI_API_KEY", Hosts: []string{"llm.internal.example.com"}},
			},
			keys:     []string{"API_TOKEN", "OPENAI_API_KEY", "OPENROUTER_API_KEY"},
			expected: []string{"OPENROUTER_API_KEY<-OPENROUTER_API_KEY", "OPENAI_API_KEY<-OPENAI_API_KEY", "API_TOKEN<-API_TOKEN"},
		},
		{
			name:    "an extra secret missing from the secret",
			extra:   []EgressSecret{{Name: "API_TOKEN", Hosts: []string{"api.example.com"}}},
			keys:    []string{"OPENROUTER_API_KEY"},
			errPart: `extraSecrets "API_TOKEN"`,
		},
		{
			name:    "an extra secret on a bundled provider's host",
			extra:   []EgressSecret{{Name: "OPENAI_ORG_TOKEN", Hosts: []string{"api.openai.com"}}},
			keys:    []string{"OPENAI_ORG_TOKEN", "OPENAI_API_KEY"},
			errPart: `both swap on host "api.openai.com"`,
		},
		{
			name:    "an extra secret under a bundled provider's wildcard",
			extra:   []EgressSecret{{Name: "OR_TOKEN", Hosts: []string{"api.openrouter.ai"}}},
			keys:    []string{"OR_TOKEN", "OPENROUTER_API_KEY"},
			errPart: `both swap on host "api.openrouter.ai"`,
		},
		{
			name:     "an extra secret on its own host",
			extra:    []EgressSecret{{Name: "API_TOKEN", Hosts: []string{"api.example.com"}}},
			keys:     []string{"API_TOKEN", "OPENAI_API_KEY"},
			expected: []string{"OPENAI_API_KEY<-OPENAI_API_KEY", "API_TOKEN<-API_TOKEN"},
		},
		{
			name:    "an alias claimed by another credential",
			extra:   []EgressSecret{{Name: "API_TOKEN", Hosts: []string{"api.example.com"}, Aliases: []string{"OPENAI_API_KEY"}}},
			keys:    []string{"API_TOKEN", "OPENAI_API_KEY"},
			errPart: `env var "OPENAI_API_KEY"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			creds, err := egressAgent(tt.extra...).GetEgress().ResolveCredentials(tt.keys)
			if tt.errPart != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errPart) {
					t.Fatalf("expected an error containing %q, got %v", tt.errPart, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := credentialSources(creds); !slices.Equal(got, tt.expected) {
				t.Errorf("got %v, expected %v", got, tt.expected)
			}
		})
	}

	t.Run("credentials do not alias the bundled provider table", func(t *testing.T) {
		creds, err := egressAgent().GetEgress().ResolveCredentials([]string{"OPENROUTER_API_KEY"})
		if err != nil {
			t.Fatal(err)
		}
		creds[0].Hosts[0] = "mutated.example.com"
		again, _ := egressAgent().GetEgress().ResolveCredentials([]string{"OPENROUTER_API_KEY"})
		if again[0].Hosts[0] != "openrouter.ai" {
			t.Errorf("mutating a credential changed the provider table: %v", again[0].Hosts)
		}
	})

	t.Run("no credentials secret means no credentials", func(t *testing.T) {
		creds, err := (&Egress{Enabled: true}).ResolveCredentials(nil)
		if err != nil || len(creds) != 0 {
			t.Errorf("got %v, %v", creds, err)
		}
	})
}

func TestHostsOverlap(t *testing.T) {
	tests := []struct {
		a, b     string
		expected bool
	}{
		{"api.example.com", "api.example.com", true},
		{"API.example.com", "api.example.com", true},
		{"*.example.com", "api.example.com", true},
		{"api.example.com", "*.example.com", true},
		{"*.example.com", "*.api.example.com", true},
		{"*.example.com", "example.com", false},
		{"api.example.com", "www.example.com", false},
		{"*.example.com", "*.example.org", false},
	}
	for _, tt := range tests {
		t.Run(tt.a+" vs "+tt.b, func(t *testing.T) {
			if got := hostsOverlap(tt.a, tt.b); got != tt.expected {
				t.Errorf("hostsOverlap(%q, %q) = %v, expected %v", tt.a, tt.b, got, tt.expected)
			}
		})
	}
}

func TestEgressSecret_GetMatchHeaders(t *testing.T) {
	if got := (&EgressSecret{}).GetMatchHeaders(); !slices.Equal(got, []string{"Authorization"}) {
		t.Errorf("expected the Authorization default, got %v", got)
	}
	if got := (&EgressSecret{MatchHeaders: []string{"x-api-key"}}).GetMatchHeaders(); !slices.Equal(got, []string{"x-api-key"}) {
		t.Errorf("expected the configured headers, got %v", got)
	}
}

func TestHermesAgent_GetEgressSourceSecretNames(t *testing.T) {
	ha := egressAgent()
	ha.Spec.Egress.CA = &EgressCA{SecretRef: &EgressSecretRef{Name: "cm-ca"}}
	if got := ha.GetEgressSourceSecretNames(); !slices.Equal(got, []string{testCredsSecret, "cm-ca"}) {
		t.Errorf("got %v", got)
	}
	ha.Spec.Egress.Enabled = false
	if got := ha.GetEgressSourceSecretNames(); got != nil {
		t.Errorf("expected nothing watched while disabled, got %v", got)
	}
}

func TestHermesAgent_ValidateEgressSecretRefs(t *testing.T) {
	tests := []struct {
		name    string
		creds   string
		ca      string
		wantErr bool
	}{
		{name: "distinct names", creds: testCredsSecret, ca: "cm-ca"},
		{name: "credentials named like the egress secret", creds: "agent-egress", wantErr: true},
		{name: "credentials named like the managed ca", creds: "agent-egress-ca", wantErr: true},
		{name: "ca named like the egress secret", creds: testCredsSecret, ca: "agent-egress", wantErr: true},
		// The CA reconciler deliberately supports a user CA under the managed name.
		{name: "ca named like the managed ca", creds: testCredsSecret, ca: "agent-egress-ca"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ha := egressAgent()
			ha.Spec.Egress.Credentials.SecretRef.Name = tt.creds
			if tt.ca != "" {
				ha.Spec.Egress.CA = &EgressCA{SecretRef: &EgressSecretRef{Name: tt.ca}}
			}
			if err := ha.ValidateEgressSecretRefs(); (err != nil) != tt.wantErr {
				t.Errorf("got error %v, expected error: %v", err, tt.wantErr)
			}
		})
	}
}

func TestHermesAgent_ValidateEgressPorts(t *testing.T) {
	port := func(p int32) *int32 { return &p }
	tests := []struct {
		name       string
		tunnelPort *int32
		mutate     func(*HermesAgent)
		wantErr    bool
	}{
		{name: "defaults with every sidecar on", mutate: func(h *HermesAgent) {
			h.Spec.SearXNG = &SearXNG{Enabled: true}
			h.Spec.Camofox = &Camofox{Enabled: true}
		}},
		{name: "tunnel on the searxng port", tunnelPort: port(8080), wantErr: true, mutate: func(h *HermesAgent) {
			h.Spec.SearXNG = &SearXNG{Enabled: true}
		}},
		{name: "plain http listener on the searxng port", tunnelPort: port(8079), wantErr: true, mutate: func(h *HermesAgent) {
			h.Spec.SearXNG = &SearXNG{Enabled: true}
		}},
		{name: "searxng port while searxng is off", tunnelPort: port(8080)},
		{name: "tunnel on the camofox port", tunnelPort: port(9377), wantErr: true, mutate: func(h *HermesAgent) {
			h.Spec.Camofox = &Camofox{Enabled: true}
		}},
		{name: "api server on the default tunnel port", wantErr: true, mutate: func(h *HermesAgent) {
			h.Spec.Hermes = &Hermes{Config: &HermesConfig{APIServer: &HermesAPIServer{Enabled: true, Port: port(9090)}}}
		}},
		{name: "webhook on the default plain http port", wantErr: true, mutate: func(h *HermesAgent) {
			h.Spec.Hermes = &Hermes{Config: &HermesConfig{Webhook: &HermesWebhook{Enabled: true, Port: port(9091)}}}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ha := egressAgent()
			ha.Spec.Egress.Config.TunnelPort = tt.tunnelPort
			if tt.mutate != nil {
				tt.mutate(ha)
			}
			if err := ha.ValidateEgressPorts(); (err != nil) != tt.wantErr {
				t.Errorf("got error %v, expected error: %v", err, tt.wantErr)
			}
		})
	}

	t.Run("disabled egress claims no port", func(t *testing.T) {
		ha := egressAgent()
		ha.Spec.Egress.Enabled = false
		ha.Spec.SearXNG = &SearXNG{Enabled: true}
		ha.Spec.Egress.Config.TunnelPort = port(8080)
		if err := ha.ValidateEgressPorts(); err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})
}
