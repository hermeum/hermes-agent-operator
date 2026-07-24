package usecase

import (
	"strings"
	"testing"

	agentsv1alpha1 "hermeum/hermes-agent-operator/api/v1alpha1"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const testTrue = "true"

func minimalHA() *agentsv1alpha1.HermesAgent {
	return &agentsv1alpha1.HermesAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec:       agentsv1alpha1.HermesAgentSpec{},
	}
}

func TestDesiredSpecHash(t *testing.T) {
	t.Run("stable for identical spec", func(t *testing.T) {
		ha := minimalHA()
		h1 := desiredSpecHash(buildStatefulSet(ha))
		h2 := desiredSpecHash(buildStatefulSet(ha))
		if h1 != h2 {
			t.Error("hash must be deterministic")
		}
	})

	t.Run("changes when replicas change", func(t *testing.T) {
		ha := minimalHA()
		h1 := desiredSpecHash(buildStatefulSet(ha))
		suspend := true
		ha.Spec.Suspend = &suspend
		if desiredSpecHash(buildStatefulSet(ha)) == h1 {
			t.Error("expected different hash when replicas change")
		}
	})

	t.Run("changes when pod template changes", func(t *testing.T) {
		ha := minimalHA()
		h1 := desiredSpecHash(buildStatefulSet(ha))
		ha.Spec.Hermes = &agentsv1alpha1.Hermes{Image: &agentsv1alpha1.HermesImage{Tag: "v2"}}
		if desiredSpecHash(buildStatefulSet(ha)) == h1 {
			t.Error("expected different hash when pod template changes")
		}
	})

	t.Run("changes when volume claim templates change", func(t *testing.T) {
		ha := minimalHA()
		h1 := desiredSpecHash(buildStatefulSet(ha))
		size := resource.MustParse("10Gi")
		ha.Spec.Hermes = &agentsv1alpha1.Hermes{Storage: &agentsv1alpha1.HermesStorage{
			Persistence: &agentsv1alpha1.HermesPersistence{Enabled: true, Size: &size},
		}}
		if desiredSpecHash(buildStatefulSet(ha)) == h1 {
			t.Error("expected different hash when PVC added")
		}
	})

	t.Run("stable when only ObjectMeta differs", func(t *testing.T) {
		ha := minimalHA()
		sts := buildStatefulSet(ha)
		h1 := desiredSpecHash(sts)
		sts.Labels["k8s-injected"] = testTrue
		if desiredSpecHash(sts) != h1 {
			t.Error("hash must not change when only ObjectMeta differs")
		}
	})

	t.Run("changes when podAnnotations change", func(t *testing.T) {
		ha := minimalHA()
		h1 := desiredSpecHash(buildStatefulSet(ha))
		ha.Spec.PodAnnotations = map[string]string{"rotatedAt": "2026-07-06T12:00:00Z"}
		if desiredSpecHash(buildStatefulSet(ha)) == h1 {
			t.Error("expected different hash when podAnnotations change")
		}
	})
}

