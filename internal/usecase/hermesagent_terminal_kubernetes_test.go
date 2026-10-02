package usecase

import (
	"encoding/json"
	"strings"
	"testing"

	agentsv1alpha1 "hermeum/hermes-agent-operator/api/v1alpha1"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	sigsyaml "sigs.k8s.io/yaml"
)

func terminalHA(k *agentsv1alpha1.HermesTerminalKubernetes) *agentsv1alpha1.HermesAgent {
	ha := minimalHA()
	ha.Spec.Hermes = &agentsv1alpha1.Hermes{
		Terminal: &agentsv1alpha1.HermesTerminal{Kubernetes: k},
	}
	return ha
}

// terminalConfig renders the generated default-profile config.yaml back into a
// map, so assertions read the keys the agent will actually receive.
func terminalConfig(t *testing.T, ha *agentsv1alpha1.HermesAgent) map[string]any {
	t.Helper()
	cm, err := buildHermesConfigMap(ha)
	if err != nil {
		t.Fatalf("buildHermesConfigMap: %v", err)
	}
	raw, ok := cm.Data["profile.default.config.yaml"]
	if !ok {
		t.Fatalf("no default profile config in %v", cm.Data)
	}
	var cfg map[string]any
	if err := yamlToMap(raw, &cfg); err != nil {
		t.Fatalf("parsing generated config: %v\n%s", err, raw)
	}
	return cfg
}

func terminalBlock(t *testing.T, cfg map[string]any) map[string]any {
	t.Helper()
	terminal, ok := cfg["terminal"].(map[string]any)
	if !ok {
		t.Fatalf("no terminal block in config: %v", cfg)
	}
	return terminal
}

