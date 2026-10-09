package usecase

import (
	"slices"
	"strings"
	"testing"

	agentsv1alpha1 "hermeum/hermes-agent-operator/api/v1alpha1"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	testTrue = "true"
	// testPriorityClass is an arbitrary existing PriorityClass name used to
	// exercise `spec.priorityClassName`.
	testPriorityClass = "system-cluster-critical"
	// consolidatedInitContainerName is the name of the operator-managed init
	// container that configures the default profile.
	consolidatedInitContainerName = "init-hermes"
	// testConfigHash is an arbitrary fixed config hash for buildStatefulSet
	// call sites that do not exercise the config-hash annotation itself.
	testConfigHash = "test-config-hash"
)

func minimalHA() *agentsv1alpha1.HermesAgent {
	return &agentsv1alpha1.HermesAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec:       agentsv1alpha1.HermesAgentSpec{},
	}
}

// imageJSON wraps a raw JSON image override (string or legacy object form).
func imageJSON(raw string) *apiextensionsv1.JSON {
	return &apiextensionsv1.JSON{Raw: []byte(raw)}
}

func TestGetImageReferenceForms(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "string with tag", raw: `"nousresearch/hermes-agent:v1.2.3"`, want: "nousresearch/hermes-agent:v1.2.3"},
		{name: "string with digest", raw: `"nousresearch/hermes-agent@sha256:abc123"`, want: "nousresearch/hermes-agent@sha256:abc123"},
		{name: "string with registry port and digest", raw: `"registry.example.com:5000/agent@sha256:abc123"`, want: "registry.example.com:5000/agent@sha256:abc123"},
		{name: "legacy object", raw: `{"repository":"my/agent","tag":"v2"}`, want: "my/agent:v2"},
		{name: "legacy repository only", raw: `{"repository":"my/agent"}`, want: "my/agent:latest"},
		{name: "legacy tag only", raw: `{"tag":"v2"}`, want: "nousresearch/hermes-agent:v2"},
		{name: "empty string", raw: `""`, want: "nousresearch/hermes-agent:latest"},
		{name: "nil", raw: "", want: "nousresearch/hermes-agent:latest"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ha := minimalHA()
			if tt.raw != "" {
				ha.Spec.Hermes = &agentsv1alpha1.Hermes{Image: imageJSON(tt.raw)}
			}
			if got := ha.GetHermes().GetImage(); got != tt.want {
				t.Errorf("GetImage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDigestPinnedImageInPod(t *testing.T) {
	ha := minimalHA()
	ha.Spec.Hermes = &agentsv1alpha1.Hermes{Image: imageJSON(`"nousresearch/hermes-agent@sha256:abc123"`)}
	sts := buildStatefulSet(ha, testConfigHash)
	for _, c := range sts.Spec.Template.Spec.Containers {
		if c.Name != "hermes-agent" {
			continue
		}
		if c.Image != "nousresearch/hermes-agent@sha256:abc123" {
			t.Errorf("hermes-agent image = %q, want digest reference", c.Image)
		}
		return
	}
	t.Errorf("hermes-agent container not found in %+v", sts.Spec.Template.Spec.Containers)
}

func TestUsesLegacyImageForm(t *testing.T) {
	t.Run("false when unset", func(t *testing.T) {
		if minimalHA().UsesLegacyImageForm() {
			t.Error("UsesLegacyImageForm() = true, want false")
		}
	})

	t.Run("false when string form", func(t *testing.T) {
		ha := minimalHA()
		ha.Spec.Hermes = &agentsv1alpha1.Hermes{Image: imageJSON(`"nousresearch/hermes-agent:v2"`)}
		ha.Spec.SearXNG = &agentsv1alpha1.SearXNG{Enabled: true, Image: imageJSON(`"searxng/searxng:latest"`)}
		ha.Spec.Camofox = &agentsv1alpha1.Camofox{Enabled: true, Image: imageJSON(`"camofox:latest"`)}
		if ha.UsesLegacyImageForm() {
			t.Error("UsesLegacyImageForm() = true, want false")
		}
	})

	t.Run("true when object form", func(t *testing.T) {
		ha := minimalHA()
		ha.Spec.Hermes = &agentsv1alpha1.Hermes{Image: imageJSON(`{"repository":"my/agent","tag":"v2"}`)}
		if !ha.UsesLegacyImageForm() {
			t.Error("UsesLegacyImageForm() = false, want true")
		}
	})

	t.Run("true when sidecar object form", func(t *testing.T) {
		ha := minimalHA()
		ha.Spec.SearXNG = &agentsv1alpha1.SearXNG{Enabled: true, Image: imageJSON(`{"tag":"v3"}`)}
		if !ha.UsesLegacyImageForm() {
			t.Error("UsesLegacyImageForm() = false, want true")
		}
		ha = minimalHA()
		ha.Spec.Camofox = &agentsv1alpha1.Camofox{Enabled: true, Image: imageJSON(`{"repository":"camofox"}`)}
		if !ha.UsesLegacyImageForm() {
			t.Error("UsesLegacyImageForm() = false, want true")
		}
	})

	t.Run("false on nil receiver", func(t *testing.T) {
		var ha *agentsv1alpha1.HermesAgent
		if ha.UsesLegacyImageForm() {
			t.Error("UsesLegacyImageForm() = true, want false")
		}
	})
}

func TestDesiredSpecHash(t *testing.T) {
	t.Run("stable for identical spec", func(t *testing.T) {
		ha := minimalHA()
		h1 := desiredSpecHash(buildStatefulSet(ha, testConfigHash))
		h2 := desiredSpecHash(buildStatefulSet(ha, testConfigHash))
		if h1 != h2 {
			t.Error("hash must be deterministic")
		}
	})

	t.Run("changes when replicas change", func(t *testing.T) {
		ha := minimalHA()
		h1 := desiredSpecHash(buildStatefulSet(ha, testConfigHash))
		suspend := true
		ha.Spec.Suspend = &suspend
		if desiredSpecHash(buildStatefulSet(ha, testConfigHash)) == h1 {
			t.Error("expected different hash when replicas change")
		}
	})

	t.Run("changes when pod template changes", func(t *testing.T) {
		ha := minimalHA()
		h1 := desiredSpecHash(buildStatefulSet(ha, testConfigHash))
		ha.Spec.Hermes = &agentsv1alpha1.Hermes{Image: imageJSON(`"nousresearch/hermes-agent:v2"`)}
		if desiredSpecHash(buildStatefulSet(ha, testConfigHash)) == h1 {
			t.Error("expected different hash when pod template changes")
		}
	})

	t.Run("changes when volume claim templates change", func(t *testing.T) {
		ha := minimalHA()
		h1 := desiredSpecHash(buildStatefulSet(ha, testConfigHash))
		size := resource.MustParse("10Gi")
		ha.Spec.Hermes = &agentsv1alpha1.Hermes{Storage: &agentsv1alpha1.HermesStorage{
			Persistence: &agentsv1alpha1.HermesPersistence{Enabled: true, Size: &size},
		}}
		if desiredSpecHash(buildStatefulSet(ha, testConfigHash)) == h1 {
			t.Error("expected different hash when PVC added")
		}
	})

	t.Run("stable when only ObjectMeta differs", func(t *testing.T) {
		ha := minimalHA()
		sts := buildStatefulSet(ha, testConfigHash)
		h1 := desiredSpecHash(sts)
		sts.Labels["k8s-injected"] = testTrue
		if desiredSpecHash(sts) != h1 {
			t.Error("hash must not change when only ObjectMeta differs")
		}
	})

	t.Run("changes when hostUsers changes", func(t *testing.T) {
		ha := minimalHA()
		h1 := desiredSpecHash(buildStatefulSet(ha, testConfigHash))
		ha.Spec.HostUsers = ptrBool(false)
		if desiredSpecHash(buildStatefulSet(ha, testConfigHash)) == h1 {
			t.Error("expected different hash when hostUsers changes")
		}
	})

	t.Run("changes when podAnnotations change", func(t *testing.T) {
		ha := minimalHA()
		h1 := desiredSpecHash(buildStatefulSet(ha, testConfigHash))
		ha.Spec.PodAnnotations = map[string]string{"rotatedAt": "2026-07-06T12:00:00Z"}
		if desiredSpecHash(buildStatefulSet(ha, testConfigHash)) == h1 {
			t.Error("expected different hash when podAnnotations change")
		}
	})

	t.Run("changes when podLabels change", func(t *testing.T) {
		ha := minimalHA()
		h1 := desiredSpecHash(buildStatefulSet(ha, testConfigHash))
		ha.Spec.PodLabels = map[string]string{"example.com/internet-client": testTrue}
		if desiredSpecHash(buildStatefulSet(ha, testConfigHash)) == h1 {
			t.Error("expected different hash when podLabels change")
		}
	})

	t.Run("changes when runtimeClassName changes", func(t *testing.T) {
		ha := minimalHA()
		h1 := desiredSpecHash(buildStatefulSet(ha, testConfigHash))
		ha.Spec.RuntimeClassName = ptrString("kata-qemu")
		if desiredSpecHash(buildStatefulSet(ha, testConfigHash)) == h1 {
			t.Error("expected different hash when runtimeClassName changes")
		}
	})

	t.Run("changes when priorityClassName changes", func(t *testing.T) {
		ha := minimalHA()
		h1 := desiredSpecHash(buildStatefulSet(ha, testConfigHash))
		ha.Spec.PriorityClassName = testPriorityClass
		if desiredSpecHash(buildStatefulSet(ha, testConfigHash)) == h1 {
			t.Error("expected different hash when priorityClassName changes")
		}
	})
}

func TestBuildStatefulSetHostUsers(t *testing.T) {
	t.Run("unset leaves the pod in the host user namespace", func(t *testing.T) {
		ha := minimalHA()
		sts := buildStatefulSet(ha, testConfigHash)
		if got := sts.Spec.Template.Spec.HostUsers; got != nil {
			t.Errorf("expected nil hostUsers, got %v", *got)
		}
	})

	t.Run("false gives the pod its own user namespace", func(t *testing.T) {
		ha := minimalHA()
		ha.Spec.HostUsers = ptrBool(false)
		sts := buildStatefulSet(ha, testConfigHash)
		got := sts.Spec.Template.Spec.HostUsers
		if got == nil {
			t.Fatal("expected hostUsers to be set")
		}
		if *got {
			t.Error("hostUsers = true, want false")
		}
	})

	// An explicit true is not the same as an unset field: it pins the pod to
	// the host user namespace even if the cluster default ever changes.
	t.Run("true is passed through rather than dropped", func(t *testing.T) {
		ha := minimalHA()
		ha.Spec.HostUsers = ptrBool(true)
		sts := buildStatefulSet(ha, testConfigHash)
		got := sts.Spec.Template.Spec.HostUsers
		if got == nil {
			t.Fatal("expected hostUsers to be set")
		}
		if !*got {
			t.Error("hostUsers = false, want true")
		}
	})
}

func TestBuildStatefulSetPodAnnotations(t *testing.T) {
	t.Run("no extra annotations when unset", func(t *testing.T) {
		ha := minimalHA()
		sts := buildStatefulSet(ha, testConfigHash)
		if len(sts.Spec.Template.Annotations) != 1 {
			t.Errorf("expected only config-hash annotation, got %v", sts.Spec.Template.Annotations)
		}
	})

	t.Run("user annotations are merged in", func(t *testing.T) {
		ha := minimalHA()
		ha.Spec.PodAnnotations = map[string]string{"rotatedAt": "2026-07-06T12:00:00Z", "prometheus.io/scrape": testTrue}
		sts := buildStatefulSet(ha, testConfigHash)
		if sts.Spec.Template.Annotations["rotatedAt"] != "2026-07-06T12:00:00Z" {
			t.Error("expected rotatedAt annotation to be present")
		}
		if sts.Spec.Template.Annotations["prometheus.io/scrape"] != testTrue {
			t.Error("expected prometheus.io/scrape annotation to be present")
		}
		if _, ok := sts.Spec.Template.Annotations[domain+"/config-hash"]; !ok {
			t.Error("expected config-hash annotation to still be present")
		}
	})
}

func TestBuildStatefulSetPodLabels(t *testing.T) {
	// selectorMatchesTemplate reports whether every selector label is present
	// with the same value on the pod template, i.e. whether the StatefulSet can
	// still adopt its own pods.
	selectorMatchesTemplate := func(sts *appsv1.StatefulSet) bool {
		for k, v := range sts.Spec.Selector.MatchLabels {
			if sts.Spec.Template.Labels[k] != v {
				return false
			}
		}
		return true
	}

	t.Run("only operator labels when unset", func(t *testing.T) {
		ha := minimalHA()
		sts := buildStatefulSet(ha, testConfigHash)
		if len(sts.Spec.Template.Labels) != 3 {
			t.Errorf("expected only the three operator labels, got %v", sts.Spec.Template.Labels)
		}
	})

	t.Run("user labels are merged in", func(t *testing.T) {
		ha := minimalHA()
		ha.Spec.PodLabels = map[string]string{
			"example.com/internet-client": testTrue,
			"example.com/traefik-route":   testTrue,
		}
		sts := buildStatefulSet(ha, testConfigHash)
		if sts.Spec.Template.Labels["example.com/internet-client"] != testTrue {
			t.Error("expected example.com/internet-client label to be present")
		}
		if sts.Spec.Template.Labels["example.com/traefik-route"] != testTrue {
			t.Error("expected example.com/traefik-route label to be present")
		}
		if sts.Spec.Template.Labels[agentsv1alpha1.LabelManagedBy] != agentsv1alpha1.ManagedByValue {
			t.Error("expected the operator labels to still be present")
		}
		if !selectorMatchesTemplate(sts) {
			t.Errorf("selector %v no longer matches template labels %v",
				sts.Spec.Selector.MatchLabels, sts.Spec.Template.Labels)
		}
	})

	t.Run("user labels cannot shadow the selector labels", func(t *testing.T) {
		ha := minimalHA()
		ha.Spec.PodLabels = map[string]string{
			agentsv1alpha1.LabelName:     "hijacked",
			agentsv1alpha1.LabelInstance: "hijacked",
		}
		sts := buildStatefulSet(ha, testConfigHash)
		if sts.Spec.Template.Labels[agentsv1alpha1.LabelName] != agentsv1alpha1.AppNameValue {
			t.Errorf("%s = %q, want %q", agentsv1alpha1.LabelName, sts.Spec.Template.Labels[agentsv1alpha1.LabelName], agentsv1alpha1.AppNameValue)
		}
		if sts.Spec.Template.Labels[agentsv1alpha1.LabelInstance] != ha.Name {
			t.Errorf("%s = %q, want %q", agentsv1alpha1.LabelInstance, sts.Spec.Template.Labels[agentsv1alpha1.LabelInstance], ha.Name)
		}
		if !selectorMatchesTemplate(sts) {
			t.Errorf("selector %v no longer matches template labels %v",
				sts.Spec.Selector.MatchLabels, sts.Spec.Template.Labels)
		}
	})

	t.Run("StatefulSet labels are untouched", func(t *testing.T) {
		ha := minimalHA()
		ha.Spec.PodLabels = map[string]string{"example.com/internet-client": testTrue}
		sts := buildStatefulSet(ha, testConfigHash)
		if _, ok := sts.Labels["example.com/internet-client"]; ok {
			t.Errorf("podLabels must not leak onto the StatefulSet, got %v", sts.Labels)
		}
	})
}

func TestBuildStatefulSetRuntimeClassName(t *testing.T) {
	t.Run("unset leaves the pod on the cluster default runtime", func(t *testing.T) {
		ha := minimalHA()
		sts := buildStatefulSet(ha, testConfigHash)
		if sts.Spec.Template.Spec.RuntimeClassName != nil {
			t.Errorf("expected nil runtimeClassName, got %q", *sts.Spec.Template.Spec.RuntimeClassName)
		}
	})

	t.Run("set is passed through to the pod spec", func(t *testing.T) {
		ha := minimalHA()
		ha.Spec.RuntimeClassName = ptrString("kata-qemu-runtime-rs")
		sts := buildStatefulSet(ha, testConfigHash)
		got := sts.Spec.Template.Spec.RuntimeClassName
		if got == nil {
			t.Fatal("expected runtimeClassName to be set")
		}
		if *got != "kata-qemu-runtime-rs" {
			t.Errorf("runtimeClassName = %q, want %q", *got, "kata-qemu-runtime-rs")
		}
	})
}

func TestBuildStatefulSetPriorityClassName(t *testing.T) {
	t.Run("empty when unset", func(t *testing.T) {
		ha := minimalHA()
		sts := buildStatefulSet(ha, testConfigHash)
		if got := sts.Spec.Template.Spec.PriorityClassName; got != "" {
			t.Errorf("expected empty priorityClassName, got %q", got)
		}
	})

	t.Run("set is passed through to the pod spec", func(t *testing.T) {
		ha := minimalHA()
		ha.Spec.PriorityClassName = testPriorityClass
		sts := buildStatefulSet(ha, testConfigHash)
		if got := sts.Spec.Template.Spec.PriorityClassName; got != testPriorityClass {
			t.Errorf("priorityClassName = %q, want %q", got, testPriorityClass)
		}
	})
}

func TestBuildStatefulSetPersistenceVolumes(t *testing.T) {
	findVolume := func(sts *appsv1.StatefulSet) *corev1.Volume {
		for i := range sts.Spec.Template.Spec.Volumes {
			if sts.Spec.Template.Spec.Volumes[i].Name == hermesHomeVolume {
				return &sts.Spec.Template.Spec.Volumes[i]
			}
		}
		return nil
	}

	t.Run("existingSnapshot mounts the restored PVC explicitly", func(t *testing.T) {
		ha := minimalHA()
		ha.Spec.Hermes = &agentsv1alpha1.Hermes{Storage: &agentsv1alpha1.HermesStorage{
			Persistence: &agentsv1alpha1.HermesPersistence{
				Enabled:          true,
				ExistingSnapshot: ptrString("snap-1"),
			},
		}}
		sts := buildStatefulSet(ha, testConfigHash)
		if len(sts.Spec.VolumeClaimTemplates) != 0 {
			t.Error("existingSnapshot must not provision a volumeClaimTemplate")
		}
		vol := findVolume(sts)
		if vol == nil || vol.PersistentVolumeClaim == nil || vol.PersistentVolumeClaim.ClaimName != "snap-1-restore" {
			t.Errorf("expected explicit volume mounting snap-1-restore, got %+v", vol)
		}
	})

	t.Run("existingClaim wins over existingSnapshot", func(t *testing.T) {
		ha := minimalHA()
		ha.Spec.Hermes = &agentsv1alpha1.Hermes{Storage: &agentsv1alpha1.HermesStorage{
			Persistence: &agentsv1alpha1.HermesPersistence{
				Enabled:          true,
				ExistingClaim:    ptrString("my-claim"),
				ExistingSnapshot: ptrString("snap-1"),
			},
		}}
		sts := buildStatefulSet(ha, testConfigHash)
		vol := findVolume(sts)
		if vol == nil || vol.PersistentVolumeClaim == nil || vol.PersistentVolumeClaim.ClaimName != "my-claim" {
			t.Errorf("expected existingClaim to win, got %+v", vol)
		}
	})

	t.Run("enabled without snapshot uses volumeClaimTemplate", func(t *testing.T) {
		ha := minimalHA()
		size := resource.MustParse("10Gi")
		ha.Spec.Hermes = &agentsv1alpha1.Hermes{Storage: &agentsv1alpha1.HermesStorage{
			Persistence: &agentsv1alpha1.HermesPersistence{Enabled: true, Size: &size},
		}}
		sts := buildStatefulSet(ha, testConfigHash)
		if len(sts.Spec.VolumeClaimTemplates) != 1 || sts.Spec.VolumeClaimTemplates[0].Name != hermesHomeVolume {
			t.Errorf("expected hermes-data volumeClaimTemplate, got %+v", sts.Spec.VolumeClaimTemplates)
		}
		if findVolume(sts) != nil {
			t.Error("expected no explicit hermes-data volume")
		}
	})
}

func ptrBool(b bool) *bool       { return &b }
func ptrInt(i int) *int          { return &i }
func ptrString(s string) *string { return &s }

func TestBuildPluginsScript(t *testing.T) {

	t.Run("default enable", func(t *testing.T) {
		got := buildPluginsScript(hermesDefaultProfile, []agentsv1alpha1.HermesPlugin{
			{Identifier: "anpicasso/hermes-plugin-chrome-profiles"},
		})

		wantCmd := `hermes plugins install -p "default" --force --enable "anpicasso/hermes-plugin-chrome-profiles"`
		if !strings.Contains(got, wantCmd) {
			t.Errorf("expected install command %q in script, got:\n%s", wantCmd, got)
		}

		wantCase := `"hermes-plugin-chrome-profiles"`
		if !strings.Contains(got, wantCase+")") {
			t.Errorf("expected case pattern %q in script, got:\n%s", wantCase, got)
		}

		wantManifest := "profiles/default/plugins"
		if !strings.Contains(got, wantManifest) {
			t.Errorf("expected manifest path %q in script, got:\n%s", wantManifest, got)
		}
	})

	t.Run("explicit no-enable", func(t *testing.T) {
		got := buildPluginsScript(hermesDefaultProfile, []agentsv1alpha1.HermesPlugin{
			{Identifier: "https://github.com/owner/hermes-plugin-foo.git", Enable: ptrBool(false)},
		})

		wantCmd := `hermes plugins install -p "default" --force --no-enable "https://github.com/owner/hermes-plugin-foo.git"`
		if !strings.Contains(got, wantCmd) {
			t.Errorf("expected install command %q in script, got:\n%s", wantCmd, got)
		}
	})

	t.Run("explicit enable true", func(t *testing.T) {
		got := buildPluginsScript(hermesDefaultProfile, []agentsv1alpha1.HermesPlugin{
			{Identifier: "owner/repo", Enable: ptrBool(true)},
		})

		wantCmd := `hermes plugins install -p "default" --force --enable "owner/repo"`
		if !strings.Contains(got, wantCmd) {
			t.Errorf("expected install command %q in script, got:\n%s", wantCmd, got)
		}
	})

	t.Run("remove command uses bare hermes not absolute path", func(t *testing.T) {
		got := buildPluginsScript(hermesDefaultProfile, []agentsv1alpha1.HermesPlugin{
			{Identifier: "owner/hermes-plugin-a"},
		})
		if strings.Contains(got, "/hermes ") {
			t.Errorf("expected bare 'hermes' command, not absolute '/hermes' path, got:\n%s", got)
		}
		if !strings.Contains(got, `hermes plugins remove -p "default" "$name"`) {
			t.Errorf("expected plugin remove command in script, got:\n%s", got)
		}
	})

	t.Run("multiple plugins build case pattern and manifest", func(t *testing.T) {
		got := buildPluginsScript(hermesDefaultProfile, []agentsv1alpha1.HermesPlugin{
			{Identifier: "owner/hermes-plugin-a"},
			{Identifier: "owner/hermes-plugin-b", Enable: ptrBool(false)},
		})

		wantCase := `"hermes-plugin-a"|"hermes-plugin-b"`
		if !strings.Contains(got, wantCase) {
			t.Errorf("expected case pattern %q, got:\n%s", wantCase, got)
		}

		wantManifest := "hermes-plugin-a\nhermes-plugin-b"
		if !strings.Contains(got, wantManifest) {
			t.Errorf("expected manifest %q, got:\n%s", wantManifest, got)
		}

		if !strings.Contains(got, `hermes plugins install -p "default" --force --enable "owner/hermes-plugin-a"`) {
			t.Errorf("missing install command for plugin a in:\n%s", got)
		}
		if !strings.Contains(got, `hermes plugins install -p "default" --force --no-enable "owner/hermes-plugin-b"`) {
			t.Errorf("missing install command for plugin b in:\n%s", got)
		}
	})

	t.Run("with ref pins commit sha", func(t *testing.T) {
		got := buildPluginsScript(hermesDefaultProfile, []agentsv1alpha1.HermesPlugin{
			{Identifier: "owner/hermes-plugin-pinned", Ref: "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0"},
		})

		wantCmd := `hermes plugins install -p "default" --force --enable --ref "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0" "owner/hermes-plugin-pinned"`
		if !strings.Contains(got, wantCmd) {
			t.Errorf("expected install command %q in script, got:\n%s", wantCmd, got)
		}
	})

	t.Run("named profile uses profile in commands and manifest", func(t *testing.T) {
		got := buildPluginsScript("coder", []agentsv1alpha1.HermesPlugin{
			{Identifier: "owner/hermes-plugin-foo"},
		})

		if !strings.Contains(got, `hermes plugins install -p "coder"`) {
			t.Errorf("expected -p \"coder\" in install command, got:\n%s", got)
		}
		if !strings.Contains(got, `hermes plugins remove -p "coder"`) {
			t.Errorf("expected -p \"coder\" in remove command, got:\n%s", got)
		}
		if !strings.Contains(got, "profiles/coder/plugins") {
			t.Errorf("expected manifest path profiles/coder/plugins, got:\n%s", got)
		}
	})
}

func TestBuildSkillsScript(t *testing.T) {

	t.Run("identifier only", func(t *testing.T) {
		got := buildSkillsScript(hermesDefaultProfile, []agentsv1alpha1.HermesSkill{
			{Identifier: "openai/skills/skill-creator"},
		})

		wantCmd := `hermes skills install -p "default" --yes openai/skills/skill-creator`
		if !strings.Contains(got, wantCmd) {
			t.Errorf("expected %q in script, got:\n%s", wantCmd, got)
		}

		// name derived from identifier: skill-creator
		if !strings.Contains(got, `"skill-creator"`) {
			t.Errorf("expected name %q in case pattern, got:\n%s", "skill-creator", got)
		}
	})

	t.Run("with all options", func(t *testing.T) {
		got := buildSkillsScript(hermesDefaultProfile, []agentsv1alpha1.HermesSkill{
			{
				Identifier: "https://example.com/SKILL.md",
				Category:   "writing",
				Name:       "my-skill",
				Force:      true,
			},
		})

		wantCmd := `hermes skills install -p "default" --yes --category writing --name my-skill --force https://example.com/SKILL.md`
		if !strings.Contains(got, wantCmd) {
			t.Errorf("expected %q in script, got:\n%s", wantCmd, got)
		}

		if !strings.Contains(got, `"my-skill"`) {
			t.Errorf("expected explicit name in case pattern, got:\n%s", got)
		}
	})

	t.Run("uninstall command present", func(t *testing.T) {
		got := buildSkillsScript(hermesDefaultProfile, []agentsv1alpha1.HermesSkill{
			{Identifier: "openai/skills/s1"},
		})

		if !strings.Contains(got, `hermes skills uninstall -p "default" "$name" || true`) {
			t.Errorf("expected uninstall command in script, got:\n%s", got)
		}
	})

	t.Run("multiple skills manifest order", func(t *testing.T) {
		got := buildSkillsScript(hermesDefaultProfile, []agentsv1alpha1.HermesSkill{
			{Identifier: "openai/skills/alpha"},
			{Identifier: "openai/skills/beta.md"},
		})

		wantCase := `"alpha"|"beta"`
		if !strings.Contains(got, wantCase) {
			t.Errorf("expected case pattern %q, got:\n%s", wantCase, got)
		}
		if !strings.Contains(got, "alpha\nbeta") {
			t.Errorf("expected manifest content, got:\n%s", got)
		}
	})

	t.Run("named profile uses profile in commands and manifest", func(t *testing.T) {
		got := buildSkillsScript("coder", []agentsv1alpha1.HermesSkill{
			{Identifier: "openai/skills/foo"},
		})

		if !strings.Contains(got, `hermes skills install -p "coder"`) {
			t.Errorf("expected -p \"coder\" in install command, got:\n%s", got)
		}
		if !strings.Contains(got, `hermes skills uninstall -p "coder"`) {
			t.Errorf("expected -p \"coder\" in uninstall command, got:\n%s", got)
		}
		if !strings.Contains(got, "profiles/coder/skills") {
			t.Errorf("expected manifest path profiles/coder/skills, got:\n%s", got)
		}
	})

	t.Run("update command present per skill", func(t *testing.T) {
		got := buildSkillsScript(hermesDefaultProfile, []agentsv1alpha1.HermesSkill{
			{Identifier: "openai/skills/skill-creator"},
		})

		wantUpdate := `hermes skills update -p "default" skill-creator || true`
		if !strings.Contains(got, wantUpdate) {
			t.Errorf("expected update command %q in script, got:\n%s", wantUpdate, got)
		}
	})

	t.Run("update command uses explicit name", func(t *testing.T) {
		got := buildSkillsScript(hermesDefaultProfile, []agentsv1alpha1.HermesSkill{
			{Identifier: "https://example.com/SKILL.md", Name: "my-skill"},
		})

		wantUpdate := `hermes skills update -p "default" my-skill || true`
		if !strings.Contains(got, wantUpdate) {
			t.Errorf("expected update command %q in script, got:\n%s", wantUpdate, got)
		}
	})

	t.Run("update command for named profile", func(t *testing.T) {
		got := buildSkillsScript("coder", []agentsv1alpha1.HermesSkill{
			{Identifier: "openai/skills/foo"},
		})

		wantUpdate := `hermes skills update -p "coder" foo || true`
		if !strings.Contains(got, wantUpdate) {
			t.Errorf("expected update command %q in script, got:\n%s", wantUpdate, got)
		}
	})

	t.Run("update commands for multiple skills", func(t *testing.T) {
		got := buildSkillsScript(hermesDefaultProfile, []agentsv1alpha1.HermesSkill{
			{Identifier: "openai/skills/alpha"},
			{Identifier: "openai/skills/beta.md"},
		})

		wantUpdate1 := `hermes skills update -p "default" alpha || true`
		wantUpdate2 := `hermes skills update -p "default" beta || true`
		if !strings.Contains(got, wantUpdate1) {
			t.Errorf("expected update command %q in script, got:\n%s", wantUpdate1, got)
		}
		if !strings.Contains(got, wantUpdate2) {
			t.Errorf("expected update command %q in script, got:\n%s", wantUpdate2, got)
		}
	})
}

func TestBuildBundlesScript(t *testing.T) {

	t.Run("minimal", func(t *testing.T) {
		got := buildBundlesScript(hermesDefaultProfile, []agentsv1alpha1.HermesBundle{
			{Name: "finance"},
		})

		wantCmd := `hermes bundles create -p "default" "finance"`
		if !strings.Contains(got, wantCmd) {
			t.Errorf("expected %q in script, got:\n%s", wantCmd, got)
		}
	})

	t.Run("all options", func(t *testing.T) {
		got := buildBundlesScript(hermesDefaultProfile, []agentsv1alpha1.HermesBundle{
			{
				Name:        "finance",
				Skills:      []string{"a", "b"},
				Description: "d",
				Instruction: "i",
				Force:       true,
			},
		})

		wantCmd := `hermes bundles create -p "default" --skill "a" --skill "b" --description "d" --instruction "i" --force "finance"`
		if !strings.Contains(got, wantCmd) {
			t.Errorf("expected:\n%s\n\nin script:\n%s", wantCmd, got)
		}
	})

	t.Run("delete command present", func(t *testing.T) {
		got := buildBundlesScript(hermesDefaultProfile, []agentsv1alpha1.HermesBundle{
			{Name: "finance"},
		})
		if !strings.Contains(got, `hermes bundles delete -p "default" "$name" || true`) {
			t.Errorf("expected delete command in script, got:\n%s", got)
		}
	})

	t.Run("multiple bundles manifest order", func(t *testing.T) {
		got := buildBundlesScript(hermesDefaultProfile, []agentsv1alpha1.HermesBundle{
			{Name: "a"},
			{Name: "b"},
		})

		wantCase := `"a"|"b"`
		if !strings.Contains(got, wantCase) {
			t.Errorf("expected case pattern %q, got:\n%s", wantCase, got)
		}
		if !strings.Contains(got, "a\nb") {
			t.Errorf("expected manifest content, got:\n%s", got)
		}
	})

	t.Run("named profile uses profile in commands and manifest", func(t *testing.T) {
		got := buildBundlesScript("coder", []agentsv1alpha1.HermesBundle{
			{Name: "myBundle"},
		})

		if !strings.Contains(got, `hermes bundles create -p "coder"`) {
			t.Errorf("expected -p \"coder\" in create command, got:\n%s", got)
		}
		if !strings.Contains(got, `hermes bundles delete -p "coder"`) {
			t.Errorf("expected -p \"coder\" in delete command, got:\n%s", got)
		}
		if !strings.Contains(got, "profiles/coder/bundles") {
			t.Errorf("expected manifest path profiles/coder/bundles, got:\n%s", got)
		}
	})
}

func TestBuildPythonPackagesScript(t *testing.T) {

	t.Run("nil config returns no-op", func(t *testing.T) {
		got := buildPythonPackagesScript(nil)
		if !strings.Contains(got, "No Python packages configured") {
			t.Errorf("expected no-op message, got:\n%s", got)
		}
	})

	t.Run("empty packages returns no-op", func(t *testing.T) {
		got := buildPythonPackagesScript(&agentsv1alpha1.HermesPipPackages{})
		if !strings.Contains(got, "No Python packages configured") {
			t.Errorf("expected no-op message, got:\n%s", got)
		}
	})

	t.Run("single package install command", func(t *testing.T) {
		got := buildPythonPackagesScript(&agentsv1alpha1.HermesPipPackages{
			Install: []string{"requests"},
		})

		wantCmd := `uv pip install --python /opt/hermes/.venv/bin/python --target "$TARGET" "requests"`
		if !strings.Contains(got, wantCmd) {
			t.Errorf("expected %q in script, got:\n%s", wantCmd, got)
		}
	})

	t.Run("multiple packages all quoted", func(t *testing.T) {
		got := buildPythonPackagesScript(&agentsv1alpha1.HermesPipPackages{
			Install: []string{"requests", "pandas==2.1.0", "beautifulsoup4[lxml]"},
		})

		wantCmd := `uv pip install --python /opt/hermes/.venv/bin/python --target "$TARGET" "requests" "pandas==2.1.0" "beautifulsoup4[lxml]"`
		if !strings.Contains(got, wantCmd) {
			t.Errorf("expected %q in script, got:\n%s", wantCmd, got)
		}
	})

	t.Run("extraArgs inserted before packages", func(t *testing.T) {
		got := buildPythonPackagesScript(&agentsv1alpha1.HermesPipPackages{
			Install:   []string{"langfuse"},
			ExtraArgs: []string{"--index-url=https://private.example.com/simple"},
		})

		wantCmd := `uv pip install --python /opt/hermes/.venv/bin/python --target "$TARGET" "--index-url=https://private.example.com/simple" "langfuse"`
		if !strings.Contains(got, wantCmd) {
			t.Errorf("expected %q in script, got:\n%s", wantCmd, got)
		}
	})

	t.Run("multiple extraArgs all quoted", func(t *testing.T) {
		got := buildPythonPackagesScript(&agentsv1alpha1.HermesPipPackages{
			Install:   []string{"requests"},
			ExtraArgs: []string{"--index-url=https://a.example.com/simple", "--extra-index-url=https://pypi.org/simple"},
		})

		wantCmd := `uv pip install --python /opt/hermes/.venv/bin/python --target "$TARGET" "--index-url=https://a.example.com/simple" "--extra-index-url=https://pypi.org/simple" "requests"`
		if !strings.Contains(got, wantCmd) {
			t.Errorf("expected %q in script, got:\n%s", wantCmd, got)
		}
	})

	t.Run("manifest contains package list", func(t *testing.T) {
		got := buildPythonPackagesScript(&agentsv1alpha1.HermesPipPackages{
			Install: []string{"alpha", "beta"},
		})

		if !strings.Contains(got, "alpha\nbeta") {
			t.Errorf("expected manifest content 'alpha\\nbeta', got:\n%s", got)
		}
	})
}

func TestBuildCronsScript(t *testing.T) {

	t.Run("minimal", func(t *testing.T) {
		got := buildCronsScript(hermesDefaultProfile, []agentsv1alpha1.HermesCron{
			{Name: "daily", Schedule: "0 9 * * *"},
		})

		wantCmd := `hermes cron create -p "default" --name "daily" "0 9 * * *"`
		if !strings.Contains(got, wantCmd) {
			t.Errorf("expected %q in script, got:\n%s", wantCmd, got)
		}
	})

	t.Run("with prompt", func(t *testing.T) {
		got := buildCronsScript(hermesDefaultProfile, []agentsv1alpha1.HermesCron{
			{Name: "p", Schedule: "30m", Prompt: "say hi"},
		})

		wantCmd := `hermes cron create -p "default" --name "p" "30m" "say hi"`
		if !strings.Contains(got, wantCmd) {
			t.Errorf("expected %q in script, got:\n%s", wantCmd, got)
		}
	})

	t.Run("all options", func(t *testing.T) {
		got := buildCronsScript(hermesDefaultProfile, []agentsv1alpha1.HermesCron{
			{
				Name:            "full",
				Schedule:        "every 2h",
				Prompt:          "do thing",
				Deliver:         "telegram",
				Repeat:          ptrInt(3),
				Skills:          []string{"alpha", "beta"},
				Script:          "myscript.sh",
				NoAgent:         true,
				Workdir:         "/opt/data",
				MonitorScript:   "source.sh",
				MonitorURL:      "https://example.com/status",
				Model:           "gpt-5",
				Provider:        "openrouter",
				ReasoningEffort: "high",
				Continuity:      true,
				Profile:         "default",
			},
		})

		wantCmd := `hermes cron create -p "default" --name "full" --deliver "telegram" --repeat 3 --skill "alpha" --skill "beta" --script "myscript.sh" --no-agent --workdir "/opt/data" --monitor-script "source.sh" --monitor-url "https://example.com/status" --model "gpt-5" --provider "openrouter" --reasoning-effort "high" --continuity --profile "default" "every 2h" "do thing"`
		if !strings.Contains(got, wantCmd) {
			t.Errorf("expected:\n%s\n\nin script:\n%s", wantCmd, got)
		}
	})

	t.Run("new params only", func(t *testing.T) {
		got := buildCronsScript(hermesDefaultProfile, []agentsv1alpha1.HermesCron{
			{
				Name:            "new",
				Schedule:        "0 9 * * *",
				MonitorURL:      "https://example.com",
				Model:           "claude-opus-4-5",
				Provider:        "nous",
				ReasoningEffort: "low",
				Continuity:      true,
			},
		})

		wantCmd := `hermes cron create -p "default" --name "new" --monitor-url "https://example.com" --model "claude-opus-4-5" --provider "nous" --reasoning-effort "low" --continuity "0 9 * * *"`
		if !strings.Contains(got, wantCmd) {
			t.Errorf("expected:\n%s\n\nin script:\n%s", wantCmd, got)
		}
	})

	t.Run("remove uses hermes cron remove", func(t *testing.T) {
		got := buildCronsScript(hermesDefaultProfile, []agentsv1alpha1.HermesCron{
			{Name: "j", Schedule: "1h"},
		})
		if !strings.Contains(got, `hermes cron remove -p "default" "$id" || true`) {
			t.Errorf("expected remove command in script, got:\n%s", got)
		}
	})

	t.Run("manifest contains names", func(t *testing.T) {
		got := buildCronsScript(hermesDefaultProfile, []agentsv1alpha1.HermesCron{
			{Name: "a", Schedule: "1h"},
			{Name: "b", Schedule: "2h"},
		})
		if !strings.Contains(got, "a\nb") {
			t.Errorf("expected manifest with names a\\nb, got:\n%s", got)
		}
	})

	t.Run("default profile uses top-level cron jobs.json path", func(t *testing.T) {
		got := buildCronsScript(hermesDefaultProfile, []agentsv1alpha1.HermesCron{
			{Name: "j", Schedule: "1h"},
		})
		if !strings.Contains(got, "/cron/jobs.json") {
			t.Errorf("expected /cron/jobs.json for default profile, got:\n%s", got)
		}
		if strings.Contains(got, "/profiles/default/cron/jobs.json") {
			t.Errorf("expected top-level /cron/jobs.json, not profiles path, got:\n%s", got)
		}
	})

	t.Run("named profile uses profiles cron jobs.json path", func(t *testing.T) {
		got := buildCronsScript("coder", []agentsv1alpha1.HermesCron{
			{Name: "j", Schedule: "1h"},
		})
		if !strings.Contains(got, "/profiles/coder/cron/jobs.json") {
			t.Errorf("expected /profiles/coder/cron/jobs.json, got:\n%s", got)
		}
		if !strings.Contains(got, "profiles/coder/crons") {
			t.Errorf("expected manifest path profiles/coder/crons, got:\n%s", got)
		}
	})
}

// findInitContainer returns a pointer to the init container with the given
// name, or nil if none exists in the StatefulSet.
func findInitContainer(sts *appsv1.StatefulSet, name string) *corev1.Container {
	for i := range sts.Spec.Template.Spec.InitContainers {
		if sts.Spec.Template.Spec.InitContainers[i].Name == name {
			return &sts.Spec.Template.Spec.InitContainers[i]
		}
	}
	return nil
}

// findHermesContainer returns a pointer to the hermes-agent container in the
// StatefulSet, or nil if none exists.
func findHermesContainer(sts *appsv1.StatefulSet) *corev1.Container {
	for i := range sts.Spec.Template.Spec.Containers {
		if sts.Spec.Template.Spec.Containers[i].Name == hermesContainerName {
			return &sts.Spec.Template.Spec.Containers[i]
		}
	}
	return nil
}

// hasEnvVar reports whether the given env var name is present in the slice.
func hasEnvVar(envs []corev1.EnvVar, name string) bool {
	for _, e := range envs {
		if e.Name == name {
			return true
		}
	}
	return false
}

// hasVolumeMount reports whether a volume mount with the given name exists.
func hasVolumeMount(mounts []corev1.VolumeMount, name string) bool {
	for _, m := range mounts {
		if m.Name == name {
			return true
		}
	}
	return false
}

func TestOperatorDotEnvAPIServer(t *testing.T) {
	t.Run("enabled: ConfigMap has operator keys, init-hermes mounts them", func(t *testing.T) {
		ha := minimalHA()
		ha.Spec.Hermes = &agentsv1alpha1.Hermes{
			Config: &agentsv1alpha1.HermesConfig{
				APIServer: &agentsv1alpha1.HermesAPIServer{Enabled: true},
			},
		}

		// ConfigMap must contain the operator env-var keys.
		data, err := buildHermesConfigMapData(ha, resolvedConfigDocuments{})
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"API_SERVER_ENABLED", "API_SERVER_HOST", "API_SERVER_PORT"} {
			if _, ok := data[key]; !ok {
				t.Errorf("expected %q in ConfigMap data", key)
			}
		}
		if data["API_SERVER_ENABLED"] != testTrue {
			t.Errorf("expected API_SERVER_ENABLED=true, got %q", data["API_SERVER_ENABLED"])
		}
		if data["API_SERVER_HOST"] != "0.0.0.0" {
			t.Errorf("expected API_SERVER_HOST=0.0.0.0, got %q", data["API_SERVER_HOST"])
		}

		// init-hermes must exist and mount the operator ConfigMap and Secret.
		sts := buildStatefulSet(ha, testConfigHash)
		ic := findInitContainer(sts, consolidatedInitContainerName)
		if ic == nil {
			t.Fatal("expected init-hermes init container")
		}
		if !hasVolumeMount(ic.VolumeMounts, "hermes-operator-dotenv-configmap") {
			t.Error("expected hermes-operator-dotenv-configmap volume mount on init-hermes")
		}
		if !hasVolumeMount(ic.VolumeMounts, "hermes-operator-dotenv-secret") {
			t.Error("expected hermes-operator-dotenv-secret volume mount on init-hermes")
		}
		// The script must iterate the operator mount paths.
		if !strings.Contains(ic.Args[0], "/hermes-operator-dotenv-configmap") {
			t.Errorf("expected operator configmap mount path in script, got:\n%s", ic.Args[0])
		}
		if !strings.Contains(ic.Args[0], "/hermes-operator-dotenv-secret") {
			t.Errorf("expected operator secret mount path in script, got:\n%s", ic.Args[0])
		}

		// API_SERVER_* env vars are injected via .env, not container env.
		c := findHermesContainer(sts)
		if c == nil {
			t.Fatal("expected hermes-agent container")
		}
		for _, name := range []string{"API_SERVER_ENABLED", "API_SERVER_HOST", "API_SERVER_PORT", "API_SERVER_KEY"} {
			if hasEnvVar(c.Env, name) {
				t.Errorf("expected %q absent from hermes-agent container Env (injected via .env)", name)
			}
		}
	})

	t.Run("corsOrigins in ConfigMap when set", func(t *testing.T) {
		ha := minimalHA()
		ha.Spec.Hermes = &agentsv1alpha1.Hermes{
			Config: &agentsv1alpha1.HermesConfig{
				APIServer: &agentsv1alpha1.HermesAPIServer{
					Enabled:     true,
					CORSOrigins: []string{"https://a.example", "https://b.example"},
				},
			},
		}
		data, err := buildHermesConfigMapData(ha, resolvedConfigDocuments{})
		if err != nil {
			t.Fatal(err)
		}
		if data["API_SERVER_CORS_ORIGINS"] != "https://a.example,https://b.example" {
			t.Errorf("expected CORS origins in ConfigMap, got %q", data["API_SERVER_CORS_ORIGINS"])
		}
	})

	t.Run("custom port in ConfigMap", func(t *testing.T) {
		ha := minimalHA()
		port := int32(9000)
		ha.Spec.Hermes = &agentsv1alpha1.Hermes{
			Config: &agentsv1alpha1.HermesConfig{
				APIServer: &agentsv1alpha1.HermesAPIServer{Enabled: true, Port: &port},
			},
		}
		data, err := buildHermesConfigMapData(ha, resolvedConfigDocuments{})
		if err != nil {
			t.Fatal(err)
		}
		if data["API_SERVER_PORT"] != "9000" {
			t.Errorf("expected API_SERVER_PORT=9000, got %q", data["API_SERVER_PORT"])
		}
	})

	t.Run("disabled: no operator keys in ConfigMap, no dotenv section", func(t *testing.T) {
		ha := minimalHA()
		data, err := buildHermesConfigMapData(ha, resolvedConfigDocuments{})
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"API_SERVER_ENABLED", "API_SERVER_HOST", "API_SERVER_PORT"} {
			if _, ok := data[key]; ok {
				t.Errorf("expected %q absent from ConfigMap when apiServer disabled", key)
			}
		}
		sts := buildStatefulSet(ha, testConfigHash)
		ic := findInitContainer(sts, consolidatedInitContainerName)
		if ic == nil {
			t.Fatal("expected init-hermes")
		}
		if strings.Contains(ic.Args[0], "hermes config env-path") {
			t.Errorf("expected no dotenv section in init-hermes when nothing enabled, got:\n%s", ic.Args[0])
		}
	})
}

