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

package v1alpha1

import (
	networkingv1 "k8s.io/api/networking/v1"
)

// HermesSecurity configures the security context for the pod and container.
type HermesSecurity struct {
	// rbac configures the ServiceAccount and Role used by the HermesAgent pod.
	// +optional
	RBAC *RBAC `json:"rbac,omitempty"`
	// networkPolicy configures network isolation for the HermesAgent pod.
	// +optional
	NetworkPolicy *NetworkPolicy `json:"networkPolicy,omitempty"`
}

// NetworkPolicy configures network isolation for the Hermes agent instance
type NetworkPolicy struct {
	// Enabled enables network policy creation
	// +kubebuilder:default=true
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// AllowedIngressCIDRs is a list of CIDRs allowed to access this instance
	// +optional
	AllowedIngressCIDRs []string `json:"allowedIngressCIDRs,omitempty"`

	// AllowedIngressNamespaces is a list of namespace names allowed to access this instance
	// +optional
	AllowedIngressNamespaces []string `json:"allowedIngressNamespaces,omitempty"`

	// AllowedEgressCIDRs is a list of CIDRs this instance can reach
	// Default allows all egress on port 443 for AI APIs
	// +optional
	AllowedEgressCIDRs []string `json:"allowedEgressCIDRs,omitempty"`

	// AllowDNS allows DNS resolution (port 53)
	// +kubebuilder:default=true
	// +optional
	AllowDNS *bool `json:"allowDNS,omitempty"`

	// AdditionalEgress appends custom egress rules to the default DNS + HTTPS rules.
	// Use this to allow traffic to cluster-internal services on non-standard ports.
	// +optional
	AdditionalEgress []networkingv1.NetworkPolicyEgressRule `json:"additionalEgress,omitempty"`
}

// RBAC configures RBAC for the HermesAgent instance.
type RBAC struct {
	// CreateServiceAccount creates a dedicated ServiceAccount for the instance.
	// +kubebuilder:default=true
	// +optional
	CreateServiceAccount *bool `json:"createServiceAccount,omitempty"`

	// ServiceAccountName is the name of an existing ServiceAccount to use.
	// Only used if CreateServiceAccount is false.
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

	// ServiceAccountAnnotations are annotations to add to the managed ServiceAccount.
	// Use this for cloud provider integrations like AWS IRSA or GCP Workload Identity.
	// +optional
	ServiceAccountAnnotations map[string]string `json:"serviceAccountAnnotations,omitempty"`

	// AdditionalRules adds custom RBAC rules to the generated Role.
	// +optional
	AdditionalRules []RBACRule `json:"additionalRules,omitempty"`
}

func (r *RBAC) ShouldCreateServiceAccount() bool {
	if r == nil {
		return false
	}
	if r.CreateServiceAccount == nil {
		return true
	}
	return *r.CreateServiceAccount
}

func (r *RBAC) GetAdditionalRules() []RBACRule {
	if r == nil {
		return nil
	}
	return r.AdditionalRules
}

// RBACRule represents a RBAC rule.
type RBACRule struct {
	// APIGroups is the name of the APIGroup that contains the resources.
	APIGroups []string `json:"apiGroups"`
	// Resources is a list of resources this rule applies to.
	Resources []string `json:"resources"`
	// Verbs is a list of verbs that apply to the resources.
	Verbs []string `json:"verbs"`
}

func (s *HermesSecurity) GetRBAC() *RBAC {
	if s == nil {
		return nil
	}
	return s.RBAC
}

func (s *HermesSecurity) GetNetworkPolicy() *NetworkPolicy {
	if s == nil {
		return nil
	}
	return s.NetworkPolicy
}

// IsEnabled reports whether a NetworkPolicy should be created. Omitting the
// block entirely means no policy; including it enables one by default.
func (n *NetworkPolicy) IsEnabled() bool {
	if n == nil {
		return false
	}
	if n.Enabled == nil {
		return true
	}
	return *n.Enabled
}

func (n *NetworkPolicy) ShouldAllowDNS() bool {
	if n == nil || n.AllowDNS == nil {
		return true
	}
	return *n.AllowDNS
}
