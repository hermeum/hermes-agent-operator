package usecase

import (
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
	// consolidatedInitContainerName is the name of the operator-managed init
	// container that configures the default profile.
	consolidatedInitContainerName = "init-hermes"
)

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
	t.Run("enabled: ConfigMap has operator keys, init-hermes mounts them", func(t *testing.T) {
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

		// init-hermes must exist and mount the operator ConfigMap and Secret.
		sts := buildStatefulSet(ha)
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

	t.Run("disabled: no operator keys in ConfigMap, no dotenv section", func(t *testing.T) {
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

func TestOperatorDotEnvSidecars(t *testing.T) {
	t.Run("searxng: SEARXNG_URL in ConfigMap and init-hermes", func(t *testing.T) {
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

		cm, err := buildHermesConfigMap(ha)
		if err != nil {
			t.Fatal(err)
		}
		if cm.Data["CAMOFOX_URL"] != "http://localhost:9377" {
			t.Errorf("expected CAMOFOX_URL in ConfigMap, got %q", cm.Data["CAMOFOX_URL"])
		}

		sts := buildStatefulSet(ha)
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

		sts := buildStatefulSet(ha)

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
	sts := buildStatefulSet(ha)
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
	sts := buildStatefulSet(ha)
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
	sts := buildStatefulSet(ha)
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
	sts := buildStatefulSet(ha)
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
	sts := buildStatefulSet(ha)
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
	sts := buildStatefulSet(ha)
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
	sts := buildStatefulSet(ha)
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

	sts := buildStatefulSet(ha)

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
	sts2 := buildStatefulSet(ha)
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
	sts := buildStatefulSet(ha)
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
	if !strings.Contains(got, "a\nb") {
		t.Errorf("expected manifest with sorted names a\\nb, got:\n%s", got)
	}
}

func TestBuildProfileCreationScript(t *testing.T) {
	got := buildProfileCreationScript("coder", true)
	if !strings.Contains(got, `hermes profile create "coder" --no-alias --clone || true`) {
		t.Errorf("expected create with clone, got:\n%s", got)
	}
	got2 := buildProfileCreationScript("writer", false)
	if !strings.Contains(got2, `hermes profile create "writer" --no-alias || true`) {
		t.Errorf("expected create without clone, got:\n%s", got2)
	}
	if strings.Contains(got2, "--clone") {
		t.Errorf("unexpected --clone flag, got:\n%s", got2)
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
