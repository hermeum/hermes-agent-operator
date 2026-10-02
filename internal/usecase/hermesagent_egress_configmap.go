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
	"fmt"
	"time"

	agentsv1alpha1 "hermeum/hermes-agent-operator/api/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/yaml"
)

const (
	// egressProxyConfigKey is the ConfigMap key holding iron-proxy's proxy.yaml.
	egressProxyConfigKey = "proxy.yaml"
	// egressCAMountDir is where the CA secret is mounted in the iron-proxy sidecar.
	egressCAMountDir = "/etc/iron-proxy"
)

// egressCredEnvVar returns the deterministic sidecar env var name for the
// injection rule at the given index. The name is used both as source.var in
// proxy.yaml and as the sidecar container env var name, so it must be stable.
func egressCredEnvVar(index int) string {
	return fmt.Sprintf("EGRESS_CRED_%d", index)
}

// reconcileEgressConfigMap ensures the iron-proxy proxy.yaml ConfigMap exists
// when egress is enabled and is removed when it is disabled.
func (u *HermesAgentUseCase) reconcileEgressConfigMap(ctx context.Context, ha *agentsv1alpha1.HermesAgent) (result ctrl.Result, err error) {
	defer func() {
		if err != nil {
			err = u.markReconcileFailed(ctx, ha, condReasonEgressConfigMapFailed, err)
		}
	}()

	cmNsName := types.NamespacedName{Name: ha.GetEgressConfigName(), Namespace: ha.Namespace}

	existing, err := u.kube.GetConfigMap(ctx, GetConfigMapParam{NamespacedName: cmNsName})
	if err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}

	if !ha.GetEgress().IsEnabled() {
		if existing != nil {
			if err := u.kube.DeleteConfigMap(ctx, DeleteConfigMapParam{NamespacedName: cmNsName}); err != nil {
				return ctrl.Result{RequeueAfter: 30 * time.Second}, err
			}
			u.tel.Debug(ctx, "Egress ConfigMap deleted")
		}
		ha.Status.ManagedResources.EgressConfigMap = ""
		if err := u.kube.UpdateHermesAgentStatus(ctx, UpdateHermesAgentStatusParam{HermesAgent: ha}); err != nil {
			return ctrl.Result{RequeueAfter: 30 * time.Second}, err
		}
		return ctrl.Result{}, nil
	}

	desired, err := buildEgressConfigMap(ha)
	if err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}

	if existing != nil {
		if configMapDataEqual(desired, existing) {
			return ctrl.Result{}, nil
		}
		desired.ResourceVersion = existing.ResourceVersion
		if err := u.kube.UpdateConfigMapOwnedByHermesAgent(ctx, UpdateConfigMapParam{HermesAgent: ha, ConfigMap: desired}); err != nil {
			return ctrl.Result{RequeueAfter: 30 * time.Second}, err
		}
		u.tel.Debug(ctx, "Egress ConfigMap updated")
		return ctrl.Result{}, nil
	}

	if err := u.kube.CreateConfigMapOwnedByHermesAgent(ctx, CreateConfigMapOfHermesAgentParam{HermesAgent: ha, ConfigMap: desired}); err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}
	u.tel.Debug(ctx, "Egress ConfigMap created")
	ha.Status.ManagedResources.EgressConfigMap = ha.GetEgressConfigName()
	if err := u.kube.UpdateHermesAgentStatus(ctx, UpdateHermesAgentStatusParam{HermesAgent: ha}); err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}
	return ctrl.Result{}, nil
}

// ironProxyConfig models the subset of iron-proxy's proxy.yaml the operator
// renders. Marshaled with sigs.k8s.io/yaml, which serializes struct fields in
// declaration order, so the output is deterministic across reconciles.
type ironProxyConfig struct {
	DNS        ironProxyDNS         `json:"dns"`
	Proxy      ironProxyProxy       `json:"proxy"`
	TLS        ironProxyTLS         `json:"tls"`
	Transforms []ironProxyTransform `json:"transforms"`
	Log        ironProxyLog         `json:"log"`
}

