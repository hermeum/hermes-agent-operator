package usecase

import (
	"fmt"
	"regexp"
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
		var cmd strings.Builder
		fmt.Fprintf(&cmd, "hermes plugins install -p %q --force %s", profile, enableFlag)
		if p.Ref != "" {
			fmt.Fprintf(&cmd, " --ref %q", p.Ref)
		}
		fmt.Fprintf(&cmd, " %q", p.Identifier)
		installLines = append(installLines, cmd.String())
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
//
// Deleting a profile also removes the operator's manifests for it.  The
// manifests are outside the profile directory, so otherwise they stay after
// the profile is gone.  If the same profile key is added again, the stale
// manifest names the desired source, and the install does not run.
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
      *)
        hermes profile delete "$pname" || true
        rm -rf "$HERMES_HOME/.hermes-agent-operator/profiles/$pname"
        ;;
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
	manifestDir := "$HERMES_HOME/.hermes-agent-operator/profiles/" + name
	cmd := fmt.Sprintf("hermes profile create %q --no-alias", name)
	if clone {
		cmd += " --clone"
	}
	// A profile that no longer comes from a distribution drops the operator's
	// distribution manifest and checkout.  Otherwise, adding the same
	// distribution back later would match the stale manifest and skip the
	// install.
	return fmt.Sprintf(`set -eu
%s || true
rm -rf "%s/distribution" "%s/src"
echo "Profile %s ready"
`, cmd, manifestDir, manifestDir, name)
}

// `gitHubShorthand` matches the one shorthand source the Hermes CLI accepts.
// It mirrors `_GITHUB_SHORTHAND_RE` in `hermes_cli/profile_distribution.py`,
// which `_looks_like_git_url` and `_git_clone` use, so re-check those when the
// CLI changes.  Do not make it broader: the CLI treats any other source
// without a scheme as a local directory, and expanding one here would let a
// pinned profile install from somewhere an unpinned one cannot.
var gitHubShorthand = regexp.MustCompile(`^github\.com/[\w.-]+/[\w.-]+/?$`)

// `gitCloneURL` returns the URL to clone a distribution source from.  The CLI
// expands a `github.com/owner/repo` shorthand to https:// before it clones.
// git reads the shorthand as a local path, so the operator must also expand it
// when it clones a pinned ref.  Other sources do not change.
func gitCloneURL(source string) string {
	if gitHubShorthand.MatchString(source) {
		return "https://" + strings.TrimRight(source, "/")
	}
	return source
}

