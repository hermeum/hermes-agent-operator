package usecase

import (
	"context"
	"errors"
	"slices"
	"testing"

	agentsv1alpha1 "hermeum/hermes-agent-operator/api/v1alpha1"

	corev1 "k8s.io/api/core/v1"
)

func findEnv(envs []corev1.EnvVar, name string) *corev1.EnvVar {
	for i := range envs {
		if envs[i].Name == name {
			return &envs[i]
		}
	}
	return nil
}

func TestEgressPodInputs(t *testing.T) {
	ha := egressHA(nil)
	base := func() map[string]*corev1.Secret {
		return map[string]*corev1.Secret{
			egressCredsSecret: credsSecret("OPENROUTER_API_KEY"),
			ha.GetEgressSecretName(): {Data: map[string][]byte{
				"OPENROUTER_API_KEY":                []byte("token"),
				"GOOGLE_API_KEY":                    []byte("token2"),
				agentsv1alpha1.EgressProxyConfigKey: []byte("dns: {}"),
			}},
			ha.GetEgressCASecretName(): {Data: map[string][]byte{"ca.crt": []byte("cert"), "ca.key": []byte("key")}},
		}
	}
	inputs := func(t *testing.T, secrets map[string]*corev1.Secret) ([]string, string) {
		t.Helper()
		names, hash, err := NewHermesAgentUseCase(newFakeEgressKube(secrets), silentTelemetry{}).egressPodInputs(context.Background(), ha)
		if err != nil {
			t.Fatal(err)
		}
		return names, hash
	}

	names, baseline := inputs(t, base())
	if !slices.Equal(names, []string{"GOOGLE_API_KEY", "OPENROUTER_API_KEY"}) {
		t.Errorf("expected the token names without proxy.yaml, got %v", names)
	}

	tests := []struct {
		name   string
		mutate func(map[string]*corev1.Secret)
	}{
		{
			name:   "rotating a real key",
			mutate: func(s map[string]*corev1.Secret) { s[egressCredsSecret].Data["OPENROUTER_API_KEY"] = []byte("rotated") },
		},
		{
			name:   "rotating the ca certificate",
			mutate: func(s map[string]*corev1.Secret) { s[ha.GetEgressCASecretName()].Data["ca.crt"] = []byte("new") },
		},
		{
			name: "changing the egress secret",
			mutate: func(s map[string]*corev1.Secret) {
				s[ha.GetEgressSecretName()].Data["OPENROUTER_API_KEY"] = []byte("new")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			secrets := base()
			tt.mutate(secrets)
			if _, hash := inputs(t, secrets); hash == baseline {
				t.Error("expected the pod to restart")
			}
		})
	}

	t.Run("waits for a secret the cache does not hold yet", func(t *testing.T) {
		for _, missing := range []string{egressCredsSecret, ha.GetEgressSecretName(), ha.GetEgressCASecretName()} {
			secrets := base()
			delete(secrets, missing)
			_, _, err := NewHermesAgentUseCase(newFakeEgressKube(secrets), silentTelemetry{}).egressPodInputs(context.Background(), ha)
			if !errors.Is(err, errEgressInputsPending) {
				t.Errorf("without %s: expected errEgressInputsPending, got %v", missing, err)
			}
		}
	})

	t.Run("the ca key alone does not restart the pod", func(t *testing.T) {
		secrets := base()
		secrets[ha.GetEgressCASecretName()].Data["ca.key"] = []byte("other")
		if _, hash := inputs(t, secrets); hash != baseline {
			t.Error("expected the same hash")
		}
	})
}