func TestApplyTerminalKubernetesConfig(t *testing.T) {
	t.Run("no terminal block when the backend is not requested", func(t *testing.T) {
		cm, err := buildHermesConfigMap(minimalHA())
		if err != nil {
			t.Fatalf("buildHermesConfigMap: %v", err)
		}
		if raw, ok := cm.Data["profile.default.config.yaml"]; ok && strings.Contains(raw, "terminal") {
			t.Errorf("unrequested backend leaked into config:\n%s", raw)
		}
	})

	t.Run("no terminal block when explicitly disabled", func(t *testing.T) {
		ha := terminalHA(&agentsv1alpha1.HermesTerminalKubernetes{Enabled: ptrBool(false)})
		cm, err := buildHermesConfigMap(ha)
		if err != nil {
			t.Fatalf("buildHermesConfigMap: %v", err)
		}
		if raw, ok := cm.Data["profile.default.config.yaml"]; ok && strings.Contains(raw, "terminal") {
			t.Errorf("disabled backend leaked into config:\n%s", raw)
		}
	})

	t.Run("defaults are written even without a raw config", func(t *testing.T) {
		ha := terminalHA(&agentsv1alpha1.HermesTerminalKubernetes{})
		terminal := terminalBlock(t, terminalConfig(t, ha))

		if got := terminal["backend"]; got != "kubernetes" {
			t.Errorf("backend = %v, want kubernetes", got)
		}
		kube, ok := terminal["kubernetes"].(map[string]any)
		if !ok {
			t.Fatalf("no terminal.kubernetes block: %v", terminal)
		}
		if got := kube["exec_container_name"]; got != defaultExecContainerNameForTest {
			t.Errorf("exec_container_name = %v, want %s", got, defaultExecContainerNameForTest)
		}
		// Always emitted, so the NetworkPolicy selector and the pod labels
		// cannot drift apart.
		selector, ok := kube["owned_selector"].(map[string]any)
		if !ok {
			t.Fatalf("owned_selector missing: %v", kube)
		}
		if got := selector["app.kubernetes.io/managed-by"]; got != wantOwnedSelectorValue {
			t.Errorf("owned_selector managed-by = %v, want hermes-agent", got)
		}
		// Unset optionals stay out, so the agent applies its own defaults.
		for _, key := range []string{"namespace", "spec", "metadata", "ready_timeout_seconds", "trusted_sandbox"} {
			if _, ok := kube[key]; ok {
				t.Errorf("%s written despite being unset: %v", key, kube[key])
			}
		}
	})

	t.Run("explicit settings are written through", func(t *testing.T) {
		ha := terminalHA(&agentsv1alpha1.HermesTerminalKubernetes{
			Namespace:           "hermes-sessions",
			ExecContainerName:   "devbox",
			ReadyTimeoutSeconds: ptrInt32(300),
			OwnerReference:      "off",
			TrustedSandbox:      ptrBool(false),
			OwnedSelector:       map[string]string{"acme.example/owner": "hermes"},
			PodSpec: &apiextensionsv1.JSON{
				Raw: []byte(`{"containers":[{"name":"devbox","image":"ubuntu:26.04"}]}`),
			},
			PodMetadata: &apiextensionsv1.JSON{
				Raw: []byte(`{"annotations":{"sidecar.istio.io/inject":"false"}}`),
			},
		})
		kube := terminalBlock(t, terminalConfig(t, ha))["kubernetes"].(map[string]any)

		if got := kube["namespace"]; got != "hermes-sessions" {
			t.Errorf("namespace = %v", got)
		}
		if got := kube["exec_container_name"]; got != "devbox" {
			t.Errorf("exec_container_name = %v", got)
		}
		if got := kube["owner_reference"]; got != "off" {
			t.Errorf("owner_reference = %v", got)
		}
		if got := kube["trusted_sandbox"]; got != false {
			t.Errorf("trusted_sandbox = %v", got)
		}
		// YAML round-trips numbers as float64.
		if got := kube["ready_timeout_seconds"]; got != float64(300) {
			t.Errorf("ready_timeout_seconds = %v (%T)", got, got)
		}
		spec, ok := kube["spec"].(map[string]any)
		if !ok {
			t.Fatalf("spec missing: %v", kube)
		}
		if _, ok := spec["containers"]; !ok {
			t.Errorf("podSpec not passed through verbatim: %v", spec)
		}
		meta, ok := kube["metadata"].(map[string]any)
		if !ok {
			t.Fatalf("metadata missing: %v", kube)
		}
		if _, ok := meta["annotations"]; !ok {
			t.Errorf("podMetadata not passed through verbatim: %v", meta)
		}
	})

	t.Run("raw config wins over every generated key", func(t *testing.T) {
		ha := terminalHA(&agentsv1alpha1.HermesTerminalKubernetes{ExecContainerName: "devbox"})
		ha.Spec.Hermes.Config = &agentsv1alpha1.HermesConfig{
			Raw: &apiextensionsv1.JSON{Raw: []byte(
				`{"terminal":{"backend":"local","kubernetes":{"exec_container_name":"hand-written"}}}`,
			)},
		}
		terminal := terminalBlock(t, terminalConfig(t, ha))

		if got := terminal["backend"]; got != "local" {
			t.Errorf("backend = %v, want the raw value 'local'", got)
		}
		kube := terminal["kubernetes"].(map[string]any)
		if got := kube["exec_container_name"]; got != "hand-written" {
			t.Errorf("exec_container_name = %v, want the raw value", got)
		}
	})

	t.Run("existing raw keys survive alongside the generated ones", func(t *testing.T) {
		ha := terminalHA(&agentsv1alpha1.HermesTerminalKubernetes{})
		ha.Spec.Hermes.Config = &agentsv1alpha1.HermesConfig{
			Raw: &apiextensionsv1.JSON{Raw: []byte(`{"model":{"provider":"anthropic"}}`)},
		}
		cfg := terminalConfig(t, ha)

		if _, ok := cfg["model"]; !ok {
			t.Errorf("user config dropped: %v", cfg)
		}
		if got := terminalBlock(t, cfg)["backend"]; got != "kubernetes" {
			t.Errorf("backend = %v, want kubernetes", got)
		}
	})
}

func TestSessionPodRules(t *testing.T) {
	t.Run("none when the backend is off", func(t *testing.T) {
		if got := sessionPodRules(minimalHA()); got != nil {
			t.Errorf("expected no rules, got %v", got)
		}
	})

	t.Run("none when rbac management is declined", func(t *testing.T) {
		ha := terminalHA(&agentsv1alpha1.HermesTerminalKubernetes{RBAC: ptrBool(false)})
		if got := sessionPodRules(ha); got != nil {
			t.Errorf("expected no rules, got %v", got)
		}
	})

	t.Run("pods and pods/exec when enabled", func(t *testing.T) {
		rules := sessionPodRules(terminalHA(&agentsv1alpha1.HermesTerminalKubernetes{}))
		assertRule(t, rules, "pods", []string{"create", "get", "delete"})
		// Both verbs: exec is a websocket-upgrading GET the API server refuses
		// without `create`, so granting only `get` fails at command time.
		assertRule(t, rules, "pods/exec", []string{"get", "create"})
	})

	t.Run("user rules are kept alongside", func(t *testing.T) {
		ha := terminalHA(&agentsv1alpha1.HermesTerminalKubernetes{})
		ha.Spec.Security = &agentsv1alpha1.HermesSecurity{
			RBAC: &agentsv1alpha1.RBAC{AdditionalRules: []agentsv1alpha1.RBACRule{{
				APIGroups: []string{""}, Resources: []string{"configmaps"}, Verbs: []string{"get"},
			}}},
		}
		rules := append(sessionPodRules(ha), ha.GetSecurity().GetRBAC().GetAdditionalRules()...)
		assertRule(t, rules, "pods/exec", []string{"get", "create"})
		assertRule(t, rules, "configmaps", []string{"get"})
	})
}

