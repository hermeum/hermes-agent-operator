package usecase

import (
	"context"
	"strings"
	"testing"

	agentsv1alpha1 "hermeum/hermes-agent-operator/api/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// `configMapKube` serves `ConfigMap` objects by name; anything else is reported
// missing, which is also how the operator's own bootstrap `ConfigMap` looks
// before it is created.
type configMapKube struct {
	fakeSnapshotKube
	configMaps map[string]map[string]string
}

func (f *configMapKube) GetConfigMap(ctx context.Context, param GetConfigMapParam) (*corev1.ConfigMap, error) {
	data, ok := f.configMaps[param.NamespacedName.Name]
	if !ok {
		return nil, nil
	}
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: param.NamespacedName.Name, Namespace: param.NamespacedName.Namespace},
		Data:       data,
	}, nil
}

// haWithConfigRef returns an agent whose default profile reads its config from
// the named `ConfigMap` key.
func haWithConfigRef(name, key string) *agentsv1alpha1.HermesAgent {
	ha := minimalHA()
	ha.Spec.Hermes = &agentsv1alpha1.Hermes{
		Config: &agentsv1alpha1.HermesConfig{
			ConfigMapRef: &agentsv1alpha1.HermesConfigMapKeyRef{Name: name, Key: key},
		},
	}
	return ha
}