func TestOperatorDotEnvWebhook(t *testing.T) {
	ha := minimalHA()
	ha.Spec.Hermes = &agentsv1alpha1.Hermes{
		Config: &agentsv1alpha1.HermesConfig{
			Webhook: &agentsv1alpha1.HermesWebhook{Enabled: true},
		},
	}

	data, err := buildHermesConfigMapData(ha, resolvedConfigDocuments{})
	if err != nil {
		t.Fatal(err)
	}
	if data["WEBHOOK_ENABLED"] != testTrue {
		t.Errorf("expected WEBHOOK_ENABLED=true, got %q", data["WEBHOOK_ENABLED"])
	}
	if data["WEBHOOK_PORT"] != "8644" {
		t.Errorf("expected WEBHOOK_PORT=8644, got %q", data["WEBHOOK_PORT"])
	}

	sts := buildStatefulSet(ha, testConfigHash)
	ic := findInitContainer(sts, consolidatedInitContainerName)
	if ic == nil {
		t.Fatal("expected init-hermes init container")
	}
	if !hasVolumeMount(ic.VolumeMounts, "hermes-operator-dotenv-secret") {
		t.Error("expected hermes-operator-dotenv-secret volume mount on init-hermes")
	}

	// WEBHOOK_* env vars are injected via .env, not container env.
	c := findHermesContainer(sts)
	if c == nil {
		t.Fatal("expected hermes-agent container")
	}
	for _, name := range []string{"WEBHOOK_ENABLED", "WEBHOOK_PORT", "WEBHOOK_SECRET"} {
		if hasEnvVar(c.Env, name) {
			t.Errorf("expected %q absent from hermes-agent container Env (injected via .env)", name)
		}
	}
}

