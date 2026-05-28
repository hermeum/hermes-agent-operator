package usecase

import (
	"strings"
	"testing"

	agentsv1alpha1 "noahingh/hermes-agent-operator/api/v1alpha1"
)

func ptrBool(b bool) *bool { return &b }
func ptrInt(i int) *int    { return &i }

func TestBuildPluginsScript(t *testing.T) {
	u := &HermesAgentUseCase{}

	t.Run("default enable", func(t *testing.T) {
		got := u.buildPluginsScript([]agentsv1alpha1.HermesPlugin{
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
		got := u.buildPluginsScript([]agentsv1alpha1.HermesPlugin{
			{Identifier: "https://github.com/owner/hermes-plugin-foo.git", Enable: ptrBool(false)},
		})

		wantCmd := `hermes plugins install --force --no-enable "https://github.com/owner/hermes-plugin-foo.git"`
		if !strings.Contains(got, wantCmd) {
			t.Errorf("expected install command %q in script, got:\n%s", wantCmd, got)
		}
	})

	t.Run("explicit enable true", func(t *testing.T) {
		got := u.buildPluginsScript([]agentsv1alpha1.HermesPlugin{
			{Identifier: "owner/repo", Enable: ptrBool(true)},
		})

		wantCmd := `hermes plugins install --force --enable "owner/repo"`
		if !strings.Contains(got, wantCmd) {
			t.Errorf("expected install command %q in script, got:\n%s", wantCmd, got)
		}
	})

	t.Run("multiple plugins build case pattern and manifest", func(t *testing.T) {
		got := u.buildPluginsScript([]agentsv1alpha1.HermesPlugin{
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
	u := &HermesAgentUseCase{}

	t.Run("identifier only", func(t *testing.T) {
		got := u.buildSkillsScript([]agentsv1alpha1.HermesSkill{
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
		got := u.buildSkillsScript([]agentsv1alpha1.HermesSkill{
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
		got := u.buildSkillsScript([]agentsv1alpha1.HermesSkill{
			{Identifier: "openai/skills/s1"},
		})

		if !strings.Contains(got, `hermes skills uninstall "$name" || true`) {
			t.Errorf("expected uninstall command in script, got:\n%s", got)
		}
	})

	t.Run("multiple skills manifest order", func(t *testing.T) {
		got := u.buildSkillsScript([]agentsv1alpha1.HermesSkill{
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

func TestBuildCronsScript(t *testing.T) {
	u := &HermesAgentUseCase{}

	t.Run("minimal", func(t *testing.T) {
		got := u.buildCronsScript([]agentsv1alpha1.HermesCron{
			{Name: "daily", Schedule: "0 9 * * *"},
		})

		wantCmd := `hermes cron create --name "daily" "0 9 * * *"`
		if !strings.Contains(got, wantCmd) {
			t.Errorf("expected %q in script, got:\n%s", wantCmd, got)
		}
	})

	t.Run("with prompt", func(t *testing.T) {
		got := u.buildCronsScript([]agentsv1alpha1.HermesCron{
			{Name: "p", Schedule: "30m", Prompt: "say hi"},
		})

		wantCmd := `hermes cron create --name "p" "30m" "say hi"`
		if !strings.Contains(got, wantCmd) {
			t.Errorf("expected %q in script, got:\n%s", wantCmd, got)
		}
	})

	t.Run("all options", func(t *testing.T) {
		got := u.buildCronsScript([]agentsv1alpha1.HermesCron{
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
		got := u.buildCronsScript([]agentsv1alpha1.HermesCron{
			{Name: "j", Schedule: "1h"},
		})
		if !strings.Contains(got, `hermes cron remove "$id" || true`) {
			t.Errorf("expected remove command in script, got:\n%s", got)
		}
	})

	t.Run("manifest contains names", func(t *testing.T) {
		got := u.buildCronsScript([]agentsv1alpha1.HermesCron{
			{Name: "a", Schedule: "1h"},
			{Name: "b", Schedule: "2h"},
		})
		if !strings.Contains(got, "a\nb") {
			t.Errorf("expected manifest with names a\\nb, got:\n%s", got)
		}
	})
}
