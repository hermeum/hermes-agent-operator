package usecase

import (
	"context"
	"time"

	agentsv1alpha1 "hermeum/hermes-agent-operator/api/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
)

// sessionPodNetworkPolicySuffix names the policy after the instance it belongs
// to, next to the instance policy rather than replacing it.
const sessionPodNetworkPolicySuffix = "-session-pods"

// reconcileSessionPodNetworkPolicy isolates the session pods the Hermes
// `kubernetes` terminal backend creates.
//
// Those pods are created by the agent, not by this operator, so they carry the
// agent's own labels and the instance NetworkPolicy — which selects on
// selectorLabels — never matches them. Without this policy a session reaches
// the API server and every ClusterIP Service in the cluster.
func (u *HermesAgentUseCase) reconcileSessionPodNetworkPolicy(ctx context.Context, ha *agentsv1alpha1.HermesAgent) (ctrl.Result, error) {
	nsName := types.NamespacedName{Namespace: ha.Namespace, Name: ha.Name + sessionPodNetworkPolicySuffix}

	existing, err := u.kube.GetNetworkPolicy(ctx, GetNetworkPolicyParam{NamespacedName: nsName})
	if err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}

	k8sTerminal := ha.GetHermes().GetTerminal().GetKubernetes()
	np := k8sTerminal.GetNetworkPolicy()

	// A session-pod namespace other than the agent's is out of scope: the policy
	// would have to be created there, and the operator is not granted that
	// namespace. Leaving it uncreated is honest; a policy in the wrong namespace
	// would look like isolation without being any.
	inAgentNamespace := k8sTerminal.GetNamespace() == "" || k8sTerminal.GetNamespace() == ha.Namespace

	if !k8sTerminal.IsEnabled() || !np.IsEnabled() || !inAgentNamespace {
		if existing != nil {
			err := u.kube.DeleteNetworkPolicy(ctx, DeleteNetworkPolicyParam{NamespacedName: nsName})
			if err != nil {
				return ctrl.Result{RequeueAfter: 30 * time.Second}, err
			}
			u.tel.Debug(ctx, "session pod NetworkPolicy deleted")
		}
		ha.Status.ManagedResources.SessionPodNetworkPolicy = ""
		if err := u.kube.UpdateHermesAgentStatus(ctx, UpdateHermesAgentStatusParam{HermesAgent: ha}); err != nil {
			return ctrl.Result{RequeueAfter: 30 * time.Second}, err
		}
		return ctrl.Result{}, nil
	}

	desired := buildSessionPodNetworkPolicy(ha, k8sTerminal, np)
	if existing != nil {
		if networkPolicyEqual(desired, existing) {
			return ctrl.Result{}, nil
		}
		desired.ResourceVersion = existing.ResourceVersion
		err := u.kube.UpdateNetworkPolicyOwnedByHermesAgent(ctx, UpdateNetworkPolicyParam{HermesAgent: ha, NetworkPolicy: desired})
		if err != nil {
			return ctrl.Result{RequeueAfter: 30 * time.Second}, err
		}
		u.tel.Debug(ctx, "session pod NetworkPolicy updated")
		return ctrl.Result{}, nil
	}

	err = u.kube.CreateNetworkPolicyOwnedByHermesAgent(ctx, CreateNetworkPolicyOfHermesAgentParam{HermesAgent: ha, NetworkPolicy: desired})
	if err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}
	u.tel.Debug(ctx, "session pod NetworkPolicy created")
	ha.Status.ManagedResources.SessionPodNetworkPolicy = nsName.Name
	if err := u.kube.UpdateHermesAgentStatus(ctx, UpdateHermesAgentStatusParam{HermesAgent: ha}); err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}
	return ctrl.Result{}, nil
}

func buildSessionPodNetworkPolicy(
	ha *agentsv1alpha1.HermesAgent,
	k8sTerminal *agentsv1alpha1.HermesTerminalKubernetes,
	np *agentsv1alpha1.SessionPodNetworkPolicy,
) *networkingv1.NetworkPolicy {
	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ha.Name + sessionPodNetworkPolicySuffix,
			Namespace: ha.Namespace,
			Labels:    resourceLabels(ha),
		},
		Spec: networkingv1.NetworkPolicySpec{
			// The same labels the agent stamps on the pods it creates. Both come
			// from GetOwnedSelector, so a rebranded selector moves them together
			// instead of leaving session pods unselected.
			PodSelector: metav1.LabelSelector{
				MatchLabels: k8sTerminal.GetOwnedSelector(),
			},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
			// No ingress rules: nothing should reach into a session pod.
			Ingress: nil,
			Egress:  buildSessionPodEgress(np),
		},
	}
}

func buildSessionPodEgress(np *agentsv1alpha1.SessionPodNetworkPolicy) []networkingv1.NetworkPolicyEgressRule {
	udp := corev1.ProtocolUDP
	tcp := corev1.ProtocolTCP

	rules := make([]networkingv1.NetworkPolicyEgressRule, 0, 2)

	dns := intstr.FromInt32(53)
	// 5353 is not optional on OpenShift: OVN-Kubernetes applies egress ACLs
	// after service DNAT, so a query to the DNS service on :53 is rewritten to
	// the CoreDNS pod on :5353 before the policy is evaluated. A rule naming
	// only 53 never matches and every lookup times out.
	dnsAlt := intstr.FromInt32(5353)
	rules = append(rules, networkingv1.NetworkPolicyEgressRule{
		To: []networkingv1.NetworkPolicyPeer{{
			NamespaceSelector: &metav1.LabelSelector{},
			PodSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"k8s-app": "kube-dns"},
			},
		}},
		Ports: []networkingv1.NetworkPolicyPort{
			{Protocol: &udp, Port: &dns},
			{Protocol: &tcp, Port: &dns},
			{Protocol: &udp, Port: &dnsAlt},
			{Protocol: &tcp, Port: &dnsAlt},
		},
	})

	if np.ShouldAllowInternet() {
		rules = append(rules, networkingv1.NetworkPolicyEgressRule{
			To: []networkingv1.NetworkPolicyPeer{{
				IPBlock: &networkingv1.IPBlock{
					CIDR:   "0.0.0.0/0",
					Except: np.GetExcludedEgressCIDRs(),
				},
			}},
		})
	}

	return rules
}