// TestSearXNGVolumeOwnership reproduces the ownership warnings reported in
// issue #98: kubelet creates emptyDir/PVC volumes as root:root, but the
// upstream searxng image entrypoint requires them to be owned by
// searxng:searxng (uid/gid 977) and cannot fix them itself because it runs
// non-root. The operator therefore prepares both volumes in a root-owned
// init container step.
func TestSearXNGVolumeOwnership(t *testing.T) {
	ha := minimalHA()
	ha.Spec.SearXNG = &agentsv1alpha1.SearXNG{Enabled: true}

	ic := findInitContainer(buildStatefulSet(ha, testConfigHash), "init-searxng-config")
	if ic == nil {
		t.Fatal("expected init-searxng-config init container")
	}

	t.Run("volumes are chowned to the searxng user", func(t *testing.T) {
		// The whole script must live in a single Args element: with Command
		// ["/bin/sh","-ec"], only the first operand after -c is the script —
		// additional Args elements become $0 and are never executed, which is
		// how the chown originally went missing (#98).
		if len(ic.Args) != 1 {
			t.Fatalf("init-searxng-config args = %q, want exactly one script element", ic.Args)
		}
		// Literal uid/gid on purpose: catches drift of the searxngUID/GID
		// constants away from the 977 the upstream image actually uses.
		want := `cp -r /bootstrap-searxng/. /etc/searxng/ && chown -R 977:977 /etc/searxng /var/cache/searxng`
		if !strings.Contains(ic.Args[0], want) {
			t.Errorf("init-searxng-config script =\n%s\nwant to contain:\n%s", ic.Args[0], want)
		}
	})

	t.Run("chown step runs as root", func(t *testing.T) {
		sc := ic.SecurityContext
		if sc == nil {
			t.Fatal("expected a security context on init-searxng-config")
		}
		if sc.RunAsUser == nil || *sc.RunAsUser != 0 {
			t.Error("chown step must run as root (uid 0)")
		}
		if sc.RunAsGroup == nil || *sc.RunAsGroup != 0 {
			t.Error("chown step must run as group 0")
		}
		if sc.RunAsNonRoot != nil && *sc.RunAsNonRoot {
			t.Error("chown step must not set runAsNonRoot")
		}
		if sc.AllowPrivilegeEscalation == nil || *sc.AllowPrivilegeEscalation {
			t.Error("chown step must not allow privilege escalation")
		}
		if sc.Capabilities == nil || !slices.Contains(sc.Capabilities.Drop, "ALL") {
			t.Error("chown step must drop all capabilities except CHOWN")
		}
		if len(sc.Capabilities.Add) != 1 || sc.Capabilities.Add[0] != "CHOWN" {
			t.Errorf("chown step capabilities.Add = %v, want [CHOWN]", sc.Capabilities.Add)
		}
	})

	t.Run("searxng runtime still runs as uid/gid 977 non-root", func(t *testing.T) {
		c := findContainer(buildStatefulSet(ha, testConfigHash), "searxng")
		if c == nil {
			t.Fatal("expected searxng container")
		}
		sc := c.SecurityContext
		if sc == nil || sc.RunAsUser == nil || *sc.RunAsUser != 977 {
			t.Error("searxng container must keep running as uid 977")
		}
		if sc == nil || sc.RunAsNonRoot == nil || !*sc.RunAsNonRoot {
			t.Error("searxng container must keep runAsNonRoot")
		}
	})

	t.Run("no chown step when searxng is disabled", func(t *testing.T) {
		ha := minimalHA()
		if ic := findInitContainer(buildStatefulSet(ha, testConfigHash), "init-searxng-config"); ic != nil {
			t.Error("expected no init-searxng-config init container when searxng is disabled")
		}
	})
}

