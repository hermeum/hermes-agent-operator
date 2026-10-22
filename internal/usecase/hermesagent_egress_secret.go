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
	"slices"
	"strings"
	"time"

	agentsv1alpha1 "hermeum/hermes-agent-operator/api/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/yaml"
)

// defaultEgressUpstreamDenyCIDRs mirrors hermes-agent's
// _DEFAULT_UPSTREAM_DENY_CIDRS: iron-proxy refuses to dial these whatever the
// allowlist says, so DNS rebinding through an allowed host cannot reach cloud
// metadata or the cluster network.
var defaultEgressUpstreamDenyCIDRs = []string{
	"127.0.0.0/8", "::1/128",
	"169.254.0.0/16", "fe80::/10",
	"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "fc00::/7",
	"::ffff:0:0/96",
	"100.64.0.0/10",
	"198.18.0.0/15",
}

// reconcileEgressSecret manages the Secret holding iron-proxy's proxy.yaml and
// the agent's proxy tokens, one key per credential env var. Tokens are minted
// once per credential and preserved across reconciles, so the agent and the
// proxy never disagree on them.
func (u *HermesAgentUseCase) reconcileEgressSecret(ctx context.Context, ha *agentsv1alpha1.HermesAgent) (result ctrl.Result, err error) {
	defer func() {
		if err != nil {
			err = u.markReconcileFailed(ctx, ha, condReasonEgressSecretFailed, err)
		}
	}()

	secretNsName := types.NamespacedName{Name: ha.GetEgressSecretName(), Namespace: ha.Namespace}

	existing, err := u.kube.GetSecret(ctx, GetSecretParam{NamespacedName: secretNsName})
	if err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}

	if !ha.GetEgress().IsEnabled() {
		if existing != nil {
			if err := u.kube.DeleteSecret(ctx, DeleteSecretParam{NamespacedName: secretNsName}); err != nil {
				return ctrl.Result{RequeueAfter: 30 * time.Second}, err
			}
			u.tel.Debug(ctx, "Egress Secret deleted")
		}
		return u.recordEgressSecret(ctx, ha, "")
	}

	if err := ha.ValidateEgressPorts(); err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}
	creds, err := u.resolveEgressCredentials(ctx, ha)
	if err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}

	desired, err := buildEgressSecret(ha, creds, existing)
	if err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}

	switch {
	case existing == nil:
		if err := u.kube.CreateSecretOwnedByHermesAgent(ctx, CreateSecretOfHermesAgentParam{HermesAgent: ha, Secret: desired}); err != nil {
			return ctrl.Result{RequeueAfter: 30 * time.Second}, err
		}
		u.tel.Debug(ctx, "Egress Secret created")
	case !maps.EqualFunc(existing.Data, desired.Data, bytes.Equal):
		existing.Data = desired.Data
		if err := u.kube.UpdateSecretOwnedByHermesAgent(ctx, UpdateSecretOfHermesAgentParam{HermesAgent: ha, Secret: existing}); err != nil {
			return ctrl.Result{RequeueAfter: 30 * time.Second}, err
		}
		u.tel.Debug(ctx, "Egress Secret updated")
	}
	return u.recordEgressSecret(ctx, ha, ha.GetEgressSecretName())
}

// recordEgressSecret records the managed egress Secret in the status, writing
// it only on change: this loop runs for every agent, egress or not.
func (u *HermesAgentUseCase) recordEgressSecret(ctx context.Context, ha *agentsv1alpha1.HermesAgent, name string) (ctrl.Result, error) {
	if ha.Status.ManagedResources.EgressSecret == name {
		return ctrl.Result{}, nil
	}
	ha.Status.ManagedResources.EgressSecret = name
	if err := u.kube.UpdateHermesAgentStatus(ctx, UpdateHermesAgentStatusParam{HermesAgent: ha}); err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}
	return ctrl.Result{}, nil
}