// TestBuildEgressContainerEnv checks the agent gets proxy tokens while the
// sidecar gets the real keys.
func TestBuildEgressContainerEnv(t *testing.T) {
	ha := egressHA(nil)
	ha.Spec.Egress.Env = []corev1.EnvVar{{Name: "IRON_LOG_LEVEL", Value: "debug"}}
	sts := buildStatefulSet(ha, statefulSetInputs{
		configHash:       "cfg",
		egressTokenNames: []string{"GEMINI_API_KEY", "GOOGLE_API_KEY", "OPENROUTER_API_KEY"},
		egressHash:       "h1",
	})

	agent := findHermesContainer(sts)
	for _, name := range []string{"OPENROUTER_API_KEY", "GEMINI_API_KEY", "GOOGLE_API_KEY"} {
		env := findEnv(agent.Env, name)
		if env == nil || env.ValueFrom == nil || env.ValueFrom.SecretKeyRef == nil {
			t.Fatalf("expected %s on the agent from a secretKeyRef, got %+v", name, env)
		}
		if ref := env.ValueFrom.SecretKeyRef; ref.Name != ha.GetEgressSecretName() || ref.Key != name {
			t.Errorf("%s must come from its own egress secret key, got %s/%s", name, ref.Name, ref.Key)
		}
	}
	if findEnv(agent.Env, "HTTPS_PROXY") != nil {
		t.Error("the proxy env vars belong in the .env, not the container env")
	}
	for _, from := range agent.EnvFrom {
		if from.SecretRef != nil && from.SecretRef.Name == egressCredsSecret {
			t.Error("the agent must never read the credentials secret")
		}
	}

	proxy := findContainer(sts, "iron-proxy")
	if proxy == nil {
		t.Fatal("expected the iron-proxy sidecar")
	}
	if len(proxy.EnvFrom) != 1 || proxy.EnvFrom[0].SecretRef.Name != egressCredsSecret {
		t.Errorf("expected the sidecar to read the credentials secret, got %+v", proxy.EnvFrom)
	}
	if got := findEnv(proxy.Env, "IRON_LOG_LEVEL"); got == nil || got.Value != "debug" {
		t.Errorf("expected the user's env on the sidecar, got %+v", proxy.Env)
	}
	if sts.Spec.Template.Annotations[domain+"/egress-hash"] != "h1" {
		t.Error("expected the egress hash on the pod template")
	}
}

func TestBuildEgressContainer(t *testing.T) {
	ha := egressHA(nil)
	in := statefulSetInputs{configHash: "cfg", egressTokenNames: []string{"OPENROUTER_API_KEY"}, egressHash: "h1"}

	t.Run("the agent mounts the ca certificate, never its key", func(t *testing.T) {
		sts := buildStatefulSet(ha, in)
		for _, v := range sts.Spec.Template.Spec.Volumes {
			if v.Name != "egress-agent-ca" {
				continue
			}
			for _, item := range v.Secret.Items {
				if item.Key != agentsv1alpha1.EgressCAManagedCertKey {
					t.Errorf("agent ca volume exposes %q", item.Key)
				}
			}
			return
		}
		t.Error("expected the agent ca volume")
	})

	t.Run("a user-supplied ca is projected under the fixed names", func(t *testing.T) {
		withCA := ha.DeepCopy()
		withCA.Spec.Egress.CA = &agentsv1alpha1.EgressCA{SecretRef: &agentsv1alpha1.EgressSecretRef{Name: "cm-ca"}}
		sts := buildStatefulSet(withCA, in)
		for _, v := range sts.Spec.Template.Spec.Volumes {
			if v.Name != "egress-config" {
				continue
			}
			ca := v.Projected.Sources[1].Secret
			want := []corev1.KeyToPath{{Key: "tls.crt", Path: "ca.crt"}, {Key: "tls.key", Path: "ca.key"}}
			if ca.Name != "cm-ca" || !slices.Equal(ca.Items, want) {
				t.Errorf("unexpected ca projection %+v", ca)
			}
			return
		}
		t.Error("expected the egress config volume")
	})

	t.Run("disabled egress leaves the statefulset untouched", func(t *testing.T) {
		sts := buildStatefulSet(minimalHA(), in)
		if findContainer(sts, "iron-proxy") != nil || findEnv(findHermesContainer(sts).Env, "OPENROUTER_API_KEY") != nil {
			t.Error("expected no egress wiring")
		}
	})
}