func TestOperatorDotEnvSidecars(t *testing.T) {
	t.Run("searxng: SEARXNG_URL in ConfigMap and init-hermes", func(t *testing.T) {
		ha := minimalHA()
		ha.Spec.SearXNG = &agentsv1alpha1.SearXNG{Enabled: true}

		data, err := buildHermesConfigMapData(ha, resolvedConfigDocuments{})
		if err != nil {
			t.Fatal(err)
		}
		if data["SEARXNG_URL"] != "http://localhost:8080" {
			t.Errorf("expected SEARXNG_URL in ConfigMap, got %q", data["SEARXNG_URL"])
		}

		sts := buildStatefulSet(ha, testConfigHash)
		ic := findInitContainer(sts, consolidatedInitContainerName)
		if ic == nil {
			t.Fatal("expected init-hermes init container")
		}
		if !strings.Contains(ic.Args[0], "/hermes-operator-dotenv-configmap") {
			t.Errorf("expected operator configmap mount path in script, got:\n%s", ic.Args[0])
		}

		c := findHermesContainer(sts)
		if c == nil {
			t.Fatal("expected hermes-agent container")
		}
		if !hasEnvVar(c.Env, "SEARXNG_URL") {
			t.Error("expected SEARXNG_URL in hermes-agent container Env")
		}
	})

	t.Run("camofox: CAMOFOX_URL in ConfigMap and init-hermes", func(t *testing.T) {
		ha := minimalHA()
		ha.Spec.Camofox = &agentsv1alpha1.Camofox{Enabled: true}

		data, err := buildHermesConfigMapData(ha, resolvedConfigDocuments{})
		if err != nil {
			t.Fatal(err)
		}
		if data["CAMOFOX_URL"] != "http://localhost:9377" {
			t.Errorf("expected CAMOFOX_URL in ConfigMap, got %q", data["CAMOFOX_URL"])
		}

		sts := buildStatefulSet(ha, testConfigHash)
		ic := findInitContainer(sts, consolidatedInitContainerName)
		if ic == nil {
			t.Fatal("expected init-hermes init container")
		}
		if !strings.Contains(ic.Args[0], "/hermes-operator-dotenv-configmap") {
			t.Errorf("expected operator configmap mount path in script, got:\n%s", ic.Args[0])
		}

		c := findHermesContainer(sts)
		if c == nil {
			t.Fatal("expected hermes-agent container")
		}
		if !hasEnvVar(c.Env, "CAMOFOX_URL") {
			t.Error("expected CAMOFOX_URL in hermes-agent container Env")
		}
	})
}