// buildEgressSecret renders proxy.yaml and the proxy tokens, reusing the
// tokens already in existing.
func buildEgressSecret(ha *agentsv1alpha1.HermesAgent, creds []agentsv1alpha1.EgressCredential, existing *corev1.Secret) (*corev1.Secret, error) {
	tokens := make(map[string]string, len(creds))
	for _, c := range creds {
		if existing != nil && len(existing.Data[c.Name]) > 0 {
			tokens[c.Name] = string(existing.Data[c.Name])
			continue
		}
		token, err := mintEgressProxyToken(c.Name)
		if err != nil {
			return nil, err
		}
		tokens[c.Name] = token
	}

	proxyYAML, err := buildEgressProxyYAML(ha, creds, tokens)
	if err != nil {
		return nil, err
	}

	// Every env var name of a credential, aliases included, gets its own key,
	// so that the agent's env and its .env can look a token up by name.
	data := map[string][]byte{agentsv1alpha1.EgressProxyConfigKey: []byte(proxyYAML)}
	for _, c := range creds {
		for _, name := range c.EnvNames() {
			data[name] = []byte(tokens[c.Name])
		}
	}
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ha.GetEgressSecretName(),
			Namespace: ha.Namespace,
			Labels:    ha.ResourceLabels(),
		},
		Data: data,
	}, nil
}

// mintEgressProxyToken mirrors hermes-agent's mint_proxy_token: a recognizable
// prefix derived from the env var name plus 128 random bits, e.g.
// "openrouter-<32 hex>" for OPENROUTER_API_KEY.
func mintEgressProxyToken(envName string) (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("minting egress proxy token: %w", err)
	}
	prefix := strings.ReplaceAll(strings.ToLower(envName), "_api_key", "")
	return fmt.Sprintf("%s-%x", prefix, raw), nil
}

// ironProxyConfig models the subset of iron-proxy's proxy.yaml (v0.49 schema)
// the operator renders. sigs.k8s.io/yaml emits keys sorted, so the output is
// stable across reconciles.
type ironProxyConfig struct {
	DNS        ironProxyDNS         `json:"dns"`
	Proxy      ironProxyProxy       `json:"proxy"`
	Metrics    ironProxyListen      `json:"metrics"`
	TLS        ironProxyTLS         `json:"tls"`
	Transforms []ironProxyTransform `json:"transforms"`
	Log        ironProxyLog         `json:"log"`
}

type ironProxyDNS struct {
	Enabled bool `json:"enabled"`
}

type ironProxyProxy struct {
	TunnelListen                  string   `json:"tunnel_listen"`
	HTTPListen                    string   `json:"http_listen"`
	HTTPSListen                   string   `json:"https_listen"`
	MaxRequestBodyBytes           int64    `json:"max_request_body_bytes"`
	MaxResponseBodyBytes          int64    `json:"max_response_body_bytes"`
	UpstreamResponseHeaderTimeout string   `json:"upstream_response_header_timeout"`
	UpstreamDenyCIDRs             []string `json:"upstream_deny_cidrs"`
}

type ironProxyListen struct {
	Listen string `json:"listen"`
}

type ironProxyTLS struct {
	Mode                string `json:"mode"`
	CACert              string `json:"ca_cert"`
	CAKey               string `json:"ca_key"`
	CertCacheSize       int    `json:"cert_cache_size"`
	LeafCertExpiryHours int    `json:"leaf_cert_expiry_hours"`
}

type ironProxyTransform struct {
	Name   string `json:"name"`
	Config any    `json:"config"`
}

type ironProxyAllowlist struct {
	Domains []string `json:"domains"`
}

type ironProxySecrets struct {
	Secrets []ironProxySecret `json:"secrets"`
}

type ironProxySecret struct {
	Source  ironProxySecretSource  `json:"source"`
	Replace ironProxySecretReplace `json:"replace"`
	Rules   []ironProxyRule        `json:"rules"`
}

type ironProxySecretSource struct {
	Type string `json:"type"`
	Var  string `json:"var"`
}

type ironProxySecretReplace struct {
	ProxyValue   string   `json:"proxy_value"`
	MatchHeaders []string `json:"match_headers"`
	MatchQuery   bool     `json:"match_query"`
	MatchBody    bool     `json:"match_body"`
	Require      bool     `json:"require"`
}

type ironProxyRule struct {
	Host    string   `json:"host"`
	Methods []string `json:"methods"`
}

// egressRuleMethods are the methods a secrets rule matches: every request
// method but CONNECT. iron-proxy runs its transforms on the tunnel CONNECT
// too, which carries no token, so with require on, a rule that matched it
// would reject every tunnel to the credential's hosts. Inside the tunnel,
// require still rejects a request that carries no token, or a real key.
var egressRuleMethods = []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"}

type ironProxyLog struct {
	Level string `json:"level"`
}

