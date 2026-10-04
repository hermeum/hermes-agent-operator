package usecase

import (
	"fmt"
	"sort"
	"strings"

	agentsv1alpha1 "hermeum/hermes-agent-operator/api/v1alpha1"
)

func buildConfigScript(profile string) string {
	bootstrapKey := "profile." + profile + ".config.yaml"
	return fmt.Sprintf(`set -eu
mkdir -p "$HERMES_HOME/home"
if [ -f "/bootstrap/%s" ]; then
  config_path=$(hermes config path -p %q)
  mkdir -p "$(dirname "$config_path")"
  cp "/bootstrap/%s" "$config_path"
  echo "Config written for profile %s"
fi
`, bootstrapKey, profile, bootstrapKey, profile)
}

func buildWorkspaceScript(profile string) string {
	bootstrapPrefix := "profile." + profile + ".workspace."
	manifestDir := "$HERMES_HOME/.hermes-agent-operator/profiles/" + profile
	return fmt.Sprintf(`set -eu
MANIFEST_FILE="%s/workspace-files"
UPDATED_MANIFEST=""
mkdir -p "%s"
profile_home=$(dirname "$(hermes config path -p %q)")

# delete files that were previously managed but are no longer in workspace.files
if [ -f "$MANIFEST_FILE" ]; then
  while IFS= read -r managed; do
    [ -z "$managed" ] && continue
    key="%s$(echo "$managed" | sed 's|/|%s|g')"
    if [ ! -f "/bootstrap/$key" ]; then
      rm -f "$profile_home/$managed"
      echo "Removed outdated workspace file: $managed"
    fi
  done < "$MANIFEST_FILE"
fi

for f in /bootstrap/%s*; do
  [ -f "$f" ] || continue
  relpath=$(basename "$f" | sed 's/^%s//' | sed 's/%s/\//g')
  target="$profile_home/$relpath"
  mkdir -p "$(dirname "$target")"
  cp "$f" "$target"
  echo "Copied workspace file: $relpath"
  UPDATED_MANIFEST="$UPDATED_MANIFEST$relpath
"
done

printf '%%s' "$UPDATED_MANIFEST" > "$MANIFEST_FILE"
`, manifestDir, manifestDir, profile,
		bootstrapPrefix, agentsv1alpha1.HermesWorkspacePathSeparator,
		bootstrapPrefix, bootstrapPrefix, agentsv1alpha1.HermesWorkspacePathSeparator)
}

// pluginDirName derives the plugin directory name from a Git URL or owner/repo shorthand.
// e.g. "owner/hermes-plugin-foo" or "https://github.com/owner/hermes-plugin-foo.git" → "hermes-plugin-foo".
func pluginDirName(identifier string) string {
	s := strings.TrimRight(identifier, "/")
	s = strings.TrimSuffix(s, ".git")
	s = strings.TrimRight(s, "/")
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}

func buildPluginsScript(profile string, plugins []agentsv1alpha1.HermesPlugin) string {
	manifestDir := "$HERMES_HOME/.hermes-agent-operator/profiles/" + profile
	desiredNames := make([]string, 0, len(plugins))
	installLines := make([]string, 0, len(plugins))

	for _, p := range plugins {
		name := pluginDirName(p.Identifier)
		desiredNames = append(desiredNames, name)

		enableFlag := "--enable"
		if p.Enable != nil && !*p.Enable {
			enableFlag = "--no-enable"
		}
		installLines = append(installLines,
			fmt.Sprintf("hermes plugins install -p %q --force %s %q", profile, enableFlag, p.Identifier))
	}

	// case pattern: "name1"|"name2" — safe because plugin names are GitHub repo names
	casePattern := `"` + strings.Join(desiredNames, `"|"`) + `"`
	installScript := strings.Join(installLines, "\n")
	manifestContent := strings.Join(desiredNames, "\n")

	return fmt.Sprintf(`set -eu
MANIFEST="%s/plugins"
mkdir -p "%s"

# Remove plugins present in manifest but no longer desired
if [ -f "$MANIFEST" ]; then
  while IFS= read -r name; do
    [ -z "$name" ] && continue
    case "$name" in
      %s) ;;
      *) hermes plugins remove -p %q "$name" || true ;;
    esac
  done < "$MANIFEST"
fi

# Install desired plugins
%s

# Update manifest
cat > "$MANIFEST" << 'PLUGINS_EOF'
%s
PLUGINS_EOF
`, manifestDir, manifestDir, casePattern, profile, installScript, manifestContent)
}