func TestOperatorDotEnvMultiplex(t *testing.T) {
	t.Run("named profiles get SEARXNG_URL/CAMOFOX_URL, not API_SERVER_*", func(t *testing.T) {
		ha := minimalHA()
		ha.Spec.Hermes = &agentsv1alpha1.Hermes{
			Config: &agentsv1alpha1.HermesConfig{
				APIServer: &agentsv1alpha1.HermesAPIServer{Enabled: true},
			},
			Profiles: map[string]agentsv1alpha1.HermesProfile{
				"coder":  {},
				"writer": {},
			},
		}
		ha.Spec.SearXNG = &agentsv1alpha1.SearXNG{Enabled: true}
		ha.Spec.Camofox = &agentsv1alpha1.Camofox{Enabled: true}

		sts := buildStatefulSet(ha, testConfigHash)

		// Default profile: operator ConfigMap + Secret mounted.
		defIC := findInitContainer(sts, consolidatedInitContainerName)
		if defIC == nil {
			t.Fatal("expected init-hermes")
		}
		if !hasVolumeMount(defIC.VolumeMounts, "hermes-operator-dotenv-configmap") {
			t.Error("expected operator configmap mount on default init-hermes")
		}
		if !hasVolumeMount(defIC.VolumeMounts, "hermes-operator-dotenv-secret") {
			t.Error("expected operator secret mount on default init-hermes")
		}

		// Named profiles: only sidecar URLs, no API_SERVER_*/WEBHOOK_*.
		coderIC := findInitContainer(sts, "init-profile-coder")
		if coderIC == nil {
			t.Fatal("expected init-profile-coder")
		}
		coderScript := coderIC.Args[0]
		if !strings.Contains(coderScript, "hermes-operator-dotenv-profile-coder") {
			t.Errorf("expected operator dotenv mount for coder profile, got:\n%s", coderScript)
		}
		// Must NOT have API_SERVER or WEBHOOK in named-profile dotenv.
		if strings.Contains(coderScript, "API_SERVER") {
			t.Errorf("API_SERVER_* must not appear in named-profile dotenv, got:\n%s", coderScript)
		}
		if strings.Contains(coderScript, "WEBHOOK") {
			t.Errorf("WEBHOOK_* must not appear in named-profile dotenv, got:\n%s", coderScript)
		}
		// Each named profile gets its own .env block and its own container.
		if strings.Count(coderScript, `hermes config env-path -p "coder"`) != 1 {
			t.Errorf("expected one coder dotenv block, got:\n%s", coderScript)
		}
		writerIC := findInitContainer(sts, "init-profile-writer")
		if writerIC == nil {
			t.Fatal("expected init-profile-writer")
		}
		writerScript := writerIC.Args[0]
		if !strings.Contains(writerScript, "hermes-operator-dotenv-profile-writer") {
			t.Errorf("expected operator dotenv mount for writer profile, got:\n%s", writerScript)
		}
		if strings.Contains(writerScript, "hermes-operator-dotenv-profile-coder") {
			t.Errorf("writer container must not mount coder dotenv volume, got:\n%s", writerScript)
		}
	})
}