func assertRule(t *testing.T, rules []agentsv1alpha1.RBACRule, resource string, wantVerbs []string) {
	t.Helper()
	for _, r := range rules {
		if len(r.Resources) == 0 || r.Resources[0] != resource {
			continue
		}
		if strings.Join(r.Verbs, ",") != strings.Join(wantVerbs, ",") {
			t.Errorf("%s verbs = %v, want %v", resource, r.Verbs, wantVerbs)
		}
		return
	}
	t.Errorf("no rule for %s in %v", resource, rules)
}

func TestBuildTerminalKubernetesEnv(t *testing.T) {
	t.Run("absent when the backend is off", func(t *testing.T) {
		sts := buildStatefulSet(minimalHA())
		if envVar(t, sts, "HERMES_POD_NAME") != nil {
			t.Error("downward API env injected without the backend")
		}
	})

	t.Run("pod identity from the downward API when enabled", func(t *testing.T) {
		sts := buildStatefulSet(terminalHA(&agentsv1alpha1.HermesTerminalKubernetes{}))

		for name, wantPath := range map[string]string{
			"HERMES_POD_NAME": "metadata.name",
			"HERMES_POD_UID":  "metadata.uid",
		} {
			e := envVar(t, sts, name)
			if e == nil {
				t.Errorf("%s not injected", name)
				continue
			}
			if e.ValueFrom == nil || e.ValueFrom.FieldRef == nil {
				t.Errorf("%s is not a fieldRef: %v", name, e)
				continue
			}
			if got := e.ValueFrom.FieldRef.FieldPath; got != wantPath {
				t.Errorf("%s fieldPath = %s, want %s", name, got, wantPath)
			}
		}
	})
}

func TestBuildSessionPodNetworkPolicy(t *testing.T) {
	t.Run("selects the pods the agent labels, not the agent", func(t *testing.T) {
		k := &agentsv1alpha1.HermesTerminalKubernetes{}
		np := buildSessionPodNetworkPolicy(terminalHA(k), k, nil)

		if got := np.Spec.PodSelector.MatchLabels["app.kubernetes.io/managed-by"]; got != wantOwnedSelectorValue {
			t.Errorf("podSelector managed-by = %q, want hermes-agent", got)
		}
		if np.Name != "test"+sessionPodNetworkPolicySuffix {
			t.Errorf("name = %s", np.Name)
		}
		if len(np.Spec.Ingress) != 0 {
			t.Errorf("expected no ingress, got %v", np.Spec.Ingress)
		}
	})

	t.Run("selector override moves the policy with the pods", func(t *testing.T) {
		k := &agentsv1alpha1.HermesTerminalKubernetes{
			OwnedSelector: map[string]string{"acme.example/owner": "hermes"},
		}
		np := buildSessionPodNetworkPolicy(terminalHA(k), k, nil)
		if got := np.Spec.PodSelector.MatchLabels["acme.example/owner"]; got != "hermes" {
			t.Errorf("podSelector = %v, want the overridden label", np.Spec.PodSelector.MatchLabels)
		}
	})

	t.Run("dns egress covers the OpenShift 5353 rewrite", func(t *testing.T) {
		k := &agentsv1alpha1.HermesTerminalKubernetes{}
		np := buildSessionPodNetworkPolicy(terminalHA(k), k, nil)

		ports := np.Spec.Egress[0].Ports
		for _, want := range []int32{53, 5353} {
			for _, proto := range []corev1.Protocol{corev1.ProtocolUDP, corev1.ProtocolTCP} {
				assertEgressPort(t, ports, proto, want)
			}
		}
	})

	t.Run("internet egress excludes the cluster ranges by default", func(t *testing.T) {
		k := &agentsv1alpha1.HermesTerminalKubernetes{}
		np := buildSessionPodNetworkPolicy(terminalHA(k), k, nil)

		block := findIPBlock(np.Spec.Egress)
		if block == nil {
			t.Fatal("no internet egress rule")
		}
		if block.CIDR != "0.0.0.0/0" {
			t.Errorf("cidr = %s", block.CIDR)
		}
		if strings.Join(block.Except, ",") != "10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,169.254.0.0/16" {
			t.Errorf("except = %v", block.Except)
		}
	})

	t.Run("internet egress can be dropped", func(t *testing.T) {
		k := &agentsv1alpha1.HermesTerminalKubernetes{}
		np := buildSessionPodNetworkPolicy(terminalHA(k), k, &agentsv1alpha1.SessionPodNetworkPolicy{
			AllowInternet: ptrBool(false),
		})
		if block := findIPBlock(np.Spec.Egress); block != nil {
			t.Errorf("internet egress present despite allowInternet=false: %v", block)
		}
	})

	t.Run("egress is DNS plus internet and nothing else", func(t *testing.T) {
		// There is no additionalEgress knob here on purpose: NetworkPolicies are
		// additive, so a second policy selecting the same pods adds its rules,
		// and a second copy of NetworkPolicyEgressRule's schema would cost ~28 KB
		// on a CRD that is already near the 256 KiB apply ceiling.
		k := &agentsv1alpha1.HermesTerminalKubernetes{}
		np := buildSessionPodNetworkPolicy(terminalHA(k), k, nil)
		if len(np.Spec.Egress) != 2 {
			t.Errorf("expected exactly DNS + internet egress, got %d rules: %v", len(np.Spec.Egress), np.Spec.Egress)
		}
	})
}