func skillName(s agentsv1alpha1.HermesSkill) string {
	if s.Name != "" {
		return s.Name
	}
	parts := strings.Split(s.Identifier, "/")
	return strings.TrimSuffix(parts[len(parts)-1], ".md")
}

func buildSkillsScript(profile string, skills []agentsv1alpha1.HermesSkill) string {
	manifestDir := "$HERMES_HOME/.hermes-agent-operator/profiles/" + profile
	desiredNames := make([]string, 0, len(skills))
	installLines := make([]string, 0, len(skills))
	updateLines := make([]string, 0, len(skills))

	for _, s := range skills {
		name := skillName(s)
		desiredNames = append(desiredNames, name)

		var cmd strings.Builder
		fmt.Fprintf(&cmd, "hermes skills install -p %q --yes", profile)
		if s.Category != "" {
			cmd.WriteString(" --category ")
			cmd.WriteString(s.Category)
		}
		if s.Name != "" {
			cmd.WriteString(" --name ")
			cmd.WriteString(s.Name)
		}
		if s.Force {
			cmd.WriteString(" --force")
		}
		cmd.WriteString(" ")
		cmd.WriteString(s.Identifier)
		installLines = append(installLines, cmd.String())

		// Pull any newer version of the skill. Idempotent: a no-op when up to date.
		updateLines = append(updateLines, fmt.Sprintf("hermes skills update -p %q %s || true", profile, name))
	}

	casePattern := `"` + strings.Join(desiredNames, `"|"`) + `"`
	installScript := strings.Join(installLines, "\n")
	updateScript := strings.Join(updateLines, "\n")
	manifestContent := strings.Join(desiredNames, "\n")

	return fmt.Sprintf(`set -eu
MANIFEST="%s/skills"
mkdir -p "%s"

# Remove skills present in manifest but no longer desired
if [ -f "$MANIFEST" ]; then
  while IFS= read -r name; do
    [ -z "$name" ] && continue
    case "$name" in
      %s) ;;
      *) hermes skills uninstall -p %q "$name" || true ;;
    esac
  done < "$MANIFEST"
fi

# Install desired skills
%s

# Update installed skills to the latest version available
%s

# Update manifest
cat > "$MANIFEST" << 'SKILLS_EOF'
%s
SKILLS_EOF
`, manifestDir, manifestDir, casePattern, profile, installScript, updateScript, manifestContent)
}

func buildBundlesScript(profile string, bundles []agentsv1alpha1.HermesBundle) string {
	manifestDir := "$HERMES_HOME/.hermes-agent-operator/profiles/" + profile
	desiredNames := make([]string, 0, len(bundles))
	createLines := make([]string, 0, len(bundles))

	for _, b := range bundles {
		desiredNames = append(desiredNames, b.Name)

		var cmd strings.Builder
		fmt.Fprintf(&cmd, "hermes bundles create -p %q", profile)
		for _, s := range b.Skills {
			fmt.Fprintf(&cmd, " --skill %q", s)
		}
		if b.Description != "" {
			fmt.Fprintf(&cmd, " --description %q", b.Description)
		}
		if b.Instruction != "" {
			fmt.Fprintf(&cmd, " --instruction %q", b.Instruction)
		}
		if b.Force {
			cmd.WriteString(" --force")
		}
		fmt.Fprintf(&cmd, " %q", b.Name)
		// Append "|| true" to avoid failing the whole script, bundles returns non-zero exit code when the bundle already exists.
		createLines = append(createLines, cmd.String()+" || true")
	}

	casePattern := `"` + strings.Join(desiredNames, `"|"`) + `"`
	createScript := strings.Join(createLines, "\n")
	manifestContent := strings.Join(desiredNames, "\n")

	return fmt.Sprintf(`set -eu
MANIFEST="%s/bundles"
mkdir -p "%s"

# Remove bundles present in manifest but no longer desired
if [ -f "$MANIFEST" ]; then
  while IFS= read -r name; do
    [ -z "$name" ] && continue
    case "$name" in
      %s) ;;
      *) hermes bundles delete -p %q "$name" || true ;;
    esac
  done < "$MANIFEST"
fi

# Create desired bundles
%s

# Update manifest
cat > "$MANIFEST" << 'BUNDLES_EOF'
%s
BUNDLES_EOF
`, manifestDir, manifestDir, casePattern, profile, createScript, manifestContent)
}