func TestOperatorDotEnvCollision(t *testing.T) {
	// User workspace.dotEnv keys override operator keys (user wins).
	ha := minimalHA()
	ha.Spec.Hermes = &agentsv1alpha1.Hermes{
		Config: &agentsv1alpha1.HermesConfig{
			APIServer: &agentsv1alpha1.HermesAPIServer{Enabled: true},
		},
		Workspace: &agentsv1alpha1.HermesWorkspace{
			DotEnv: &agentsv1alpha1.HermesDotEnv{
				SecretRef: &corev1.LocalObjectReference{Name: "my-env"},
			},
		},
	}
	sts := buildStatefulSet(ha, testConfigHash)
	ic := findInitContainer(sts, consolidatedInitContainerName)
	if ic == nil {
		t.Fatal("expected init-hermes")
	}
	script := ic.Args[0]
	// Operator mount path must come before user mount path.
	operatorIdx := strings.Index(script, "/hermes-operator-dotenv-configmap")
	userIdx := strings.Index(script, "/hermes-dotenv-secret")
	if operatorIdx < 0 || userIdx < 0 {
		t.Fatalf("expected both operator and user mount paths in script, got:\n%s", script)
	}
	if operatorIdx >= userIdx {
		t.Errorf("expected operator mount before user mount; operator at %d, user at %d in:\n%s",
			operatorIdx, userIdx, script)
	}
}

func TestOperatorDotEnvOrdering(t *testing.T) {
	ha := minimalHA()
	ha.Spec.Hermes = &agentsv1alpha1.Hermes{
		Config: &agentsv1alpha1.HermesConfig{
			APIServer: &agentsv1alpha1.HermesAPIServer{Enabled: true},
		},
	}
	sts := buildStatefulSet(ha, testConfigHash)
	ic := findInitContainer(sts, consolidatedInitContainerName)
	if ic == nil {
		t.Fatal("expected init-hermes")
	}
	script := ic.Args[0]
	// workspace step must run before the dotenv step inside the consolidated script.
	workspaceIdx := strings.Index(script, "profile.default.workspace.")
	dotenvIdx := strings.Index(script, `hermes config env-path -p "default"`)
	if workspaceIdx < 0 {
		t.Fatal("workspace step not found in init-hermes script")
	}
	if dotenvIdx < 0 {
		t.Fatal("dotenv step not found in init-hermes script")
	}
	if dotenvIdx <= workspaceIdx {
		t.Errorf("expected dotenv step after workspace step; workspace at %d, dotenv at %d", workspaceIdx, dotenvIdx)
	}
}

func TestOperatorDotEnvUserDotEnvAlone(t *testing.T) {
	// User dotEnv without any operator features still emits a dotenv section in init-hermes.
	ha := minimalHA()
	ha.Spec.Hermes = &agentsv1alpha1.Hermes{
		Workspace: &agentsv1alpha1.HermesWorkspace{
			DotEnv: &agentsv1alpha1.HermesDotEnv{
				SecretRef: &corev1.LocalObjectReference{Name: "my-env"},
			},
		},
	}
	sts := buildStatefulSet(ha, testConfigHash)
	if findInitContainer(sts, consolidatedInitContainerName) == nil {
		t.Error("expected dotenv section in init-hermes when user dotEnv is set")
	}
}

func TestDotEnvPluralConfigMapRefs(t *testing.T) {
	// Plural configMapRefs alone mounts N volumes in order.
	ha := minimalHA()
	ha.Spec.Hermes = &agentsv1alpha1.Hermes{
		Workspace: &agentsv1alpha1.HermesWorkspace{
			DotEnv: &agentsv1alpha1.HermesDotEnv{
				ConfigMapRefs: []corev1.LocalObjectReference{
					{Name: "shared-config"},
					{Name: "ai-config"},
				},
			},
		},
	}
	sts := buildStatefulSet(ha, testConfigHash)
	ic := findInitContainer(sts, consolidatedInitContainerName)
	if ic == nil {
		t.Fatal("expected init-hermes")
	}
	script := ic.Args[0]
	// Both plural configmap mount paths must be present, in order.
	idx0 := strings.Index(script, "/hermes-dotenv-configmap-0")
	idx1 := strings.Index(script, "/hermes-dotenv-configmap-1")
	if idx0 < 0 || idx1 < 0 {
		t.Fatalf("expected plural configmap mount paths in script, got:\n%s", script)
	}
	if idx0 >= idx1 {
		t.Errorf("expected configmap-0 before configmap-1; got %d, %d in:\n%s", idx0, idx1, script)
	}
	// Two plural configmap volumes expected.
	count := 0
	for _, v := range sts.Spec.Template.Spec.Volumes {
		if strings.HasPrefix(v.Name, "hermes-dotenv-configmap-") {
			count++
		}
	}
	if count != 2 {
		t.Errorf("expected 2 plural configmap volumes, got %d", count)
	}
}

func TestDotEnvPluralSecretRefs(t *testing.T) {
	// Plural secretRefs alone mounts N volumes in order.
	ha := minimalHA()
	ha.Spec.Hermes = &agentsv1alpha1.Hermes{
		Workspace: &agentsv1alpha1.HermesWorkspace{
			DotEnv: &agentsv1alpha1.HermesDotEnv{
				SecretRefs: []corev1.LocalObjectReference{
					{Name: "ai-providers"},
					{Name: "external-apis"},
				},
			},
		},
	}
	sts := buildStatefulSet(ha, testConfigHash)
	ic := findInitContainer(sts, consolidatedInitContainerName)
	if ic == nil {
		t.Fatal("expected init-hermes")
	}
	script := ic.Args[0]
	idx0 := strings.Index(script, "/hermes-dotenv-secret-0")
	idx1 := strings.Index(script, "/hermes-dotenv-secret-1")
	if idx0 < 0 || idx1 < 0 {
		t.Fatalf("expected plural secret mount paths in script, got:\n%s", script)
	}
	if idx0 >= idx1 {
		t.Errorf("expected secret-0 before secret-1; got %d, %d in:\n%s", idx0, idx1, script)
	}
	count := 0
	for _, v := range sts.Spec.Template.Spec.Volumes {
		if strings.HasPrefix(v.Name, "hermes-dotenv-secret-") {
			count++
		}
	}
	if count != 2 {
		t.Errorf("expected 2 plural secret volumes, got %d", count)
	}
}

func TestDotEnvMixedSingularAndPluralOrder(t *testing.T) {
	// Singular first, then plural in order; ConfigMaps before Secrets.
	ha := minimalHA()
	ha.Spec.Hermes = &agentsv1alpha1.Hermes{
		Workspace: &agentsv1alpha1.HermesWorkspace{
			DotEnv: &agentsv1alpha1.HermesDotEnv{
				ConfigMapRef:  &corev1.LocalObjectReference{Name: "legacy-config"},
				ConfigMapRefs: []corev1.LocalObjectReference{{Name: "shared-config"}},
				SecretRef:     &corev1.LocalObjectReference{Name: "legacy-secret"},
				SecretRefs:    []corev1.LocalObjectReference{{Name: "ai-providers"}},
			},
		},
	}
	sts := buildStatefulSet(ha, testConfigHash)
	ic := findInitContainer(sts, consolidatedInitContainerName)
	if ic == nil {
		t.Fatal("expected init-hermes")
	}
	script := ic.Args[0]
	// Expected order: configmap (singular) < configmap-0 < secret (singular) < secret-0.
	cmIdx := strings.Index(script, "/hermes-dotenv-configmap\"")
	cm0Idx := strings.Index(script, "/hermes-dotenv-configmap-0")
	secIdx := strings.Index(script, "/hermes-dotenv-secret\"")
	sec0Idx := strings.Index(script, "/hermes-dotenv-secret-0")
	for _, idx := range []int{cmIdx, cm0Idx, secIdx, sec0Idx} {
		if idx < 0 {
			t.Fatalf("missing expected mount path in script:\n%s", script)
		}
	}
	if cmIdx >= cm0Idx || cm0Idx >= secIdx || secIdx >= sec0Idx {
		t.Errorf("expected order cm < cm-0 < secret < secret-0; got %d %d %d %d in:\n%s",
			cmIdx, cm0Idx, secIdx, sec0Idx, script)
	}
	// Total 4 dotenv volumes (1 singular cm + 1 plural cm + 1 singular secret + 1 plural secret).
	count := 0
	for _, v := range sts.Spec.Template.Spec.Volumes {
		if strings.HasPrefix(v.Name, "hermes-dotenv-") {
			count++
		}
	}
	if count != 4 {
		t.Errorf("expected 4 dotenv volumes, got %d", count)
	}
}

func TestDotEnvPluralRefsPerProfile(t *testing.T) {
	// Per-profile plural refs mount with profile-scoped volume names.
	ha := minimalHA()
	ha.Spec.Hermes = &agentsv1alpha1.Hermes{
		Profiles: map[string]agentsv1alpha1.HermesProfile{
			"writer": {
				Workspace: &agentsv1alpha1.HermesWorkspace{
					DotEnv: &agentsv1alpha1.HermesDotEnv{
						ConfigMapRefs: []corev1.LocalObjectReference{{Name: "writer-config"}},
						SecretRefs:    []corev1.LocalObjectReference{{Name: "writer-secret"}},
					},
				},
			},
		},
	}
	sts := buildStatefulSet(ha, testConfigHash)
	ic := findInitContainer(sts, "init-profile-writer")
	if ic == nil {
		t.Fatal("expected init-profile-writer")
	}
	cmFound := false
	secFound := false
	for _, vm := range ic.VolumeMounts {
		if vm.Name == "hermes-dotenv-configmap-profile-writer-0" {
			cmFound = true
		}
		if vm.Name == "hermes-dotenv-secret-profile-writer-0" {
			secFound = true
		}
	}
	if !cmFound {
		t.Error("expected per-profile plural configmap volume mount")
	}
	if !secFound {
		t.Error("expected per-profile plural secret volume mount")
	}
}