func TestResolveConfigDocuments(t *testing.T) {
	ctx := context.Background()
	kube := &configMapKube{configMaps: map[string]map[string]string{
		"agent-config": {
			"config.yaml":   "model: claude-sonnet-4-5\n",
			"other.yaml":    "model: gpt-5\n",
			"blank.yaml":    "   \n",
			"comments.yaml": "# nothing to configure yet\n",
			"broken.yaml":   "model: [unterminated\n",
		},
	}}
	uc := NewHermesAgentUseCase(kube, silentTelemetry{})

	t.Run("default profile document is converted to JSON", func(t *testing.T) {
		refs, err := uc.resolveConfigDocuments(ctx, haWithConfigRef("agent-config", ""))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := string(refs.Default); got != `{"model":"claude-sonnet-4-5"}` {
			t.Errorf("unexpected document %q", got)
		}
	})

	t.Run("an explicit key is honoured", func(t *testing.T) {
		refs, err := uc.resolveConfigDocuments(ctx, haWithConfigRef("agent-config", "other.yaml"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := string(refs.Default); got != `{"model":"gpt-5"}` {
			t.Errorf("unexpected document %q", got)
		}
	})

	t.Run("an empty document is an empty config, not null", func(t *testing.T) {
		refs, err := uc.resolveConfigDocuments(ctx, haWithConfigRef("agent-config", "blank.yaml"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := string(refs.Default); got != "{}" {
			t.Errorf("unexpected document %q", got)
		}
	})

	t.Run("a comment-only document is an empty config, not null", func(t *testing.T) {
		// YAMLToJSON turns a comment-only document into "null", which
		// unmarshals a map to nil and would panic the defaulting below.
		refs, err := uc.resolveConfigDocuments(ctx, haWithConfigRef("agent-config", "comments.yaml"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := string(refs.Default); got != "{}" {
			t.Errorf("unexpected document %q", got)
		}
	})

	t.Run("named profile documents are keyed by profile name", func(t *testing.T) {
		ha := minimalHA()
		ha.Spec.Hermes = &agentsv1alpha1.Hermes{
			Profiles: map[string]agentsv1alpha1.HermesProfile{
				"coder": {Config: &agentsv1alpha1.HermesProfileConfig{
					ConfigMapRef: &agentsv1alpha1.HermesConfigMapKeyRef{Name: "agent-config"},
				}},
				"writer": {},
			},
		}
		refs, err := uc.resolveConfigDocuments(ctx, ha)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if refs.Default != nil {
			t.Errorf("expected no default document, got %q", refs.Default)
		}
		if got := string(refs.Profiles["coder"]); got != `{"model":"claude-sonnet-4-5"}` {
			t.Errorf("unexpected coder document %q", got)
		}
		if _, ok := refs.Profiles["writer"]; ok {
			t.Error("a profile without a configMapRef must not appear")
		}
	})

	errorCases := []struct {
		name string
		ha   *agentsv1alpha1.HermesAgent
		want string
	}{
		{
			name: "missing ConfigMap",
			ha:   haWithConfigRef("absent", ""),
			want: `ConfigMap "absent" not found`,
		},
		{
			name: "missing key",
			ha:   haWithConfigRef("agent-config", "absent.yaml"),
			want: `has no key "absent.yaml"`,
		},
		{
			name: "unparseable document",
			ha:   haWithConfigRef("agent-config", "broken.yaml"),
			want: `parsing key "broken.yaml"`,
		},
	}
	for _, tc := range errorCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := uc.resolveConfigDocuments(ctx, tc.ha)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("expected error containing %q, got %v", tc.want, err)
			}
		})
	}

	t.Run("raw and configMapRef together fail for the default profile", func(t *testing.T) {
		ha := haWithConfigRef("agent-config", "")
		ha.Spec.Hermes.Config.Raw = imageJSON(`{"model":"inline"}`)
		_, err := uc.resolveConfigDocuments(ctx, ha)
		if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
			t.Errorf("expected a mutual exclusion error, got %v", err)
		}
	})

	t.Run("raw and configMapRef together fail for a named profile", func(t *testing.T) {
		ha := minimalHA()
		ha.Spec.Hermes = &agentsv1alpha1.Hermes{
			Profiles: map[string]agentsv1alpha1.HermesProfile{
				"coder": {Config: &agentsv1alpha1.HermesProfileConfig{
					Raw:          imageJSON(`{"model":"inline"}`),
					ConfigMapRef: &agentsv1alpha1.HermesConfigMapKeyRef{Name: "agent-config"},
				}},
			},
		}
		_, err := uc.resolveConfigDocuments(ctx, ha)
		if err == nil || !strings.Contains(err.Error(), `profile "coder"`) {
			t.Errorf("expected a mutual exclusion error naming the profile, got %v", err)
		}
	})
}

func TestBuildHermesConfigMapWithReferencedDocuments(t *testing.T) {
	t.Run("referenced document is written as the default profile config", func(t *testing.T) {
		ha := haWithConfigRef("agent-config", "")
		data, err := buildHermesConfigMapData(ha, resolvedConfigDocuments{Default: []byte(`{"model":"opus"}`)})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := data["profile.default.config.yaml"]; !strings.Contains(got, "model: opus") {
			t.Errorf("unexpected config.yaml:\n%s", got)
		}
	})

	t.Run("operator defaults still apply to a referenced document", func(t *testing.T) {
		ha := haWithConfigRef("agent-config", "")
		ha.Spec.SearXNG = &agentsv1alpha1.SearXNG{Enabled: true}
		data, err := buildHermesConfigMapData(ha, resolvedConfigDocuments{Default: []byte(`{"model":"opus"}`)})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := data["profile.default.config.yaml"]; !strings.Contains(got, "search_backend: searxng") {
			t.Errorf("expected the SearXNG default to be applied, got:\n%s", got)
		}
	})

	t.Run("an empty config still takes the operator's defaults", func(t *testing.T) {
		// The regression guard: a nil map here panics the reconciler rather
		// than failing it, so the panic is in a stack trace and not in status.
		ha := haWithConfigRef("agent-config", "comments.yaml")
		ha.Spec.SearXNG = &agentsv1alpha1.SearXNG{Enabled: true}
		ha.Spec.Camofox = &agentsv1alpha1.Camofox{
			Enabled:     true,
			Persistence: agentsv1alpha1.CamofoxPersistenceSpec{Enabled: true},
		}
		ha.Spec.Hermes.Profiles = map[string]agentsv1alpha1.HermesProfile{"coder": {}}

		data, err := buildHermesConfigMapData(ha, resolvedConfigDocuments{Default: []byte("{}")})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got := data["profile.default.config.yaml"]
		for _, want := range []string{"search_backend: searxng", "multiplex_profiles: true", "managed_persistence: true"} {
			if !strings.Contains(got, want) {
				t.Errorf("expected %q in:\n%s", want, got)
			}
		}
	})

	t.Run("referenced profile document is written for that profile", func(t *testing.T) {
		ha := minimalHA()
		ha.Spec.Hermes = &agentsv1alpha1.Hermes{
			Profiles: map[string]agentsv1alpha1.HermesProfile{
				"coder": {Config: &agentsv1alpha1.HermesProfileConfig{
					ConfigMapRef: &agentsv1alpha1.HermesConfigMapKeyRef{Name: "agent-config"},
				}},
			},
		}
		data, err := buildHermesConfigMapData(ha, resolvedConfigDocuments{
			Profiles: map[string][]byte{"coder": []byte(`{"model":"haiku"}`)},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := data["profile.coder.config.yaml"]; !strings.Contains(got, "model: haiku") {
			t.Errorf("unexpected profile config.yaml:\n%s", got)
		}
	})

	t.Run("inline raw is unaffected", func(t *testing.T) {
		ha := minimalHA()
		ha.Spec.Hermes = &agentsv1alpha1.Hermes{
			Config: &agentsv1alpha1.HermesConfig{Raw: imageJSON(`{"model":"inline"}`)},
		}
		data, err := buildHermesConfigMapData(ha, resolvedConfigDocuments{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := data["profile.default.config.yaml"]; !strings.Contains(got, "model: inline") {
			t.Errorf("unexpected config.yaml:\n%s", got)
		}
	})

	t.Run("the assembled ConfigMap carries the ref identity", func(t *testing.T) {
		ha := haWithConfigRef("agent-config", "")
		ref := ha.GetHermesConfigMapRef()
		if ref.Name != ha.GetHermesName() {
			t.Errorf("expected ref name %q, got %q", ha.GetHermesName(), ref.Name)
		}
		if ref.Namespace != ha.Namespace {
			t.Errorf("expected ref namespace %q, got %q", ha.Namespace, ref.Namespace)
		}
		if ref.Labels[agentsv1alpha1.LabelManagedBy] != agentsv1alpha1.ManagedByValue {
			t.Errorf("expected ref labels to carry %q", agentsv1alpha1.LabelManagedBy)
		}
	})
}

func TestBuildStatefulSetWithReferencedConfig(t *testing.T) {
	ha := haWithConfigRef("agent-config", "")

	// configHashFor hashes the ConfigMap data the referenced documents yield —
	// the value reconcileStatefulSet computes from the live ConfigMap.
	configHashFor := func(refs resolvedConfigDocuments) string {
		data, err := buildHermesConfigMapData(ha, refs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		return configMapDataHash(data)
	}

	t.Run("config step runs for a referenced config", func(t *testing.T) {
		sts := buildStatefulSet(ha, configHashFor(resolvedConfigDocuments{Default: []byte(`{"model":"opus"}`)}))
		script := findInitContainer(sts, consolidatedInitContainerName).Args[0]
		if !strings.Contains(script, "/bootstrap/profile.default.config.yaml") {
			t.Errorf("expected the config step, got:\n%s", script)
		}
	})

	t.Run("a change to the referenced document rolls the pod", func(t *testing.T) {
		first := desiredSpecHash(buildStatefulSet(ha, configHashFor(resolvedConfigDocuments{Default: []byte(`{"model":"opus"}`)})))
		second := desiredSpecHash(buildStatefulSet(ha, configHashFor(resolvedConfigDocuments{Default: []byte(`{"model":"haiku"}`)})))
		if first == second {
			t.Error("expected the config hash to follow the referenced document")
		}
	})
}