func buildPythonPackagesScript(cfg *agentsv1alpha1.HermesPipPackages) string {
	if cfg == nil || len(cfg.Install) == 0 {
		return "echo 'No Python packages configured'"
	}

	quoted := make([]string, len(cfg.Install))
	for i, p := range cfg.Install {
		quoted[i] = fmt.Sprintf("%q", p)
	}

	var extraArgs string
	if len(cfg.ExtraArgs) > 0 {
		quotedExtra := make([]string, len(cfg.ExtraArgs))
		for i, a := range cfg.ExtraArgs {
			quotedExtra[i] = fmt.Sprintf("%q", a)
		}
		extraArgs = " " + strings.Join(quotedExtra, " ")
	}

	installCmd := "uv pip install --python /opt/hermes/.venv/bin/python --target \"$TARGET\"" +
		extraArgs + " " + strings.Join(quoted, " ")
	manifestContent := strings.Join(cfg.Install, "\n")

	return fmt.Sprintf(`set -eu
TARGET="$HERMES_HOME/.python-packages"
MANIFEST="$HERMES_HOME/.hermes-agent-operator/python-packages"
mkdir -p "$HERMES_HOME/.hermes-agent-operator"

DESIRED=$(cat <<'PKGS_EOF'
%s
PKGS_EOF
)

if [ -f "$MANIFEST" ] && [ "$(cat "$MANIFEST")" = "$DESIRED" ]; then
  echo "Python packages up-to-date, skipping"
  exit 0
fi

rm -rf "$TARGET"
mkdir -p "$TARGET"
%s

printf '%%s' "$DESIRED" > "$MANIFEST"
`, manifestContent, installCmd)
}

func buildNPMPackagesScript(cfg *agentsv1alpha1.HermesNpmPackages) string {
	if cfg == nil || len(cfg.Install) == 0 {
		return "echo 'No npm packages configured'"
	}

	quoted := make([]string, len(cfg.Install))
	for i, p := range cfg.Install {
		quoted[i] = fmt.Sprintf("%q", p)
	}

	installCmd := "npm install -g --prefix \"$TARGET\" " + strings.Join(quoted, " ")
	manifestContent := strings.Join(cfg.Install, "\n")

	return fmt.Sprintf(`set -eu
TARGET="$HERMES_HOME/.npm-packages"
MANIFEST="$HERMES_HOME/.hermes-agent-operator/npm-packages"
mkdir -p "$HERMES_HOME/.hermes-agent-operator"

DESIRED=$(cat <<'PKGS_EOF'
%s
PKGS_EOF
)

if [ -f "$MANIFEST" ] && [ "$(cat "$MANIFEST")" = "$DESIRED" ]; then
  echo "npm packages up-to-date, skipping"
  exit 0
fi

rm -rf "$TARGET"
mkdir -p "$TARGET"
%s

printf '%%s' "$DESIRED" > "$MANIFEST"
`, manifestContent, installCmd)
}

func buildDotEnvScript(profile string, mountPaths ...string) string {
	var body strings.Builder
	for _, mp := range mountPaths {
		fmt.Fprintf(&body, `  for f in "%s"/*; do
    [ -f "$f" ] || continue
    key="$(basename "$f")"
    value="$(cat "$f")"
    printf '%%s=%%s\n' "$key" "$value"
  done
`, mp)
	}
	return fmt.Sprintf(`set -eu
{
%s} > "$(hermes config env-path -p %q)"
echo "Generated .env for profile %s"
`, body.String(), profile, profile)
}

// buildProfilesCleanupScript removes named profiles no longer desired and
// rewrites the profiles manifest. It runs inside the consolidated init-hermes
// container; profile creation happens in the per-profile init containers.
func buildProfilesCleanupScript(profiles map[string]agentsv1alpha1.HermesProfile) string {
	names := make([]string, 0, len(profiles))
	for name := range profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	casePattern := `"` + strings.Join(names, `"|"`) + `"`
	manifestContent := strings.Join(names, "\n")

	return fmt.Sprintf(`set -eu
PROFILES_MANIFEST="$HERMES_HOME/.hermes-agent-operator/profiles-manifest"
mkdir -p "$HERMES_HOME/.hermes-agent-operator"

if [ -f "$PROFILES_MANIFEST" ]; then
  while IFS= read -r pname; do
    [ -z "$pname" ] && continue
    case "$pname" in
      %s) ;;
      *) hermes profile delete "$pname" || true ;;
    esac
  done < "$PROFILES_MANIFEST"
fi

cat > "$PROFILES_MANIFEST" << 'PROFILES_EOF'
%s
PROFILES_EOF
`, casePattern, manifestContent)
}