func findIPBlock(rules []networkingv1.NetworkPolicyEgressRule) *networkingv1.IPBlock {
	for _, r := range rules {
		for _, to := range r.To {
			if to.IPBlock != nil && to.IPBlock.CIDR == "0.0.0.0/0" {
				return to.IPBlock
			}
		}
	}
	return nil
}

func assertEgressPort(t *testing.T, ports []networkingv1.NetworkPolicyPort, want corev1.Protocol, port int32) {
	t.Helper()
	for _, p := range ports {
		if p.Port == nil || p.Port.IntVal != port {
			continue
		}
		if p.Protocol != nil && *p.Protocol == want {
			return
		}
	}
	t.Errorf("no %s port %d in %v", want, port, ports)
}

func ptrInt32(i int32) *int32 { return &i }

const (
	defaultExecContainerNameForTest = "workspace"
	// The agent's default owned_selector value. It happens to equal
	// hermesContainerName, but it labels session pods, not the container.
	wantOwnedSelectorValue = "hermes-agent"
)

// yamlToMap parses the generated config back through JSON, so the assertions
// see the same shape the agent parses.
func yamlToMap(in string, out *map[string]any) error {
	jsonBytes, err := sigsyaml.YAMLToJSON([]byte(in))
	if err != nil {
		return err
	}
	return json.Unmarshal(jsonBytes, out)
}

// envVar returns the named variable from the hermes-agent container, or nil.
func envVar(t *testing.T, sts *appsv1.StatefulSet, name string) *corev1.EnvVar {
	t.Helper()
	c := findHermesContainer(sts)
	if c == nil {
		t.Fatal("no hermes-agent container")
	}
	for i := range c.Env {
		if c.Env[i].Name == name {
			return &c.Env[i]
		}
	}
	return nil
}

// A HermesAgent with no terminal block at all must not make the session-pod
// reconciler touch a nil spec — the reconcile loop runs it on every instance.
func TestSessionPodNetworkPolicyNilSpec(t *testing.T) {
	ha := minimalHA()
	k := ha.GetHermes().GetTerminal().GetKubernetes()

	if k.IsEnabled() {
		t.Fatal("a missing terminal block must not enable the backend")
	}
	if got := k.GetNamespace(); got != "" {
		t.Errorf("GetNamespace on a nil spec = %q, want empty", got)
	}
	if got := k.GetExecContainerName(); got != defaultExecContainerNameForTest {
		t.Errorf("GetExecContainerName on a nil spec = %q", got)
	}
	if got := k.GetOwnedSelector()["app.kubernetes.io/managed-by"]; got != wantOwnedSelectorValue {
		t.Errorf("GetOwnedSelector on a nil spec = %v", k.GetOwnedSelector())
	}
	if k.GetNetworkPolicy() != nil {
		t.Error("GetNetworkPolicy on a nil spec must be nil")
	}
}
