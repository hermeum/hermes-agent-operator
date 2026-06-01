package usecase

import (
	"context"
	agentsv1alpha1 "noahingh/hermes-agent-operator/api/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const namespaceNameLabel = "kubernetes.io/metadata.name"

func (u *HermesAgentUseCase) reconcileNetworkPolicy(ctx context.Context, ha *agentsv1alpha1.HermesAgent) error {
	nsName := types.NamespacedName{Name: ha.Name, Namespace: ha.Namespace}

	existing, err := u.kube.GetNetworkPolicy(ctx, GetNetworkPolicyParam{NamespacedName: nsName})
	if err != nil {
		return err
	}

	np := ha.GetSecurity().GetNetworkPolicy()
	if !np.IsEnabled() {
		if existing == nil {
			return nil
		}
		err := u.kube.DeleteNetworkPolicy(ctx, DeleteNetworkPolicyParam{NamespacedName: nsName})
		u.tel.IncNetworkPolicyOperation(ctx, IncNetworkPolicyOperationParam{Operation: OperationDelete, Result: resultOf(err)})
		return err
	}

	desired := u.buildNetworkPolicy(ha, np)
	if existing != nil {
		desired.ResourceVersion = existing.ResourceVersion
		err := u.kube.UpdateNetworkPolicyOwnedByHermesAgent(ctx, UpdateNetworkPolicyParam{HermesAgent: ha, NetworkPolicy: desired})
		u.tel.IncNetworkPolicyOperation(ctx, IncNetworkPolicyOperationParam{Operation: OperationUpdate, Result: resultOf(err)})
		return err
	}

	err = u.kube.CreateNetworkPolicyOwnedByHermesAgent(ctx, CreateNetworkPolicyOfHermesAgentParam{HermesAgent: ha, NetworkPolicy: desired})
	u.tel.IncNetworkPolicyOperation(ctx, IncNetworkPolicyOperationParam{Operation: OperationCreate, Result: resultOf(err)})
	return err
}

func (u *HermesAgentUseCase) buildNetworkPolicy(ha *agentsv1alpha1.HermesAgent, np *agentsv1alpha1.NetworkPolicy) *networkingv1.NetworkPolicy {
	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ha.Name,
			Namespace: ha.Namespace,
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{"app": ha.Name},
			},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
			Ingress: buildNetworkPolicyIngress(np),
			Egress:  buildNetworkPolicyEgress(np),
		},
	}
}

func buildNetworkPolicyIngress(np *agentsv1alpha1.NetworkPolicy) []networkingv1.NetworkPolicyIngressRule {
	gateway := intstr.FromInt32(hermesGatewayPort)
	tcp := corev1.ProtocolTCP
	ports := []networkingv1.NetworkPolicyPort{{Protocol: &tcp, Port: &gateway}}

	peers := make([]networkingv1.NetworkPolicyPeer, 0, len(np.AllowedIngressCIDRs)+len(np.AllowedIngressNamespaces))
	for _, cidr := range np.AllowedIngressCIDRs {
		peers = append(peers, networkingv1.NetworkPolicyPeer{
			IPBlock: &networkingv1.IPBlock{CIDR: cidr},
		})
	}
	for _, ns := range np.AllowedIngressNamespaces {
		peers = append(peers, networkingv1.NetworkPolicyPeer{
			NamespaceSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{namespaceNameLabel: ns},
			},
		})
	}

	return []networkingv1.NetworkPolicyIngressRule{{From: peers, Ports: ports}}
}

func buildNetworkPolicyEgress(np *agentsv1alpha1.NetworkPolicy) []networkingv1.NetworkPolicyEgressRule {
	udp := corev1.ProtocolUDP
	tcp := corev1.ProtocolTCP

	rules := make([]networkingv1.NetworkPolicyEgressRule, 0, len(np.AdditionalEgress)+2)

	if np.ShouldAllowDNS() {
		dnsPort := intstr.FromInt32(53)
		rules = append(rules, networkingv1.NetworkPolicyEgressRule{
			Ports: []networkingv1.NetworkPolicyPort{
				{Protocol: &udp, Port: &dnsPort},
				{Protocol: &tcp, Port: &dnsPort},
			},
		})
	}

	httpsPort := intstr.FromInt32(443)
	httpsPeers := make([]networkingv1.NetworkPolicyPeer, 0, len(np.AllowedEgressCIDRs))
	for _, cidr := range np.AllowedEgressCIDRs {
		httpsPeers = append(httpsPeers, networkingv1.NetworkPolicyPeer{
			IPBlock: &networkingv1.IPBlock{CIDR: cidr},
		})
	}
	rules = append(rules, networkingv1.NetworkPolicyEgressRule{
		To:    httpsPeers,
		Ports: []networkingv1.NetworkPolicyPort{{Protocol: &tcp, Port: &httpsPort}},
	})

	rules = append(rules, np.AdditionalEgress...)
	return rules
}
