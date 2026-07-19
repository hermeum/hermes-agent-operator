package usecase

import (
	"testing"

	agentsv1alpha1 "hermeum/hermes-agent-operator/api/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func networkPolicyIngressPorts(t *testing.T, ha *agentsv1alpha1.HermesAgent) []networkingv1.NetworkPolicyPort {
	t.Helper()
	rules := buildNetworkPolicyIngress(ha, &agentsv1alpha1.NetworkPolicy{Enabled: ptrBool(true)})
	if len(rules) == 0 {
		return nil
	}
	return rules[0].Ports
}

func assertIngressPort(t *testing.T, ports []networkingv1.NetworkPolicyPort, want corev1.Protocol, port int32) {
	t.Helper()
	for _, p := range ports {
		if p.Port == nil || p.Port.Type != intstr.Int || p.Port.IntVal != port {
			continue
		}
		proto := corev1.ProtocolTCP
		if p.Protocol != nil {
			proto = *p.Protocol
		}
		if proto != want {
			t.Errorf("port %d has protocol %s, want %s", port, proto, want)
			return
		}
		return
	}
	t.Errorf("port %d not found in ingress ports %v", port, ports)
}

func TestBuildNetworkPolicyIngress_Ports(t *testing.T) {
	t.Run("nil when nothing enabled", func(t *testing.T) {
		ha := minimalHA()
		if got := networkPolicyIngressPorts(t, ha); got != nil {
			t.Errorf("expected nil ingress, got %v", got)
		}
	})

	t.Run("only api server when only api server enabled", func(t *testing.T) {
		ha := minimalHA()
		ha.Spec.Hermes = &agentsv1alpha1.Hermes{
			Config: &agentsv1alpha1.HermesConfig{
				APIServer: &agentsv1alpha1.HermesAPIServer{Enabled: true},
			},
		}
		ports := networkPolicyIngressPorts(t, ha)
		if len(ports) != 1 {
			t.Fatalf("expected 1 port, got %d", len(ports))
		}
		assertIngressPort(t, ports, corev1.ProtocolTCP, agentsv1alpha1.DefaultAPIServerPort)
	})

	t.Run("api server and webhook when both enabled", func(t *testing.T) {
		ha := minimalHA()
		ha.Spec.Hermes = &agentsv1alpha1.Hermes{
			Config: &agentsv1alpha1.HermesConfig{
				APIServer: &agentsv1alpha1.HermesAPIServer{Enabled: true},
				Webhook:   &agentsv1alpha1.HermesWebhook{Enabled: true},
			},
		}
		ports := networkPolicyIngressPorts(t, ha)
		if len(ports) != 2 {
			t.Fatalf("expected 2 ports, got %d", len(ports))
		}
		assertIngressPort(t, ports, corev1.ProtocolTCP, agentsv1alpha1.DefaultAPIServerPort)
		assertIngressPort(t, ports, corev1.ProtocolTCP, agentsv1alpha1.DefaultWebhookPort)
	})

	t.Run("includes additional hermes.ports with default TCP", func(t *testing.T) {
		ha := minimalHA()
		ha.Spec.Hermes = &agentsv1alpha1.Hermes{
			Config: &agentsv1alpha1.HermesConfig{
				APIServer: &agentsv1alpha1.HermesAPIServer{Enabled: true},
			},
			Ports: []corev1.ContainerPort{
				{Name: "metrics", ContainerPort: 9090},
			},
		}
		ports := networkPolicyIngressPorts(t, ha)
		if len(ports) != 2 {
			t.Fatalf("expected 2 ports, got %d (%v)", len(ports), ports)
		}
		assertIngressPort(t, ports, corev1.ProtocolTCP, agentsv1alpha1.DefaultAPIServerPort)
		assertIngressPort(t, ports, corev1.ProtocolTCP, 9090)
	})

	t.Run("respects explicit protocol on additional ports", func(t *testing.T) {
		ha := minimalHA()
		ha.Spec.Hermes = &agentsv1alpha1.Hermes{
			Config: &agentsv1alpha1.HermesConfig{
				APIServer: &agentsv1alpha1.HermesAPIServer{Enabled: true},
			},
			Ports: []corev1.ContainerPort{
				{Name: "syslog", ContainerPort: 5140, Protocol: corev1.ProtocolUDP},
			},
		}
		ports := networkPolicyIngressPorts(t, ha)
		if len(ports) != 2 {
			t.Fatalf("expected 2 ports, got %d (%v)", len(ports), ports)
		}
		assertIngressPort(t, ports, corev1.ProtocolTCP, agentsv1alpha1.DefaultAPIServerPort)
		assertIngressPort(t, ports, corev1.ProtocolUDP, 5140)
	})

	t.Run("additional ports alone are allowed", func(t *testing.T) {
		ha := minimalHA()
		ha.Spec.Hermes = &agentsv1alpha1.Hermes{
			Ports: []corev1.ContainerPort{
				{Name: "metrics", ContainerPort: 9090},
			},
		}
		ports := networkPolicyIngressPorts(t, ha)
		if len(ports) != 1 {
			t.Fatalf("expected 1 port, got %d (%v)", len(ports), ports)
		}
		assertIngressPort(t, ports, corev1.ProtocolTCP, 9090)
	})
}