// buildEgressProxyYAML renders proxy.yaml after hermes-agent's
// build_proxy_config, adapted to a sidecar: every listener binds loopback,
// which the agent shares with the sidecar and nothing outside the pod can
// reach; DNS interception is off since the agent goes through HTTPS_PROXY.
// Each credential's real value comes from the sidecar's env var named like
// the credentials Secret key that holds it, which envFrom provides.
func buildEgressProxyYAML(ha *agentsv1alpha1.HermesAgent, creds []agentsv1alpha1.EgressCredential, tokens map[string]string) (string, error) {
	eg := ha.GetEgress()
	port := eg.GetTunnelPort()

	domains := eg.GetAllowedHosts()
	secrets := make([]ironProxySecret, 0, len(creds))
	for _, c := range creds {
		rules := make([]ironProxyRule, 0, len(c.Hosts))
		for _, h := range c.Hosts {
			rules = append(rules, ironProxyRule{Host: h, Methods: slices.Clone(egressRuleMethods)})
			if !slices.Contains(domains, h) {
				domains = append(domains, h)
			}
		}
		secrets = append(secrets, ironProxySecret{
			Source: ironProxySecretSource{Type: "env", Var: c.SourceKey},
			Replace: ironProxySecretReplace{
				ProxyValue:   tokens[c.Name],
				MatchHeaders: c.GetMatchHeaders(),
				MatchQuery:   true,
				Require:      true,
			},
			Rules: rules,
		})
	}

	transforms := []ironProxyTransform{{Name: "allowlist", Config: ironProxyAllowlist{Domains: domains}}}
	if len(secrets) > 0 {
		transforms = append(transforms, ironProxyTransform{Name: "secrets", Config: ironProxySecrets{Secrets: secrets}})
	}

	denyCIDRs := eg.GetUpstreamDenyCIDRs()
	if len(denyCIDRs) == 0 {
		denyCIDRs = defaultEgressUpstreamDenyCIDRs
	}

	cfg := ironProxyConfig{
		DNS: ironProxyDNS{Enabled: false},
		Proxy: ironProxyProxy{
			TunnelListen:                  fmt.Sprintf("127.0.0.1:%d", port),
			HTTPListen:                    fmt.Sprintf("127.0.0.1:%d", port+1),
			HTTPSListen:                   "127.0.0.1:0",
			MaxRequestBodyBytes:           16 << 20,
			UpstreamResponseHeaderTimeout: "120s",
			UpstreamDenyCIDRs:             denyCIDRs,
		},
		// iron-proxy defaults metrics to :9090, the default tunnel port.
		Metrics: ironProxyListen{Listen: "127.0.0.1:0"},
		TLS: ironProxyTLS{
			Mode:                "mitm",
			CACert:              agentsv1alpha1.EgressConfigDir + "/" + agentsv1alpha1.EgressCAManagedCertKey,
			CAKey:               agentsv1alpha1.EgressConfigDir + "/" + agentsv1alpha1.EgressCAManagedKeyKey,
			CertCacheSize:       1000,
			LeafCertExpiryHours: 168,
		},
		Transforms: transforms,
		Log:        ironProxyLog{Level: "info"},
	}

	out, err := yaml.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("marshaling iron-proxy config: %w", err)
	}
	return string(out), nil
}

// resolveEgressCredentials resolves the credentials iron-proxy swaps against
// the keys of the egress credentials Secret, which must exist when it is
// referenced.
func (u *HermesAgentUseCase) resolveEgressCredentials(ctx context.Context, ha *agentsv1alpha1.HermesAgent) ([]agentsv1alpha1.EgressCredential, error) {
	if err := ha.ValidateEgressSecretRefs(); err != nil {
		return nil, err
	}
	eg := ha.GetEgress()
	var keys []string
	if name := eg.GetCredentialsSecretName(); name != "" {
		secret, err := u.kube.GetSecret(ctx, GetSecretParam{NamespacedName: types.NamespacedName{Name: name, Namespace: ha.Namespace}})
		if err != nil {
			return nil, fmt.Errorf("reading egress credentials Secret %q: %w", name, err)
		}
		if secret == nil {
			return nil, fmt.Errorf("egress credentials Secret %q not found", name)
		}
		keys = slices.Collect(maps.Keys(secret.Data))
	}
	return eg.ResolveCredentials(keys)
}
