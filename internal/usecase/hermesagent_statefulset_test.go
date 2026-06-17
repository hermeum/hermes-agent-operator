package usecase

import (
	"strings"
	"testing"

	agentsv1alpha1 "hermeum/hermes-agent-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func minimalHA() *agentsv1alpha1.HermesAgent {
	return &agentsv1alpha1.HermesAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec:       agentsv1alpha1.HermesAgentSpec{},
	}
}

func TestDesiredSpecHash(t *testing.T) {
	t.Run("deterministic for same spec", func(t *testing.T) {
		ha := minimalHA()
		h1 := desiredSpecHash(buildStatefulSet(ha))
		h2 := desiredSpecHash(buildStatefulSet(ha))
		if h1 != h2 {
			t.Errorf("hash not stable: %q vs %q", h1, h2)
		}
	})

	t.Run("returns 16-char hex string", func(t *testing.T) {
		h := desiredSpecHash(buildStatefulSet(minimalHA()))
		if len(h) != 16 {
			t.Errorf("expected 16-char hash, got %d chars: %q", len(h), h)
		}
		for _, c := range h {
			if !strings.ContainsRune("0123456789abcdef", c) {
				t.Errorf("non-hex character %q in hash %q", c, h)
			}
		}
	})

	t.Run("changes when replicas change (suspend toggles)", func(t *testing.T) {
		ha := minimalHA()
		suspend := true
		hRun := desiredSpecHash(buildStatefulSet(ha))
		ha.Spec.Suspend = &suspend
		hSuspend := desiredSpecHash(buildStatefulSet(ha))
		if hRun == hSuspend {
			t.Error("expected different hash when suspended (replicas 0 vs 1)")
		}
	})

	t.Run("changes when container image changes", func(t *testing.T) {
		ha := minimalHA()
		h1 := desiredSpecHash(buildStatefulSet(ha))

		ha.Spec.Hermes = &agentsv1alpha1.Hermes{
			Image: &agentsv1alpha1.HermesImage{Tag: "v2.0.0"},
		}
		h2 := desiredSpecHash(buildStatefulSet(ha))
		if h1 == h2 {
			t.Error("expected different hash when image tag changes")
		}
	})

	t.Run("changes when env var added", func(t *testing.T) {
		ha := minimalHA()
		h1 := desiredSpecHash(buildStatefulSet(ha))

		ha.Spec.Hermes = &agentsv1alpha1.Hermes{
			Env: []corev1.EnvVar{{Name: "CUSTOM", Value: "value"}},
		}
		h2 := desiredSpecHash(buildStatefulSet(ha))
		if h1 == h2 {
			t.Error("expected different hash when env var added")
		}
	})

	t.Run("changes when PVC added", func(t *testing.T) {
		ha := minimalHA()
		h1 := desiredSpecHash(buildStatefulSet(ha))

		size := resource.MustParse("10Gi")
		ha.Spec.Hermes = &agentsv1alpha1.Hermes{
			Storage: &agentsv1alpha1.HermesStorage{
				Persistence: &agentsv1alpha1.HermesPersistence{
					Enabled: true,
					Size:    &size,
				},
			},
		}
		h2 := desiredSpecHash(buildStatefulSet(ha))
		if h1 == h2 {
			t.Error("expected different hash when persistence PVC enabled")
		}
	})

	t.Run("stable when only ObjectMeta labels differ", func(t *testing.T) {
		ha := minimalHA()
		sts := buildStatefulSet(ha)
		h1 := desiredSpecHash(sts)

		// simulate kubernetes adding labels to ObjectMeta without touching Spec
		sts.Labels["extra-label"] = "injected-by-k8s"
		h2 := desiredSpecHash(sts)
		if h1 != h2 {
			t.Error("hash should not change when only ObjectMeta labels differ")
		}
	})
}

func TestDesiredSpecHashAnnotation(t *testing.T) {
	t.Run("reconcileStatefulSet stamps hash annotation on desired StatefulSet", func(t *testing.T) {
		ha := minimalHA()
		desired := buildStatefulSet(ha)
		hash := desiredSpecHash(desired)

		// simulate what reconcileStatefulSet does before comparing
		if desired.Annotations == nil {
			desired.Annotations = map[string]string{}
		}
		desired.Annotations[domain+"/desired-spec-hash"] = hash

		got := desired.Annotations[domain+"/desired-spec-hash"]
		if got != hash {
			t.Errorf("annotation value %q does not match computed hash %q", got, hash)
		}
	})

	t.Run("existing StatefulSet with matching hash is not updated", func(t *testing.T) {
		ha := minimalHA()
		desired := buildStatefulSet(ha)
		hash := desiredSpecHash(desired)

		// simulate an existing StatefulSet that already has the correct hash
		existing := desired.DeepCopy()
		existing.Annotations = map[string]string{domain + "/desired-spec-hash": hash}

		if existing.Annotations[domain+"/desired-spec-hash"] != hash {
			t.Error("should not update when annotation matches desired hash")
		}
	})

	t.Run("existing StatefulSet with stale hash triggers update", func(t *testing.T) {
		ha := minimalHA()
		desired := buildStatefulSet(ha)
		hash := desiredSpecHash(desired)

		// simulate an existing StatefulSet with an outdated hash (e.g. from before upgrade)
		existing := desired.DeepCopy()
		existing.Annotations = map[string]string{domain + "/desired-spec-hash": "stale0000000000"}

		if existing.Annotations[domain+"/desired-spec-hash"] == hash {
			t.Error("stale hash should not match desired hash")
		}
	})

	t.Run("existing StatefulSet with no annotation triggers update", func(t *testing.T) {
		ha := minimalHA()
		desired := buildStatefulSet(ha)
		hash := desiredSpecHash(desired)

		// simulate pre-migration StatefulSet (no annotation)
		existing := desired.DeepCopy()
		existing.Annotations = nil

		if existing.Annotations[domain+"/desired-spec-hash"] == hash {
			t.Error("missing annotation should not match desired hash")
		}
	})
}