func TestConsolidatedInitContainers(t *testing.T) {
	ha := minimalHA()
	ha.Spec.Hermes = &agentsv1alpha1.Hermes{
		Config: &agentsv1alpha1.HermesConfig{
			Raw: &apiextensionsv1.JSON{Raw: []byte(`{"foo":"bar"}`)},
		},
		Profiles: map[string]agentsv1alpha1.HermesProfile{
			"writer": {Clone: true},
			"coder":  {},
		},
	}
	ha.Spec.Hermes.Plugins = []agentsv1alpha1.HermesPlugin{{Identifier: "owner/repo"}}
	ha.Spec.Hermes.Skills = []agentsv1alpha1.HermesSkill{{Identifier: "gh-aw/system-commands"}}
	ha.Spec.Hermes.Packages = &agentsv1alpha1.HermesPackages{
		Pip: &agentsv1alpha1.HermesPipPackages{Install: []string{"requests"}},
		Npm: &agentsv1alpha1.HermesNpmPackages{Install: []string{"typescript"}},
	}
	ha.Spec.Hermes.Bundles = []agentsv1alpha1.HermesBundle{{Name: "ops"}}
	ha.Spec.Hermes.Crons = []agentsv1alpha1.HermesCron{{Name: "daily", Schedule: "1h"}}
	ha.Spec.Hermes.Workspace = &agentsv1alpha1.HermesWorkspace{
		DotEnv: &agentsv1alpha1.HermesDotEnv{
			SecretRef: &corev1.LocalObjectReference{Name: "my-env"},
		},
	}

	sts := buildStatefulSet(ha, testConfigHash)

	// Exactly one consolidated default-profile init container.
	var initHermes []*corev1.Container
	var profileInits []*corev1.Container
	for i := range sts.Spec.Template.Spec.InitContainers {
		c := &sts.Spec.Template.Spec.InitContainers[i]
		switch {
		case c.Name == consolidatedInitContainerName:
			initHermes = append(initHermes, c)
		case strings.HasPrefix(c.Name, "init-profile-"):
			profileInits = append(profileInits, c)
		}
	}
	if len(initHermes) != 1 {
		t.Fatalf("expected exactly one init-hermes, got %d", len(initHermes))
	}
	script := initHermes[0].Args[0]

	// All default-profile steps present in the consolidated script, in order.
	type step struct {
		marker string
		desc   string
	}
	steps := []step{
		{`cp "/bootstrap/profile.default.config.yaml"`, "config"},
		{"profile.default.workspace.", "workspace"},
		{`hermes config env-path -p "default"`, "dotenv"},
		{".python-packages", "python packages"},
		{".npm-packages", "npm packages"},
		{`hermes plugins install -p "default"`, "plugins"},
		{`hermes skills install -p "default"`, "skills"},
		{`hermes bundles create -p "default"`, "bundles"},
		{`hermes cron create -p "default"`, "crons"},
		{"profiles-manifest", "profiles cleanup"},
	}
	last := -1
	for _, st := range steps {
		idx := strings.Index(script, st.marker)
		if idx < 0 {
			t.Errorf("expected %s step in init-hermes script, got:\n%s", st.desc, script)
			continue
		}
		if idx < last {
			t.Errorf("expected %s step after previous step; %d < %d", st.desc, idx, last)
		}
		last = idx
	}

	// Steps run in subshells so early exit cannot abort later steps.
	if strings.Count(script, "\n(\n") != len(steps) {
		t.Errorf("expected %d subshells in init-hermes script, got %d:\n%s", len(steps), strings.Count(script, "\n(\n"), script)
	}

	// One init container per named profile, sorted alphabetically.
	if len(profileInits) != 2 {
		t.Fatalf("expected 2 per-profile init containers, got %d: %v", len(profileInits), profileInits)
	}
	if profileInits[0].Name != "init-profile-coder" || profileInits[1].Name != "init-profile-writer" {
		t.Errorf("expected sorted profile containers [init-profile-coder init-profile-writer], got [%s %s]",
			profileInits[0].Name, profileInits[1].Name)
	}

	// Each profile container creates its profile and configures it.
	coderScript := profileInits[0].Args[0]
	if !strings.Contains(coderScript, `hermes profile create "coder" --no-alias`) {
		t.Errorf("expected profile creation in coder container, got:\n%s", coderScript)
	}
	if strings.Contains(coderScript, "--clone") {
		t.Errorf("coder profile has clone=false, got:\n%s", coderScript)
	}
	if !strings.Contains(coderScript, `hermes config path -p "coder"`) {
		t.Errorf("expected workspace setup in coder container, got:\n%s", coderScript)
	}
	writerScript := profileInits[1].Args[0]
	if !strings.Contains(writerScript, `hermes profile create "writer" --no-alias --clone`) {
		t.Errorf("expected clone in writer container, got:\n%s", writerScript)
	}

	// Profile containers run after init-hermes.
	initHermesIdx := -1
	for i, c := range sts.Spec.Template.Spec.InitContainers {
		if c.Name == consolidatedInitContainerName {
			initHermesIdx = i
		}
	}
	for _, c := range profileInits {
		found := -1
		for i, cc := range sts.Spec.Template.Spec.InitContainers {
			if cc.Name == c.Name {
				found = i
			}
		}
		if found <= initHermesIdx {
			t.Errorf("expected %s after init-hermes (idx %d), got idx %d", c.Name, initHermesIdx, found)
		}
	}

	// User initScripts run after all operator-managed init containers.
	ha.Spec.Hermes.InitScripts = []agentsv1alpha1.HermesInitScript{{Name: "extra", Script: "echo hi"}}
	sts2 := buildStatefulSet(ha, testConfigHash)
	extraIdx, lastManaged := -1, -1
	for i, c := range sts2.Spec.Template.Spec.InitContainers {
		if c.Name == "extra" {
			extraIdx = i
		}
		if c.Name == consolidatedInitContainerName || strings.HasPrefix(c.Name, "init-profile-") {
			lastManaged = i
		}
	}
	if extraIdx < 0 || extraIdx <= lastManaged {
		t.Errorf("expected user initScript after managed init containers; extra idx %d, last managed idx %d", extraIdx, lastManaged)
	}
}

func TestConsolidatedInitContainersMinimal(t *testing.T) {
	// No profiles: only init-hermes, no per-profile containers; no dotenv section.
	ha := minimalHA()
	sts := buildStatefulSet(ha, testConfigHash)
	if findInitContainer(sts, consolidatedInitContainerName) == nil {
		t.Fatal("expected init-hermes")
	}
	for _, c := range sts.Spec.Template.Spec.InitContainers {
		if strings.HasPrefix(c.Name, "init-profile-") {
			t.Errorf("unexpected per-profile container without profiles: %s", c.Name)
		}
	}
	if strings.Contains(findInitContainer(sts, consolidatedInitContainerName).Args[0], "hermes config env-path") {
		t.Error("expected no dotenv section with empty spec")
	}
	if strings.Contains(findInitContainer(sts, consolidatedInitContainerName).Args[0], "profiles-manifest") {
		t.Error("expected no profiles cleanup without profiles")
	}
}

func TestBuildProfilesCleanupScript(t *testing.T) {
	got := buildProfilesCleanupScript(map[string]agentsv1alpha1.HermesProfile{
		"a": {},
		"b": {},
	})
	if !strings.Contains(got, `hermes profile delete "$pname" || true`) {
		t.Errorf("expected stale profile deletion, got:\n%s", got)
	}
	if strings.Contains(got, "profile create") {
		t.Errorf("cleanup script must not create profiles, got:\n%s", got)
	}
	// The operator's manifests are outside the profile directory, so deleting
	// the profile must remove them too.  Otherwise a re-added profile key finds
	// a `distribution` manifest with no profile.
	if !strings.Contains(got, `rm -rf "$HERMES_HOME/.hermes-agent-operator/profiles/$pname"`) {
		t.Errorf("expected the profile's manifests removed with it, got:\n%s", got)
	}
	if !strings.Contains(got, "a\nb") {
		t.Errorf("expected manifest with sorted names a\\nb, got:\n%s", got)
	}
}

func TestBuildProfileCreationScript(t *testing.T) {
	got := buildProfileCreationScript("coder", true)
	if !strings.Contains(got, `hermes profile create "coder" --no-alias --clone || true`) {
		t.Errorf("expected create with clone, got:\n%s", got)
	}
	// Dropping `distribution` from a profile drops the operator's state for it,
	// so adding the same distribution back installs it again.
	if !strings.Contains(got, `rm -rf "$HERMES_HOME/.hermes-agent-operator/profiles/coder/distribution" "$HERMES_HOME/.hermes-agent-operator/profiles/coder/src"`) {
		t.Errorf("expected the distribution manifest and checkout removed, got:\n%s", got)
	}
	got2 := buildProfileCreationScript("writer", false)
	if !strings.Contains(got2, `hermes profile create "writer" --no-alias || true`) {
		t.Errorf("expected create without clone, got:\n%s", got2)
	}
	if strings.Contains(got2, "--clone") {
		t.Errorf("unexpected --clone flag, got:\n%s", got2)
	}
}

func TestBuildStatefulSetInitContainerTerminationMessagePolicy(t *testing.T) {
	ha := minimalHA()
	ha.Spec.Hermes = &agentsv1alpha1.Hermes{
		InitChownData: true,
		InitScripts:   []agentsv1alpha1.HermesInitScript{{Name: "init-extra", Script: "true"}},
		Profiles:      map[string]agentsv1alpha1.HermesProfile{"coder": {}},
	}
	ha.Spec.SearXNG = &agentsv1alpha1.SearXNG{Enabled: true}
	// A user-supplied init container is passed through untouched.
	ha.Spec.InitContainers = []corev1.Container{{Name: "init-user", Image: "busybox"}}

	sts := buildStatefulSet(ha, testConfigHash)
	var managed int
	for _, c := range sts.Spec.Template.Spec.InitContainers {
		if c.Name == "init-user" {
			if c.TerminationMessagePolicy != "" {
				t.Errorf("user init container must be passed through untouched, got policy %q", c.TerminationMessagePolicy)
			}
			continue
		}
		managed++
		if c.TerminationMessagePolicy != corev1.TerminationMessageFallbackToLogsOnError {
			t.Errorf("init container %s: policy = %q, want FallbackToLogsOnError", c.Name, c.TerminationMessagePolicy)
		}
	}
	// init-chown-data, init-hermes, init-profile-coder, init-extra and
	// init-searxng-config.
	if managed < 5 {
		t.Errorf("expected at least 5 managed init containers, got %d", managed)
	}
}