func TestBuildStatefulSetPodAnnotations(t *testing.T) {
	t.Run("no extra annotations when unset", func(t *testing.T) {
		ha := minimalHA()
		sts := buildStatefulSet(ha)
		if len(sts.Spec.Template.Annotations) != 1 {
			t.Errorf("expected only config-hash annotation, got %v", sts.Spec.Template.Annotations)
		}
	})

	t.Run("user annotations are merged in", func(t *testing.T) {
		ha := minimalHA()
		ha.Spec.PodAnnotations = map[string]string{"rotatedAt": "2026-07-06T12:00:00Z", "prometheus.io/scrape": testTrue}
		sts := buildStatefulSet(ha)
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

func ptrBool(b bool) *bool { return &b }
func ptrInt(i int) *int    { return &i }

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

		wantCmd := `hermes cron create -p "default" --name "full" --deliver "telegram" --repeat 3 --skill "alpha" --skill "beta" --script "myscript.sh" --no-agent --workdir "/opt/data" --profile "default" "every 2h" "do thing"`
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
	t.Run("enabled: ConfigMap has operator keys, init-dotenv mounts them", func(t *testing.T) {
		ha := minimalHA()
		ha.Spec.Hermes = &agentsv1alpha1.Hermes{
			Config: &agentsv1alpha1.HermesConfig{
				APIServer: &agentsv1alpha1.HermesAPIServer{Enabled: true},
			},
		}

		// ConfigMap must contain the operator env-var keys.
		cm, err := buildHermesConfigMap(ha)
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"API_SERVER_ENABLED", "API_SERVER_HOST", "API_SERVER_PORT"} {
			if _, ok := cm.Data[key]; !ok {
				t.Errorf("expected %q in ConfigMap data", key)
			}
		}
		if cm.Data["API_SERVER_ENABLED"] != testTrue {
			t.Errorf("expected API_SERVER_ENABLED=true, got %q", cm.Data["API_SERVER_ENABLED"])
		}
		if cm.Data["API_SERVER_HOST"] != "0.0.0.0" {
			t.Errorf("expected API_SERVER_HOST=0.0.0.0, got %q", cm.Data["API_SERVER_HOST"])
		}

		// init-dotenv must exist and mount the operator ConfigMap and Secret.
		sts := buildStatefulSet(ha)
		ic := findInitContainer(sts, "init-dotenv")
		if ic == nil {
			t.Fatal("expected init-dotenv init container")
		}
		if !hasVolumeMount(ic.VolumeMounts, "hermes-operator-dotenv-configmap") {
			t.Error("expected hermes-operator-dotenv-configmap volume mount on init-dotenv")
		}
		if !hasVolumeMount(ic.VolumeMounts, "hermes-operator-dotenv-secret") {
			t.Error("expected hermes-operator-dotenv-secret volume mount on init-dotenv")
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
		cm, err := buildHermesConfigMap(ha)
		if err != nil {
			t.Fatal(err)
		}
		if cm.Data["API_SERVER_CORS_ORIGINS"] != "https://a.example,https://b.example" {
			t.Errorf("expected CORS origins in ConfigMap, got %q", cm.Data["API_SERVER_CORS_ORIGINS"])
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
		cm, err := buildHermesConfigMap(ha)
		if err != nil {
			t.Fatal(err)
		}
		if cm.Data["API_SERVER_PORT"] != "9000" {
			t.Errorf("expected API_SERVER_PORT=9000, got %q", cm.Data["API_SERVER_PORT"])
		}
	})

	t.Run("disabled: no operator keys in ConfigMap, no init-dotenv", func(t *testing.T) {
		ha := minimalHA()
		cm, err := buildHermesConfigMap(ha)
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"API_SERVER_ENABLED", "API_SERVER_HOST", "API_SERVER_PORT"} {
			if _, ok := cm.Data[key]; ok {
				t.Errorf("expected %q absent from ConfigMap when apiServer disabled", key)
			}
		}
		sts := buildStatefulSet(ha)
		if ic := findInitContainer(sts, "init-dotenv"); ic != nil {
			t.Errorf("expected no init-dotenv when nothing enabled, got: %v", ic)
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

	cm, err := buildHermesConfigMap(ha)
	if err != nil {
		t.Fatal(err)
	}
	if cm.Data["WEBHOOK_ENABLED"] != testTrue {
		t.Errorf("expected WEBHOOK_ENABLED=true, got %q", cm.Data["WEBHOOK_ENABLED"])
	}
	if cm.Data["WEBHOOK_PORT"] != "8644" {
		t.Errorf("expected WEBHOOK_PORT=8644, got %q", cm.Data["WEBHOOK_PORT"])
	}

	sts := buildStatefulSet(ha)
	ic := findInitContainer(sts, "init-dotenv")
	if ic == nil {
		t.Fatal("expected init-dotenv init container")
	}
	if !hasVolumeMount(ic.VolumeMounts, "hermes-operator-dotenv-secret") {
		t.Error("expected hermes-operator-dotenv-secret volume mount on init-dotenv")
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

func TestOperatorDotEnvSidecars(t *testing.T) {
	t.Run("searxng: SEARXNG_URL in ConfigMap and init-dotenv", func(t *testing.T) {
		ha := minimalHA()
		ha.Spec.SearXNG = &agentsv1alpha1.SearXNG{Enabled: true}

		cm, err := buildHermesConfigMap(ha)
		if err != nil {
			t.Fatal(err)
		}
		if cm.Data["SEARXNG_URL"] != "http://localhost:8080" {
			t.Errorf("expected SEARXNG_URL in ConfigMap, got %q", cm.Data["SEARXNG_URL"])
		}

		sts := buildStatefulSet(ha)
		ic := findInitContainer(sts, "init-dotenv")
		if ic == nil {
			t.Fatal("expected init-dotenv init container")
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

	t.Run("camofox: CAMOFOX_URL in ConfigMap and init-dotenv", func(t *testing.T) {
		ha := minimalHA()
		ha.Spec.Camofox = &agentsv1alpha1.Camofox{Enabled: true}

		cm, err := buildHermesConfigMap(ha)
		if err != nil {
			t.Fatal(err)
		}
		if cm.Data["CAMOFOX_URL"] != "http://localhost:9377" {
			t.Errorf("expected CAMOFOX_URL in ConfigMap, got %q", cm.Data["CAMOFOX_URL"])
		}

		sts := buildStatefulSet(ha)
		ic := findInitContainer(sts, "init-dotenv")
		if ic == nil {
			t.Fatal("expected init-dotenv init container")
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

		sts := buildStatefulSet(ha)

		// Default profile: operator ConfigMap + Secret mounted.
		defIC := findInitContainer(sts, "init-dotenv")
		if defIC == nil {
			t.Fatal("expected init-dotenv")
		}
		if !hasVolumeMount(defIC.VolumeMounts, "hermes-operator-dotenv-configmap") {
			t.Error("expected operator configmap mount on default init-dotenv")
		}
		if !hasVolumeMount(defIC.VolumeMounts, "hermes-operator-dotenv-secret") {
			t.Error("expected operator secret mount on default init-dotenv")
		}

		// Named profiles: only sidecar URLs, no API_SERVER_*/WEBHOOK_*.
		profIC := findInitContainer(sts, "init-profiles-dotenv")
		if profIC == nil {
			t.Fatal("expected init-profiles-dotenv")
		}
		profScript := profIC.Args[0]
		if !strings.Contains(profScript, "hermes-operator-dotenv-profile-coder") {
			t.Errorf("expected operator dotenv mount for coder profile, got:\n%s", profScript)
		}
		if !strings.Contains(profScript, "hermes-operator-dotenv-profile-writer") {
			t.Errorf("expected operator dotenv mount for writer profile, got:\n%s", profScript)
		}
		// Must NOT have API_SERVER or WEBHOOK in named-profile dotenv.
		if strings.Contains(profScript, "API_SERVER") {
			t.Errorf("API_SERVER_* must not appear in named-profile dotenv, got:\n%s", profScript)
		}
		if strings.Contains(profScript, "WEBHOOK") {
			t.Errorf("WEBHOOK_* must not appear in named-profile dotenv, got:\n%s", profScript)
		}
		// Each named profile gets its own .env block.
		if strings.Count(profScript, `hermes config env-path -p "coder"`) != 1 {
			t.Errorf("expected one coder dotenv block, got:\n%s", profScript)
		}
		if strings.Count(profScript, `hermes config env-path -p "writer"`) != 1 {
			t.Errorf("expected one writer dotenv block, got:\n%s", profScript)
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
	sts := buildStatefulSet(ha)
	ic := findInitContainer(sts, "init-dotenv")
	if ic == nil {
		t.Fatal("expected init-dotenv")
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
	sts := buildStatefulSet(ha)
	var workspaceIdx, dotenvIdx = -1, -1
	for i, c := range sts.Spec.Template.Spec.InitContainers {
		switch c.Name {
		case "init-workspace":
			workspaceIdx = i
		case "init-dotenv":
			dotenvIdx = i
		}
	}
	if workspaceIdx < 0 {
		t.Fatal("init-workspace not found")
	}
	if dotenvIdx < 0 {
		t.Fatal("init-dotenv not found")
	}
	if dotenvIdx <= workspaceIdx {
		t.Errorf("expected init-dotenv (idx %d) after init-workspace (idx %d)", dotenvIdx, workspaceIdx)
	}
}

func TestOperatorDotEnvUserDotEnvAlone(t *testing.T) {
	// User dotEnv without any operator features still emits init-dotenv.
	ha := minimalHA()
	ha.Spec.Hermes = &agentsv1alpha1.Hermes{
		Workspace: &agentsv1alpha1.HermesWorkspace{
			DotEnv: &agentsv1alpha1.HermesDotEnv{
				SecretRef: &corev1.LocalObjectReference{Name: "my-env"},
			},
		},
	}
	sts := buildStatefulSet(ha)
	if findInitContainer(sts, "init-dotenv") == nil {
		t.Error("expected init-dotenv when user dotEnv is set")
	}
}