type ironProxyDNS struct {
	// Enabled is always false: the agent reaches the proxy via an explicit
	// HTTPS_PROXY tunnel listener, not DNS interception.
	Enabled bool `json:"enabled"`
}

type ironProxyProxy struct {
	// TunnelListen is the explicit-proxy CONNECT/SOCKS5 listener the agent's
	// HTTPS_PROXY/HTTP_PROXY point at. upstream_deny_cidrs is intentionally
	// omitted so iron-proxy applies its secure default (blocks IMDS + loopback).
	TunnelListen string `json:"tunnel_listen"`
}

type ironProxyTLS struct {
	Mode   string `json:"mode"`
	CACert string `json:"ca_cert"`
	CAKey  string `json:"ca_key"`
}

type ironProxyTransform struct {
	Name   string         `json:"name"`
	Config map[string]any `json:"config"`
}

type ironProxyLog struct {
	Level string `json:"level"`
}

type ironProxySecret struct {
	Source ironProxySecretSource `json:"source"`
	Inject ironProxySecretInject `json:"inject"`
	Rules  []ironProxyRule       `json:"rules"`
}

type ironProxySecretSource struct {
	Type string `json:"type"`
	Var  string `json:"var"`
}

type ironProxySecretInject struct {
	Header    string `json:"header"`
	Formatter string `json:"formatter"`
}

type ironProxyRule struct {
	Host string `json:"host"`
}

// buildEgressProxyYAML renders iron-proxy's proxy.yaml from the egress spec.
// The allowlist is the union of AllowedHosts and every injected host, so an
// injected credential's destination is always reachable. Ordering is stable:
// AllowedHosts first (spec order), then injected hosts (rule order, skipping
// duplicates), and one secrets entry per injection rule in spec order.
func buildEgressProxyYAML(ha *agentsv1alpha1.HermesAgent) (string, error) {
	e := ha.GetEgress()

	domains := make([]string, 0, len(e.GetAllowedHosts())+len(e.GetInject()))
	seen := map[string]struct{}{}
	appendHost := func(h string) {
		if h == "" {
			return
		}
		if _, ok := seen[h]; ok {
			return
		}
		seen[h] = struct{}{}
		domains = append(domains, h)
	}
	for _, h := range e.GetAllowedHosts() {
		appendHost(h)
	}
	for _, in := range e.GetInject() {
		appendHost(in.Host)
	}

	secrets := make([]ironProxySecret, 0, len(e.GetInject()))
	for i, in := range e.GetInject() {
		secrets = append(secrets, ironProxySecret{
			Source: ironProxySecretSource{Type: "env", Var: egressCredEnvVar(i)},
			Inject: ironProxySecretInject{Header: in.GetHeader(), Formatter: in.GetFormatter()},
			Rules:  []ironProxyRule{{Host: in.Host}},
		})
	}

	transforms := []ironProxyTransform{
		{Name: "allowlist", Config: map[string]any{"domains": domains}},
	}
	if len(secrets) > 0 {
		transforms = append(transforms, ironProxyTransform{
			Name:   "secrets",
			Config: map[string]any{"secrets": secrets},
		})
	}

	cfg := ironProxyConfig{
		DNS:   ironProxyDNS{Enabled: false},
		Proxy: ironProxyProxy{TunnelListen: fmt.Sprintf(":%d", agentsv1alpha1.DefaultEgressTunnelPort)},
		TLS: ironProxyTLS{
			Mode:   "mitm",
			CACert: egressCAMountDir + "/" + egressCACertKey,
			CAKey:  egressCAMountDir + "/" + egressCAKeyKey,
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

func buildEgressConfigMap(ha *agentsv1alpha1.HermesAgent) (*corev1.ConfigMap, error) {
	proxyYAML, err := buildEgressProxyYAML(ha)
	if err != nil {
		return nil, err
	}
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ha.GetEgressConfigName(),
			Namespace: ha.Namespace,
			Labels:    resourceLabels(ha),
		},
		Data: map[string]string{
			egressProxyConfigKey: proxyYAML,
		},
	}, nil
}