func TestBuildProfileDistributionScript(t *testing.T) {
	const source = "github.com/you/research-bot"

	t.Run("unpinned installs from the URL and re-pulls on start", func(t *testing.T) {
		got := buildProfileDistributionScript("coder", &agentsv1alpha1.HermesProfileDistribution{Source: source}, "")
		if !strings.Contains(got, `SOURCE="github.com/you/research-bot"`) {
			t.Errorf("expected the source, got:\n%s", got)
		}
		if !strings.Contains(got, `hermes profile install "$SOURCE" --name "coder" --force --yes`) {
			t.Errorf("expected an install from the URL, got:\n%s", got)
		}
		if !strings.Contains(got, `hermes profile update "coder" --yes`) {
			t.Errorf("expected an update on start, got:\n%s", got)
		}
		// The profile comes from the `distribution`, never from profile create.
		if strings.Contains(got, "hermes profile create") {
			t.Errorf("a distribution profile must not be created separately, got:\n%s", got)
		}
		// The operator installs an unpinned `distribution` from its URL.  Thus
		// the profile records the URL, and a manual update can pull it again.
		if strings.Contains(got, "git clone") {
			t.Errorf("an unpinned distribution must not be cloned by the operator, got:\n%s", got)
		}
		// A failed update keeps the installed revision rather than stopping
		// an agent that already works.
		if !strings.Contains(got, `|| echo "Profile coder: hermes profile update failed, so the profile stays at its installed revision.`) {
			t.Errorf("expected a failed update to be reported and skipped, got:\n%s", got)
		}
		// A checkout from an earlier pin is no longer used.
		if !strings.Contains(got, "  rm -rf \"$STAGED\"\n  printf '%s' \"$DESIRED\"") {
			t.Errorf("expected the staged checkout removed after an unpinned install, got:\n%s", got)
		}
	})

	t.Run("the installed profile must still exist to skip the install", func(t *testing.T) {
		got := buildProfileDistributionScript("coder", &agentsv1alpha1.HermesProfileDistribution{Source: source}, "")
		if !strings.Contains(got, `PROFILE_DIR="$HERMES_HOME/profiles/coder"`) {
			t.Errorf("expected the profile directory, got:\n%s", got)
		}
		if !strings.Contains(got, `[ "$(cat "$MANIFEST")" = "$DESIRED" ] && [ -f "$PROFILE_DIR/distribution.yaml" ]`) {
			t.Errorf("expected the manifest match to check the profile, got:\n%s", got)
		}
	})

	t.Run("updateOnStart false leaves the installed revision alone", func(t *testing.T) {
		no := false
		got := buildProfileDistributionScript("coder", &agentsv1alpha1.HermesProfileDistribution{
			Source: source, UpdateOnStart: &no,
		}, "")
		if strings.Contains(got, "hermes profile update") {
			t.Errorf("expected no update, got:\n%s", got)
		}
		if !strings.Contains(got, "left at its installed revision") {
			t.Errorf("expected the no-op branch to say so, got:\n%s", got)
		}
	})

	t.Run("forceConfig reaches the update", func(t *testing.T) {
		got := buildProfileDistributionScript("coder", &agentsv1alpha1.HermesProfileDistribution{
			Source: source, ForceConfig: true,
		}, "")
		if !strings.Contains(got, `hermes profile update "coder" --force-config --yes`) {
			t.Errorf("expected --force-config, got:\n%s", got)
		}
	})

	t.Run("pinned clones at the ref and installs from the checkout", func(t *testing.T) {
		got := buildProfileDistributionScript("coder", &agentsv1alpha1.HermesProfileDistribution{
			Source: source, Ref: "v1.2.0",
		}, "")
		if !strings.Contains(got, `REF="v1.2.0"`) {
			t.Errorf("expected the ref, got:\n%s", got)
		}
		if !strings.Contains(got, `git clone --depth 1 --branch "$REF" "$CLONE_URL" "$STAGED"`) {
			t.Errorf("expected a clone at the ref, got:\n%s", got)
		}
		// git would read the shorthand the CLI accepts as a local path.
		if !strings.Contains(got, `CLONE_URL="https://github.com/you/research-bot"`) {
			t.Errorf("expected the shorthand expanded for the clone, got:\n%s", got)
		}
		// The manifest still records what the custom resource declared.
		if !strings.Contains(got, `SOURCE="github.com/you/research-bot"`) {
			t.Errorf("expected the declared source recorded, got:\n%s", got)
		}
		// A commit SHA is not a valid --branch argument.
		if !strings.Contains(got, `git -C "$STAGED" fetch -q --depth 1 origin "$REF"`) {
			t.Errorf("expected the explicit fetch fallback, got:\n%s", got)
		}
		// The checkout is not a git repository once installed, matching what
		// the Hermes CLI does with its own clones.
		if !strings.Contains(got, `rm -rf "$STAGED/.git"`) {
			t.Errorf("expected .git to be removed, got:\n%s", got)
		}
		// A branch resolves once, so the log keeps the commit it resolved to.
		revParse := strings.Index(got, `git -C "$STAGED" rev-parse HEAD`)
		if revParse < 0 || revParse > strings.Index(got, `rm -rf "$STAGED/.git"`) {
			t.Errorf("expected the resolved commit logged before .git is removed, got:\n%s", got)
		}
		// git must fail on a missing credential instead of asking for one.
		if !strings.Contains(got, "export GIT_TERMINAL_PROMPT=0") {
			t.Errorf("expected git prompts disabled, got:\n%s", got)
		}
		if !strings.Contains(got, `hermes profile install "$STAGED" --name "coder" --force --yes`) {
			t.Errorf("expected an install from the checkout, got:\n%s", got)
		}
		// The pin is the version: a restart must not pull a newer revision.
		if strings.Contains(got, "hermes profile update") {
			t.Errorf("a pinned distribution must not update on start, got:\n%s", got)
		}
	})

	t.Run("the manifest records the source and the ref", func(t *testing.T) {
		got := buildProfileDistributionScript("coder", &agentsv1alpha1.HermesProfileDistribution{
			Source: source, Ref: "v1.2.0",
		}, "")
		if !strings.Contains(got, "DESIRED=\"$SOURCE\t$REF\"") {
			t.Errorf("expected source and ref in the manifest value, got:\n%s", got)
		}
		if !strings.Contains(got, `printf '%s' "$DESIRED" > "$MANIFEST"`) {
			t.Errorf("expected the manifest to be written after installing, got:\n%s", got)
		}
		if !strings.Contains(got, "profiles/coder/distribution") {
			t.Errorf("expected a per-profile manifest path, got:\n%s", got)
		}
	})

	t.Run("a failure is reported as a termination message", func(t *testing.T) {
		got := buildProfileDistributionScript("coder", &agentsv1alpha1.HermesProfileDistribution{Source: source}, "")
		if !strings.Contains(got, "> /dev/termination-log") {
			t.Errorf("expected a curated termination message, got:\n%s", got)
		}
		if !strings.Contains(got, "exit 1") {
			t.Errorf("expected a failure to abort the init container, got:\n%s", got)
		}
	})
}

func TestGitCloneURL(t *testing.T) {
	tests := []struct {
		source string
		want   string
	}{
		// The one shorthand the Hermes CLI accepts, which git would otherwise
		// read as a local path.
		{"github.com/you/research-bot", "https://github.com/you/research-bot"},
		{"github.com/you/research-bot/", "https://github.com/you/research-bot"},
		// Other sources do not change, including a source without a scheme on
		// another host.  The CLI reads that as a local directory.  If the
		// operator expanded it, a pinned profile could install from a location
		// that an unpinned profile cannot.
		{"gitlab.com/team/research-bot", "gitlab.com/team/research-bot"},
		{"https://github.com/you/research-bot", "https://github.com/you/research-bot"},
		{"https://git.example.com/team/bot.git", "https://git.example.com/team/bot.git"},
		{"git@github.com:you/research-bot.git", "git@github.com:you/research-bot.git"},
		{"/srv/profiles/research-bot", "/srv/profiles/research-bot"},
	}
	for _, tt := range tests {
		t.Run(tt.source, func(t *testing.T) {
			if got := gitCloneURL(tt.source); got != tt.want {
				t.Errorf("gitCloneURL(%q) = %q, want %q", tt.source, got, tt.want)
			}
		})
	}
}

func TestBuildProfileDistributionScriptGitCredentials(t *testing.T) {
	dist := &agentsv1alpha1.HermesProfileDistribution{Source: "https://git.example.com/team/bot.git"}

	t.Run("no credentials leaves git configuration alone", func(t *testing.T) {
		got := buildProfileDistributionScript("coder", dist, "")
		if strings.Contains(got, "GIT_CONFIG") {
			t.Errorf("expected no git configuration without credentials, got:\n%s", got)
		}
	})

	t.Run("credentials are configured through the environment", func(t *testing.T) {
		got := buildProfileDistributionScript("coder", dist, "/hermes-git-credentials-coder/.git-credentials")
		if !strings.Contains(got, `CREDENTIALS="/hermes-git-credentials-coder/.git-credentials"`) {
			t.Errorf("expected the mounted credential store, got:\n%s", got)
		}
		if !strings.Contains(got, "export GIT_CONFIG_KEY_0=credential.helper") ||
			!strings.Contains(got, `export GIT_CONFIG_VALUE_0="store --file=$CREDENTIALS"`) {
			t.Errorf("expected the credential helper in the environment, got:\n%s", got)
		}
		// A missing key is named, rather than surfacing as an auth failure.
		if !strings.Contains(got, `has no key .git-credentials`) {
			t.Errorf("expected a pre-flight check naming the key, got:\n%s", got)
		}
		// The token must not reach a config file on a writable volume.
		if strings.Contains(got, "git config") || strings.Contains(got, ".gitconfig") {
			t.Errorf("expected no git config file to be written, got:\n%s", got)
		}
	})
}

func TestBuildStatefulSetProfileDistributionGitCredentials(t *testing.T) {
	ha := minimalHA()
	ha.Spec.Hermes = &agentsv1alpha1.Hermes{
		Profiles: map[string]agentsv1alpha1.HermesProfile{
			"researcher": {Distribution: &agentsv1alpha1.HermesProfileDistribution{
				Source: "https://git.example.com/team/bot.git",
				GitCredentials: &agentsv1alpha1.HermesGitCredentials{
					SecretRef: corev1.LocalObjectReference{Name: "bot-git-credentials"},
				},
			}},
		},
	}

	sts := buildStatefulSet(ha, testConfigHash)
	const volumeName = "hermes-git-credentials-researcher"

	var volume *corev1.Volume
	for i, v := range sts.Spec.Template.Spec.Volumes {
		if v.Name == volumeName {
			volume = &sts.Spec.Template.Spec.Volumes[i]
		}
	}
	if volume == nil {
		t.Fatalf("expected a %s volume", volumeName)
	}
	if volume.Secret == nil || volume.Secret.SecretName != "bot-git-credentials" {
		t.Errorf("expected the credentials Secret as the volume source, got %+v", volume.VolumeSource)
	}

	// Mounted read-only into that profile's init container...
	init := findInitContainer(sts, "init-profile-researcher")
	var mounted bool
	for _, m := range init.VolumeMounts {
		if m.Name == volumeName {
			mounted = true
			if !m.ReadOnly {
				t.Error("expected the credentials mount to be read-only")
			}
			if !strings.Contains(init.Args[0], m.MountPath+"/.git-credentials") {
				t.Errorf("expected the script to use %s, got:\n%s", m.MountPath, init.Args[0])
			}
		}
	}
	if !mounted {
		t.Error("expected the credentials mounted into init-profile-researcher")
	}

	// ...and nowhere else, so the running agent cannot read the token.
	for _, c := range sts.Spec.Template.Spec.Containers {
		for _, m := range c.VolumeMounts {
			if m.Name == volumeName {
				t.Errorf("credentials must not be mounted into container %s", c.Name)
			}
		}
	}
	for _, c := range sts.Spec.Template.Spec.InitContainers {
		if c.Name == "init-profile-researcher" {
			continue
		}
		for _, m := range c.VolumeMounts {
			if m.Name == volumeName {
				t.Errorf("credentials must not be mounted into init container %s", c.Name)
			}
		}
	}
}

func TestBuildStatefulSetProfileDistribution(t *testing.T) {
	ha := minimalHA()
	ha.Spec.Hermes = &agentsv1alpha1.Hermes{
		Profiles: map[string]agentsv1alpha1.HermesProfile{
			"researcher": {
				Distribution: &agentsv1alpha1.HermesProfileDistribution{
					Source: "github.com/you/research-bot",
					Ref:    "v1.2.0",
				},
				Config: &agentsv1alpha1.HermesProfileConfig{Raw: imageJSON(`{"model":"opus"}`)},
				Skills: []agentsv1alpha1.HermesSkill{{Identifier: "owner/skills/review", Name: "review"}},
			},
			"plain": {},
		},
	}

	sts := buildStatefulSet(ha, testConfigHash)

	script := findInitContainer(sts, "init-profile-researcher").Args[0]
	if !strings.Contains(script, `hermes profile install "$STAGED" --name "researcher"`) {
		t.Errorf("expected the distribution install, got:\n%s", script)
	}
	if strings.Contains(script, "hermes profile create") {
		t.Errorf("expected no profile creation for a distribution, got:\n%s", script)
	}
	// The operator's own declarations are layered on top of the `distribution`,
	// so they run after the install.
	installIdx := strings.Index(script, "hermes profile install")
	configIdx := strings.Index(script, "/bootstrap/profile.researcher.config.yaml")
	skillsIdx := strings.Index(script, "hermes skills install")
	if configIdx < installIdx || skillsIdx < installIdx {
		t.Errorf("expected config (%d) and skills (%d) after the install (%d):\n%s",
			configIdx, skillsIdx, installIdx, script)
	}

	// A profile without a `distribution` is created as before.
	plain := findInitContainer(sts, "init-profile-plain").Args[0]
	if !strings.Contains(plain, `hermes profile create "plain" --no-alias`) {
		t.Errorf("expected plain profile creation, got:\n%s", plain)
	}
}

func TestCombineInitSteps(t *testing.T) {
	got := combineInitSteps("step one", "step two")
	if !strings.Contains(got, "==> Step 1/2") || !strings.Contains(got, "==> Step 2/2") {
		t.Errorf("expected step banners, got:\n%s", got)
	}
	if !strings.Contains(got, "step one") || !strings.Contains(got, "step two") {
		t.Errorf("expected both steps in script, got:\n%s", got)
	}
	// Each step wrapped in a subshell so `exit 0` in one step cannot abort the next.
	if strings.Count(got, "\n(\n") != 2 {
		t.Errorf("expected 2 subshells, got:\n%s", got)
	}
}