// `buildProfileDistributionScript` installs a named profile from a profile
// distribution.  It replaces `buildProfileCreationScript` as the first step of
// the init container for that profile.  `hermes profile install` creates the
// profile, so no separate create step is necessary.
//
// The operator records the installed source and ref in its own manifest.  It
// compares them at each start, so it installs once per change and not once per
// restart:
//
//   - The operator clones a pinned distribution at its ref into a checkout on
//     the data volume, and installs from that checkout.  This is necessary
//     because the Hermes CLI always clones the default branch.  The operator
//     keeps the checkout, so the `distribution.yaml` of the profile points at
//     the pinned content.  Thus a manual `hermes profile update` in the `Pod`
//     applies the pin again, and does not move the profile to the default
//     branch.
//   - The operator installs an unpinned distribution directly from its URL, so
//     the profile records that URL.  At each start, `hermes profile update`
//     pulls it again, unless `updateOnStart` is false.
//
// `--force` installs again over an existing profile.  Upstream keeps user data
// (memories, sessions, `auth.json`, and `.env`) in both cases.  It resets
// `config.yaml` from the distribution, which is correct when the source or
// ref changes.
func buildProfileDistributionScript(name string, dist *agentsv1alpha1.HermesProfileDistribution) string {
	manifestDir := "$HERMES_HOME/.hermes-agent-operator/profiles/" + name

	// The source and ref go to the shell as variables.  The CRD patterns for
	// both fields reject each character that could end a double-quoted
	// string, so neither value can escape its quoting.
	var b strings.Builder
	fmt.Fprintf(&b, `set -eu
SOURCE=%q
CLONE_URL=%q
REF=%q
MANIFEST="%s/distribution"
STAGED="%s/src"
PROFILE_DIR="$HERMES_HOME/profiles/%s"
# A tab keeps the pair unambiguous: neither a ref nor a usable git URL holds one.
DESIRED="$SOURCE	$REF"
mkdir -p "%s"

# Write the cause to the termination message, which the operator shows on
# the InitFailed condition.
fail() {
  printf '%%s\n' "$*" > /dev/termination-log 2>/dev/null || true
  echo "$*" >&2
  exit 1
}

# The profile's own distribution.yaml is checked too, because someone can
# delete the profile by hand inside the Pod and leave the manifest behind.
if [ -f "$MANIFEST" ] && [ "$(cat "$MANIFEST")" = "$DESIRED" ] && [ -f "$PROFILE_DIR/distribution.yaml" ]; then
`, dist.GetSource(), gitCloneURL(dist.GetSource()), dist.GetRef(), manifestDir, manifestDir, name, manifestDir)

	switch {
	case dist.ShouldUpdateOnStart():
		forceConfig := ""
		if dist.ShouldForceConfig() {
			forceConfig = " --force-config"
		}
		// The profile is already installed and works, so a failed update
		// (the remote is down, or a new revision needs a newer Hermes) keeps
		// the installed revision rather than stopping the agent.
		fmt.Fprintf(&b, `  hermes profile update %q%s --yes \
    || echo "Profile %s: hermes profile update failed, so the profile stays at its installed revision. The output above says why." >&2
`, name, forceConfig, name)
	default:
		fmt.Fprintf(&b, "  echo \"Profile %s left at its installed revision\"\n", name)
	}

	b.WriteString("else\n")

	if dist.IsPinned() {
		// --branch accepts a tag or a branch name.  A commit SHA needs an
		// explicit fetch, which works only if the host allows it.
		//
		// A branch resolves to a commit once, at install, so the log records
		// that commit before .git goes away.
		fmt.Fprintf(&b, `  export GIT_TERMINAL_PROMPT=0
  rm -rf "$STAGED"
  if ! git clone --depth 1 --branch "$REF" "$CLONE_URL" "$STAGED"; then
    rm -rf "$STAGED"
    mkdir -p "$STAGED"
    git -C "$STAGED" init -q
    git -C "$STAGED" remote add origin "$CLONE_URL"
    git -C "$STAGED" fetch -q --depth 1 origin "$REF" \
      || fail "Profile %s: could not fetch ref $REF from $SOURCE. Check that the ref exists, that the repository is reachable from the cluster, and that the operator has credentials for a private repository."
    git -C "$STAGED" checkout -q FETCH_HEAD
  fi
  echo "Profile %s: $REF is commit $(git -C "$STAGED" rev-parse HEAD)"
  rm -rf "$STAGED/.git"
  hermes profile install "$STAGED" --name %q --force --yes \
    || fail "Profile %s: hermes profile install failed for $SOURCE at $REF. The output above says why; a Hermes version requirement or an unschedulable shipped cron job are the usual causes."
`, name, name, name, name)
	} else {
		// The profile now records the URL, so a checkout from an earlier pin
		// is no longer used.
		fmt.Fprintf(&b, `  hermes profile install "$SOURCE" --name %q --force --yes \
    || fail "Profile %s: hermes profile install failed for $SOURCE. The output above says why; a Hermes version requirement or an unschedulable shipped cron job are the usual causes."
  rm -rf "$STAGED"
`, name, name)
	}

	fmt.Fprintf(&b, `  printf '%%s' "$DESIRED" > "$MANIFEST"
fi
echo "Profile %s ready"
`, name)

	return b.String()
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

// `cronJobsPath` returns the path of a profile's cron job store, relative to
// HERMES_HOME.  The default profile keeps it at the root of the Hermes home;
// every named profile has its own.
func cronJobsPath(profile string) string {
	if profile == hermesDefaultProfile {
		return "/cron/jobs.json"
	}
	return "/profiles/" + profile + "/cron/jobs.json"
}

// `buildGetJobIDFunction` defines a get_job_id shell function.  The function
// reads the job store of the profile and prints the id of the cron job with a
// given name, because the hermes CLI uses ids.  If no job has that name, it
// prints nothing, so callers test for an empty result, not a non-zero exit.
func buildGetJobIDFunction(profile string) string {
	return fmt.Sprintf(`get_job_id() {
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
`, cronJobsPath(profile))
}

func buildCronsScript(profile string, crons []agentsv1alpha1.HermesCron) string {
	manifestDir := "$HERMES_HOME/.hermes-agent-operator/profiles/" + profile
	desiredNames := make([]string, 0, len(crons))
	createLines := make([]string, 0, len(crons))

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

%s
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
`, manifestDir, manifestDir, buildGetJobIDFunction(profile), profile, createScript, manifestContent)
}