func ptrBool(b bool) *bool { return &b }
func ptrInt(i int) *int    { return &i }

func TestBuildPluginsScript(t *testing.T) {

	t.Run("default enable", func(t *testing.T) {
		got := buildPluginsScript([]agentsv1alpha1.HermesPlugin{
			{Identifier: "anpicasso/hermes-plugin-chrome-profiles"},
		})

		wantCmd := `hermes plugins install --force --enable "anpicasso/hermes-plugin-chrome-profiles"`
		if !strings.Contains(got, wantCmd) {
			t.Errorf("expected install command %q in script, got:\n%s", wantCmd, got)
		}

		wantCase := `"hermes-plugin-chrome-profiles"`
		if !strings.Contains(got, wantCase+")") {
			t.Errorf("expected case pattern %q in script, got:\n%s", wantCase, got)
		}
	})

	t.Run("explicit no-enable", func(t *testing.T) {
		got := buildPluginsScript([]agentsv1alpha1.HermesPlugin{
			{Identifier: "https://github.com/owner/hermes-plugin-foo.git", Enable: ptrBool(false)},
		})

		wantCmd := `hermes plugins install --force --no-enable "https://github.com/owner/hermes-plugin-foo.git"`
		if !strings.Contains(got, wantCmd) {
			t.Errorf("expected install command %q in script, got:\n%s", wantCmd, got)
		}
	})

	t.Run("explicit enable true", func(t *testing.T) {
		got := buildPluginsScript([]agentsv1alpha1.HermesPlugin{
			{Identifier: "owner/repo", Enable: ptrBool(true)},
		})

		wantCmd := `hermes plugins install --force --enable "owner/repo"`
		if !strings.Contains(got, wantCmd) {
			t.Errorf("expected install command %q in script, got:\n%s", wantCmd, got)
		}
	})

	t.Run("remove command uses bare hermes not absolute path", func(t *testing.T) {
		got := buildPluginsScript([]agentsv1alpha1.HermesPlugin{
			{Identifier: "owner/hermes-plugin-a"},
		})
		if strings.Contains(got, "/hermes ") {
			t.Errorf("expected bare 'hermes' command, not absolute '/hermes' path, got:\n%s", got)
		}
		if !strings.Contains(got, `hermes plugins remove "$name"`) {
			t.Errorf("expected plugin remove command in script, got:\n%s", got)
		}
	})

	t.Run("multiple plugins build case pattern and manifest", func(t *testing.T) {
		got := buildPluginsScript([]agentsv1alpha1.HermesPlugin{
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

		if !strings.Contains(got, `hermes plugins install --force --enable "owner/hermes-plugin-a"`) {
			t.Errorf("missing install command for plugin a in:\n%s", got)
		}
		if !strings.Contains(got, `hermes plugins install --force --no-enable "owner/hermes-plugin-b"`) {
			t.Errorf("missing install command for plugin b in:\n%s", got)
		}
	})
}

func TestBuildSkillsScript(t *testing.T) {

	t.Run("identifier only", func(t *testing.T) {
		got := buildSkillsScript([]agentsv1alpha1.HermesSkill{
			{Identifier: "openai/skills/skill-creator"},
		})

		wantCmd := "hermes skills install --yes openai/skills/skill-creator"
		if !strings.Contains(got, wantCmd) {
			t.Errorf("expected %q in script, got:\n%s", wantCmd, got)
		}

		// name derived from identifier: skill-creator
		if !strings.Contains(got, `"skill-creator"`) {
			t.Errorf("expected name %q in case pattern, got:\n%s", "skill-creator", got)
		}
	})

	t.Run("with all options", func(t *testing.T) {
		got := buildSkillsScript([]agentsv1alpha1.HermesSkill{
			{
				Identifier: "https://example.com/SKILL.md",
				Category:   "writing",
				Name:       "my-skill",
				Force:      true,
			},
		})

		wantCmd := "hermes skills install --yes --category writing --name my-skill --force https://example.com/SKILL.md"
		if !strings.Contains(got, wantCmd) {
			t.Errorf("expected %q in script, got:\n%s", wantCmd, got)
		}

		if !strings.Contains(got, `"my-skill"`) {
			t.Errorf("expected explicit name in case pattern, got:\n%s", got)
		}
	})

	t.Run("uninstall command present", func(t *testing.T) {
		got := buildSkillsScript([]agentsv1alpha1.HermesSkill{
			{Identifier: "openai/skills/s1"},
		})

		if !strings.Contains(got, `hermes skills uninstall "$name" || true`) {
			t.Errorf("expected uninstall command in script, got:\n%s", got)
		}
	})

	t.Run("multiple skills manifest order", func(t *testing.T) {
		got := buildSkillsScript([]agentsv1alpha1.HermesSkill{
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
}

func TestBuildBundlesScript(t *testing.T) {

	t.Run("minimal", func(t *testing.T) {
		got := buildBundlesScript([]agentsv1alpha1.HermesBundle{
			{Name: "finance"},
		})

		wantCmd := `hermes bundles create "finance"`
		if !strings.Contains(got, wantCmd) {
			t.Errorf("expected %q in script, got:\n%s", wantCmd, got)
		}
	})

	t.Run("all options", func(t *testing.T) {
		got := buildBundlesScript([]agentsv1alpha1.HermesBundle{
			{
				Name:        "finance",
				Skills:      []string{"a", "b"},
				Description: "d",
				Instruction: "i",
				Force:       true,
			},
		})

		wantCmd := `hermes bundles create --skill "a" --skill "b" --description "d" --instruction "i" --force "finance"`
		if !strings.Contains(got, wantCmd) {
			t.Errorf("expected:\n%s\n\nin script:\n%s", wantCmd, got)
		}
	})

	t.Run("delete command present", func(t *testing.T) {
		got := buildBundlesScript([]agentsv1alpha1.HermesBundle{
			{Name: "finance"},
		})
		if !strings.Contains(got, `hermes bundles delete "$name" || true`) {
			t.Errorf("expected delete command in script, got:\n%s", got)
		}
	})

	t.Run("multiple bundles manifest order", func(t *testing.T) {
		got := buildBundlesScript([]agentsv1alpha1.HermesBundle{
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
}

func TestBuildCronsScript(t *testing.T) {

	t.Run("minimal", func(t *testing.T) {
		got := buildCronsScript([]agentsv1alpha1.HermesCron{
			{Name: "daily", Schedule: "0 9 * * *"},
		})

		wantCmd := `hermes cron create --name "daily" "0 9 * * *"`
		if !strings.Contains(got, wantCmd) {
			t.Errorf("expected %q in script, got:\n%s", wantCmd, got)
		}
	})

	t.Run("with prompt", func(t *testing.T) {
		got := buildCronsScript([]agentsv1alpha1.HermesCron{
			{Name: "p", Schedule: "30m", Prompt: "say hi"},
		})

		wantCmd := `hermes cron create --name "p" "30m" "say hi"`
		if !strings.Contains(got, wantCmd) {
			t.Errorf("expected %q in script, got:\n%s", wantCmd, got)
		}
	})

	t.Run("all options", func(t *testing.T) {
		got := buildCronsScript([]agentsv1alpha1.HermesCron{
			{
				Name:     "full",
				Schedule: "every 2h",
				Prompt:   "do thing",
				Deliver:  "telegram",
				Repeat:   ptrInt(3),
				Skills:   []string{"alpha", "beta"},
				Script:   "myscript.sh",
				NoAgent:  true,
				Workdir:  "/opt/data",
				Profile:  "default",
			},
		})

		wantCmd := `hermes cron create --name "full" --deliver "telegram" --repeat 3 --skill "alpha" --skill "beta" --script "myscript.sh" --no-agent --workdir "/opt/data" --profile "default" "every 2h" "do thing"`
		if !strings.Contains(got, wantCmd) {
			t.Errorf("expected:\n%s\n\nin script:\n%s", wantCmd, got)
		}
	})

	t.Run("remove uses hermes cron remove", func(t *testing.T) {
		got := buildCronsScript([]agentsv1alpha1.HermesCron{
			{Name: "j", Schedule: "1h"},
		})
		if !strings.Contains(got, `hermes cron remove "$id" || true`) {
			t.Errorf("expected remove command in script, got:\n%s", got)
		}
	})

	t.Run("manifest contains names", func(t *testing.T) {
		got := buildCronsScript([]agentsv1alpha1.HermesCron{
			{Name: "a", Schedule: "1h"},
			{Name: "b", Schedule: "2h"},
		})
		if !strings.Contains(got, "a\nb") {
			t.Errorf("expected manifest with names a\\nb, got:\n%s", got)
		}
	})
}