// buildProfileCreationScript creates a single named profile. It runs as the
// first step of the profile's own init container, after the default profile
// has been fully configured (so --clone copies complete state).
func buildProfileCreationScript(name string, clone bool) string {
	cmd := fmt.Sprintf("hermes profile create %q --no-alias", name)
	if clone {
		cmd += " --clone"
	}
	return fmt.Sprintf(`set -eu
%s || true
echo "Profile %s ready"
`, cmd, name)
}

// combineInitSteps joins step scripts into a single init script. Each step is
// wrapped in a subshell so steps that exit early (e.g. "packages up-to-date,
// exit 0") cannot abort the remaining steps, and a banner echoes the step
// number for diagnosability.
func combineInitSteps(steps ...string) string {
	var s strings.Builder
	for i, step := range steps {
		fmt.Fprintf(&s, "echo '==> Step %d/%d'\n(\n%s\n)\n", i+1, len(steps), step)
	}
	return s.String()
}

func buildCronsScript(profile string, crons []agentsv1alpha1.HermesCron) string {
	manifestDir := "$HERMES_HOME/.hermes-agent-operator/profiles/" + profile
	desiredNames := make([]string, 0, len(crons))
	createLines := make([]string, 0, len(crons))

	var jobsPathSuffix string
	if profile == hermesDefaultProfile {
		jobsPathSuffix = "/cron/jobs.json"
	} else {
		jobsPathSuffix = "/profiles/" + profile + "/cron/jobs.json"
	}

	for _, c := range crons {
		desiredNames = append(desiredNames, c.Name)

		var cmd strings.Builder
		fmt.Fprintf(&cmd, "hermes cron create -p %q", profile)
		fmt.Fprintf(&cmd, " --name %q", c.Name)
		if c.Deliver != "" {
			fmt.Fprintf(&cmd, " --deliver %q", c.Deliver)
		}
		if c.Repeat != nil {
			fmt.Fprintf(&cmd, " --repeat %d", *c.Repeat)
		}
		for _, s := range c.Skills {
			fmt.Fprintf(&cmd, " --skill %q", s)
		}
		if c.Script != "" {
			fmt.Fprintf(&cmd, " --script %q", c.Script)
		}
		if c.NoAgent {
			cmd.WriteString(" --no-agent")
		}
		if c.Workdir != "" {
			fmt.Fprintf(&cmd, " --workdir %q", c.Workdir)
		}
		if c.MonitorScript != "" {
			fmt.Fprintf(&cmd, " --monitor-script %q", c.MonitorScript)
		}
		if c.MonitorURL != "" {
			fmt.Fprintf(&cmd, " --monitor-url %q", c.MonitorURL)
		}
		if c.Model != "" {
			fmt.Fprintf(&cmd, " --model %q", c.Model)
		}
		if c.Provider != "" {
			fmt.Fprintf(&cmd, " --provider %q", c.Provider)
		}
		if c.ReasoningEffort != "" {
			fmt.Fprintf(&cmd, " --reasoning-effort %q", c.ReasoningEffort)
		}
		if c.Continuity {
			cmd.WriteString(" --continuity")
		}
		if c.Profile != "" {
			fmt.Fprintf(&cmd, " --profile %q", c.Profile)
		}
		fmt.Fprintf(&cmd, " %q", c.Schedule)
		if c.Prompt != "" {
			fmt.Fprintf(&cmd, " %q", c.Prompt)
		}
		createLines = append(createLines, cmd.String())
	}

	createScript := strings.Join(createLines, "\n")
	manifestContent := strings.Join(desiredNames, "\n")

	return fmt.Sprintf(`set -eu
MANIFEST="%s/crons"
mkdir -p "%s"

get_job_id() {
  python3 - "$1" <<'PY'
import json, os, sys
p = os.environ.get("HERMES_HOME", "/opt/data") + "%s"
if not os.path.exists(p):
    sys.exit(0)
with open(p) as f:
    data = json.load(f)
for j in data.get("jobs", []):
    if j.get("name") == sys.argv[1]:
        print(j.get("id", ""))
        break
PY
}

# Remove crons present in manifest but no longer desired
if [ -f "$MANIFEST" ]; then
  while IFS= read -r name; do
    [ -z "$name" ] && continue
    id=$(get_job_id "$name")
    [ -z "$id" ] && continue
    hermes cron remove -p %q "$id" || true
  done < "$MANIFEST"
fi

# Create desired crons
%s

# Update manifest
cat > "$MANIFEST" << 'CRONS_EOF'
%s
CRONS_EOF
`, manifestDir, manifestDir, jobsPathSuffix, profile, createScript, manifestContent)
}
