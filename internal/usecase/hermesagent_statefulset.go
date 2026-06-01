package usecase

import (
	"context"
	"crypto/sha256"
	"fmt"
	agentsv1alpha1 "noahingh/hermes-agent-operator/api/v1alpha1"
	"sort"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	hermesContainerName          = "hermes-agent"
	hermesGatewayPortName        = "gateway"
	hermesGatewayPort            = int32(8642)
	hermesWorkspacePathSeparator = "--"
)

func (u *HermesAgentUseCase) reconcileStatefulSet(ctx context.Context, ha *agentsv1alpha1.HermesAgent) error {
	sts, err := u.kube.GetStatefulSet(ctx, GetStatefulSetParam{
		NamespacedName: types.NamespacedName{Name: ha.Name, Namespace: ha.Namespace},
	})
	if err != nil {
		return err
	}

	desired := buildStatefulSet(ha)

	if sts != nil {
		desired.ResourceVersion = sts.ResourceVersion
		err := u.kube.UpdateStatefulSetOwnedByHermesAgent(ctx, UpdateStatefulSetParam{HermesAgent: ha, StatefulSet: desired})
		u.tel.IncStatefulSetOperation(ctx, IncStatefulSetOperationParam{Operation: OperationUpdate, Result: resultOf(err)})
		return err
	}

	err = u.kube.CreateStatefulSetOwnedByHermesAgent(ctx, CreateStatefulSetOfHermesAgentParam{
		HermesAgent: ha,
		StatefulSet: desired,
	})
	u.tel.IncStatefulSetOperation(ctx, IncStatefulSetOperationParam{Operation: OperationCreate, Result: resultOf(err)})
	return err
}

func configMapDataHash(data map[string]string) string {
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, k := range keys {
		_, _ = fmt.Fprintf(h, "%s\x00%s\x00", k, data[k])
	}
	return fmt.Sprintf("%x", h.Sum(nil))[:16]
}

func buildStatefulSet(ha *agentsv1alpha1.HermesAgent) *appsv1.StatefulSet {
	replicas := int32(1)
	if ha.IsSuspended() {
		replicas = int32(0)
	}

	// The config hash annotation is used to trigger a rolling update of the StatefulSet when the config changes.
	cm, _ := buildHermesConfigMap(ha)
	configHash := configMapDataHash(cm.Data)

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ha.Name,
			Namespace: ha.Namespace,
			Labels:    resourceLabels(ha),
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: selectorLabels(ha),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: resourceLabels(ha),
					Annotations: map[string]string{
						domain + "/config-hash": configHash,
					},
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: ha.GetServiceAccountName(),
					SecurityContext:    ha.GetSecurity().GetPodSecurityContext(),
				},
			},
		},
	}

	sts = buildHermesContainer(ha, sts)
	sts = buildSearXNGContainer(ha, sts)
	sts = buildCamofoxContainer(ha, sts)

	// additional user-provided init containers run after the operator-managed ones.
	sts.Spec.Template.Spec.InitContainers = append(sts.Spec.Template.Spec.InitContainers, ha.GetInitContainers()...)

	// additional user-provided sidecar containers run alongside the hermes-agent container.
	sts.Spec.Template.Spec.Containers = append(sts.Spec.Template.Spec.Containers, ha.GetSidecars()...)

	// additional user-provided volumes.
	sts.Spec.Template.Spec.Volumes = append(sts.Spec.Template.Spec.Volumes, ha.GetExtraVolumes()...)

	return sts
}

// buildHermesContainer populates the StatefulSet with all resources driven by the hermes spec:
// the main hermes-agent container (env, envFrom), init containers for config and workspace,
// and volumes/PVCs for persistence, bootstrap config, and shared memory.
func buildHermesContainer(ha *agentsv1alpha1.HermesAgent, sts *appsv1.StatefulSet) *appsv1.StatefulSet {
	const (
		hermesDefaultPathEnv  = "/opt/data/.local/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
		hermesPathEnv         = hermesDefaultPathEnv + ":/opt/hermes/.venv/bin"
		hermesHomeVolume      = "hermes-data"
		hermesHomeMount       = "/opt/data"
		hermesDSHMVolume      = "dshm"
		hermesDSHMMount       = "/dev/shm"
		hermesTmpVolume       = "tmp"
		hermesTmpMount        = "/tmp"
		hermesBootstrapVolume = "bootstrap"
		hermesBootstrapMount  = "/opt/hermes/bootstrap"
	)

	sts = sts.DeepCopy()
	sizeLimit := resource.MustParse("1Gi")
	sec := ha.GetSecurity()

	initContainers := []corev1.Container{}
	containers := []corev1.Container{
		{
			Name:            hermesContainerName,
			Image:           ha.GetHermes().GetImage(),
			ImagePullPolicy: corev1.PullIfNotPresent,
			Args:            []string{"gateway", "run"},
			WorkingDir:      "/opt/hermes",
			Ports: []corev1.ContainerPort{
				{
					Name:          hermesGatewayPortName,
					ContainerPort: hermesGatewayPort,
					Protocol:      corev1.ProtocolTCP,
				},
			},
			Env: append([]corev1.EnvVar{
				{Name: "HERMES_HOME", Value: hermesHomeMount},
				{Name: "HOME", Value: hermesHomeMount + "/home"},
				{Name: "PATH", Value: hermesPathEnv},
			}, ha.GetHermes().GetEnv()...),
			EnvFrom:         ha.GetHermes().GetEnvFrom(),
			Resources:       ha.GetHermes().GetResources(),
			SecurityContext: sec.GetContainerSecurityContext(),
			LivenessProbe: ha.GetHermes().GetProbes().GetLiveness().GetProbe("/health", hermesGatewayPortName, corev1.Probe{
				InitialDelaySeconds: 15, PeriodSeconds: 20, TimeoutSeconds: 1, FailureThreshold: 3,
			}),
			ReadinessProbe: ha.GetHermes().GetProbes().GetReadiness().GetProbe("/health", hermesGatewayPortName, corev1.Probe{
				InitialDelaySeconds: 5, PeriodSeconds: 10, TimeoutSeconds: 1, FailureThreshold: 3,
			}),
			StartupProbe: ha.GetHermes().GetProbes().GetStartup().GetProbe("/health", hermesGatewayPortName, corev1.Probe{
				InitialDelaySeconds: 0, PeriodSeconds: 10, TimeoutSeconds: 1, FailureThreshold: 10,
			}),
			VolumeMounts: append([]corev1.VolumeMount{
				{Name: hermesDSHMVolume, MountPath: hermesDSHMMount},
				{Name: hermesHomeVolume, MountPath: hermesHomeMount},
				{Name: hermesTmpVolume, MountPath: hermesTmpMount},
			}, ha.GetExtraVolumeMounts()...),
		},
	}
	volumes := []corev1.Volume{
		{
			Name: hermesDSHMVolume,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{
					Medium:    corev1.StorageMediumMemory,
					SizeLimit: &sizeLimit,
				},
			},
		},
		{
			Name:         hermesTmpVolume,
			VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
		},
		{
			Name: hermesBootstrapVolume,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: ha.GetHermesName()},
				},
			},
		},
	}
	pvc := []corev1.PersistentVolumeClaim{}

	// persistence: existingClaim > enabled PVC > emptyDir fallback.
	hp := ha.GetHermes().GetPersistence()
	if ec := hp.GetExistingClaim(); ec != "" {
		volumes = append(volumes, corev1.Volume{
			Name: hermesHomeVolume,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: ec,
				},
			},
		})
	} else if hp != nil && hp.Enabled {
		pvc = append(pvc, corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: hermesHomeVolume},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: hp.GetSize(),
					},
				},
				StorageClassName: hp.StorageClassName,
			},
		})
	} else {
		volumes = append(volumes, corev1.Volume{
			Name:         hermesHomeVolume,
			VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
		})
	}

	// config: init container copies config.yaml from the bootstrap ConfigMap to the data volume.
	if hc := ha.GetHermes().GetConfig(); hc != nil {
		initContainers = append(initContainers, corev1.Container{
			Name:            "init-config",
			Image:           ha.GetHermes().GetImage(),
			ImagePullPolicy: corev1.PullIfNotPresent,
			Command:         []string{"/bin/sh", "-ec"},
			Args:            []string{buildConfigScript()},
			Env: []corev1.EnvVar{
				{Name: "HERMES_HOME", Value: hermesHomeMount},
				{Name: "PATH", Value: hermesPathEnv},
			},
			SecurityContext: sec.GetContainerSecurityContext(),
			VolumeMounts: []corev1.VolumeMount{
				{Name: hermesHomeVolume, MountPath: hermesHomeMount},
				{Name: hermesBootstrapVolume, MountPath: hermesBootstrapMount, ReadOnly: true},
				{Name: hermesTmpVolume, MountPath: hermesTmpMount},
			},
		})
	}

	// workspace: init container copies workspace files from the bootstrap ConfigMap.
	// ConfigMap keys use the format "workspace.<path>" with "/" replaced by "--".
	if hw := ha.GetHermes().GetWorkspace(); hw != nil && len(hw.Files) > 0 {
		initContainers = append(initContainers, corev1.Container{
			Name:            "init-workspace",
			Image:           ha.GetHermes().GetImage(),
			ImagePullPolicy: corev1.PullIfNotPresent,
			Command:         []string{"/bin/sh", "-ec"},
			Args:            []string{buildWorkspaceScript()},
			Env: []corev1.EnvVar{
				{Name: "HERMES_HOME", Value: hermesHomeMount},
				{Name: "PATH", Value: hermesPathEnv},
			},
			SecurityContext: sec.GetContainerSecurityContext(),
			VolumeMounts: []corev1.VolumeMount{
				{Name: hermesHomeVolume, MountPath: hermesHomeMount},
				{Name: hermesBootstrapVolume, MountPath: hermesBootstrapMount, ReadOnly: true},
				{Name: hermesTmpVolume, MountPath: hermesTmpMount},
			},
		})
	}

	// plugins: init container installs desired plugins and removes stale ones.
	if plugins := ha.GetHermes().GetPlugins(); len(plugins) > 0 {
		initContainers = append(initContainers, corev1.Container{
			Name:            "init-plugins",
			Image:           ha.GetHermes().GetImage(),
			ImagePullPolicy: corev1.PullIfNotPresent,
			Command:         []string{"/bin/sh", "-ec"},
			Args:            []string{buildPluginsScript(plugins)},
			Env: []corev1.EnvVar{
				{Name: "HERMES_HOME", Value: hermesHomeMount},
				{Name: "PATH", Value: hermesPathEnv},
			},
			SecurityContext: sec.GetContainerSecurityContext(),
			VolumeMounts: []corev1.VolumeMount{
				{Name: hermesHomeVolume, MountPath: hermesHomeMount},
				{Name: hermesTmpVolume, MountPath: hermesTmpMount},
			},
		})
	}

	// skills: init container installs/uninstalls skills via the hermes CLI.
	if skills := ha.GetHermes().GetSkills(); len(skills) > 0 {
		initContainers = append(initContainers, corev1.Container{
			Name:            "init-skills",
			Image:           ha.GetHermes().GetImage(),
			ImagePullPolicy: corev1.PullIfNotPresent,
			Command:         []string{"/bin/sh", "-ec"},
			Args:            []string{buildSkillsScript(skills)},
			Env: []corev1.EnvVar{
				{Name: "HERMES_HOME", Value: hermesHomeMount},
				{Name: "PATH", Value: hermesPathEnv},
			},
			SecurityContext: sec.GetContainerSecurityContext(),
			VolumeMounts: []corev1.VolumeMount{
				{Name: hermesHomeVolume, MountPath: hermesHomeMount},
				{Name: hermesTmpVolume, MountPath: hermesTmpMount},
			},
		})
	}

	// bundles: init container reconciles bundles via the hermes CLI.
	if bundles := ha.GetHermes().GetBundles(); len(bundles) > 0 {
		initContainers = append(initContainers, corev1.Container{
			Name:            "init-bundles",
			Image:           ha.GetHermes().GetImage(),
			ImagePullPolicy: corev1.PullIfNotPresent,
			Command:         []string{"/bin/sh", "-ec"},
			Args:            []string{buildBundlesScript(bundles)},
			Env: []corev1.EnvVar{
				{Name: "HERMES_HOME", Value: "/opt/data"},
				{Name: "PATH", Value: hermesPathEnv},
			},
			SecurityContext: sec.GetContainerSecurityContext(),
			VolumeMounts: []corev1.VolumeMount{
				{Name: "data", MountPath: "/opt/data"},
				{Name: "tmp", MountPath: "/tmp"},
			},
		})
	}

	// crons: init container reconciles scheduled jobs via the hermes CLI.
	if crons := ha.GetHermes().GetCrons(); len(crons) > 0 {
		initContainers = append(initContainers, corev1.Container{
			Name:            "init-crons",
			Image:           "nousresearch/hermes-agent:latest",
			ImagePullPolicy: corev1.PullIfNotPresent,
			Command:         []string{"/bin/sh", "-ec"},
			Args:            []string{buildCronsScript(crons)},
			Env: []corev1.EnvVar{
				{Name: "HERMES_HOME", Value: hermesHomeMount},
				{Name: "PATH", Value: hermesPathEnv},
			},
			SecurityContext: sec.GetContainerSecurityContext(),
			VolumeMounts: []corev1.VolumeMount{
				{Name: hermesHomeVolume, MountPath: hermesHomeMount},
				{Name: hermesTmpVolume, MountPath: hermesTmpMount},
			},
		})
	}

	sts.Spec.Template.Spec.InitContainers = append(sts.Spec.Template.Spec.InitContainers, initContainers...)
	sts.Spec.Template.Spec.Containers = append(sts.Spec.Template.Spec.Containers, containers...)
	sts.Spec.Template.Spec.Volumes = append(sts.Spec.Template.Spec.Volumes, volumes...)
	sts.Spec.VolumeClaimTemplates = append(sts.Spec.VolumeClaimTemplates, pvc...)

	return sts
}

func findContainer(sts *appsv1.StatefulSet, name string) *corev1.Container {
	for i := range sts.Spec.Template.Spec.Containers {
		if sts.Spec.Template.Spec.Containers[i].Name == name {
			return &sts.Spec.Template.Spec.Containers[i]
		}
	}
	return nil
}

func buildSearXNGContainer(ha *agentsv1alpha1.HermesAgent, sts *appsv1.StatefulSet) *appsv1.StatefulSet {
	sts = sts.DeepCopy()

	sx := ha.GetSearXNG()
	if !sx.IsEnabled() {
		return sts
	}

	const (
		searxngContainerName = "searxng"
		searxngPortName      = "searxng"
		searxngPort          = int32(8080)
		searxngConfigVolume  = "searxng-config"
		searxngConfigMount   = "/etc/searxng"
		searxngCacheVolume   = "searxng-cache"
		searxngCacheMount    = "/var/cache/searxng"
		searxngURL           = "http://localhost:8080"
	)

	// Inject SEARXNG_URL into the hermes-agent container env so that the web_search tool can find it.
	if c := findContainer(sts, hermesContainerName); c != nil {
		c.Env = append(c.Env, corev1.EnvVar{Name: "SEARXNG_URL", Value: searxngURL})
	}

	sts.Spec.Template.Spec.Containers = append(sts.Spec.Template.Spec.Containers, corev1.Container{
		Name:            searxngContainerName,
		Image:           sx.GetImage(),
		ImagePullPolicy: corev1.PullIfNotPresent,
		Ports: []corev1.ContainerPort{
			{Name: searxngPortName, ContainerPort: searxngPort, Protocol: corev1.ProtocolTCP},
		},
		Env: append([]corev1.EnvVar{
			{Name: "SEARXNG_BASE_URL", Value: searxngURL + "/"},
		}, sx.GetExtraEnv()...),
		Resources: sx.GetResources(),
		VolumeMounts: []corev1.VolumeMount{
			{Name: searxngConfigVolume, MountPath: searxngConfigMount},
			{Name: searxngCacheVolume, MountPath: searxngCacheMount},
		},
	})

	sts.Spec.Template.Spec.Volumes = append(sts.Spec.Template.Spec.Volumes, corev1.Volume{
		Name: searxngConfigVolume,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: ha.GetSearXNGName()},
			},
		},
	})

	// cache: existingClaim > managed PVC > emptyDir fallback.
	sp := sx.GetPersistence()
	switch {
	case sp.GetExistingClaim() != "":
		sts.Spec.Template.Spec.Volumes = append(sts.Spec.Template.Spec.Volumes, corev1.Volume{
			Name: searxngCacheVolume,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: sp.GetExistingClaim()},
			},
		})
	case sp.IsEnabled():
		sts.Spec.VolumeClaimTemplates = append(sts.Spec.VolumeClaimTemplates, corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: searxngCacheVolume},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceStorage: sp.GetSize()},
				},
				StorageClassName: sp.StorageClassName,
			},
		})
	default:
		sts.Spec.Template.Spec.Volumes = append(sts.Spec.Template.Spec.Volumes, corev1.Volume{
			Name:         searxngCacheVolume,
			VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
		})
	}

	return sts
}

func buildCamofoxContainer(ha *agentsv1alpha1.HermesAgent, sts *appsv1.StatefulSet) *appsv1.StatefulSet {
	sts = sts.DeepCopy()

	cx := ha.GetCamofox()
	if !cx.IsEnabled() {
		return sts
	}

	const (
		camofoxContainerName = "camofox"
		camofoxPortName      = "camofox"
		camofoxPort          = int32(9377)
		camofoxDataVolume    = "camofox-data"
		camofoxDataMount     = "/root/.camofox"
		camofoxURL           = "http://localhost:9377"
	)

	// Inject CAMOFOX_URL into the hermes-agent container env so that the browser tool can find it.
	if c := findContainer(sts, hermesContainerName); c != nil {
		c.Env = append(c.Env, corev1.EnvVar{Name: "CAMOFOX_URL", Value: camofoxURL})
	}

	sts.Spec.Template.Spec.Containers = append(sts.Spec.Template.Spec.Containers, corev1.Container{
		Name:            camofoxContainerName,
		Image:           cx.GetImage(),
		ImagePullPolicy: corev1.PullIfNotPresent,
		Ports: []corev1.ContainerPort{
			{Name: camofoxPortName, ContainerPort: camofoxPort, Protocol: corev1.ProtocolTCP},
		},
		Env:       cx.GetExtraEnv(),
		Resources: cx.GetResources(),
		VolumeMounts: []corev1.VolumeMount{
			{Name: camofoxDataVolume, MountPath: camofoxDataMount},
		},
	})

	// data: existingClaim > managed PVC > emptyDir fallback.
	cp := cx.GetPersistence()
	switch {
	case cp.GetExistingClaim() != "":
		sts.Spec.Template.Spec.Volumes = append(sts.Spec.Template.Spec.Volumes, corev1.Volume{
			Name: camofoxDataVolume,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: cp.GetExistingClaim()},
			},
		})
	case cp.IsEnabled():
		sts.Spec.Template.Spec.Volumes = append(sts.Spec.Template.Spec.Volumes, corev1.Volume{
			Name: camofoxDataVolume,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: ha.GetCamofoxName()},
			},
		})
	default:
		sts.Spec.Template.Spec.Volumes = append(sts.Spec.Template.Spec.Volumes, corev1.Volume{
			Name:         camofoxDataVolume,
			VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
		})
	}

	return sts
}

func buildConfigScript() string {
	return `set -eu
mkdir -p "/opt/data/home"
cp "/bootstrap/config.yaml" "/opt/data/config.yaml"
echo "Config file copied"
`
}

func buildWorkspaceScript() string {
	return fmt.Sprintf(`set -eu
MANIFEST_FILE="/opt/data/.hermes-agent-operator/workspace-files"
UPDATED_MANIFEST=""
mkdir -p "/opt/data/.hermes-agent-operator"

# delete files that were previously managed but are no longer in workspace.files
if [ -f "$MANIFEST_FILE" ]; then
  while IFS= read -r managed; do
    [ -z "$managed" ] && continue
    key="workspace.$(echo "$managed" | sed 's|/|%s|g')"
    if [ ! -f "/bootstrap/$key" ]; then
      rm -f "/opt/data/$managed"
			echo "Removed outdated workspace file: $managed"
    fi
  done < "$MANIFEST_FILE"
fi

for f in /bootstrap/workspace.*; do
  [ -f "$f" ] || continue
  relpath=$(basename "$f" | sed 's/^workspace\.//' | sed 's/%s/\//g')
  target="/opt/data/$relpath"
  mkdir -p "$(dirname "$target")"
  cp "$f" "$target"
	echo "Copied workspace file: $relpath"
  UPDATED_MANIFEST="$UPDATED_MANIFEST$relpath
"
done

printf '%%s' "$UPDATED_MANIFEST" > "$MANIFEST_FILE"
`, hermesWorkspacePathSeparator, hermesWorkspacePathSeparator)
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

func buildPluginsScript(plugins []agentsv1alpha1.HermesPlugin) string {
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
			fmt.Sprintf("hermes plugins install --force %s %q", enableFlag, p.Identifier))
	}

	// case pattern: "name1"|"name2" — safe because plugin names are GitHub repo names
	casePattern := `"` + strings.Join(desiredNames, `"|"`) + `"`
	installScript := strings.Join(installLines, "\n")
	manifestContent := strings.Join(desiredNames, "\n")

	script := fmt.Sprintf(`set -eu
MANIFEST="/opt/data/.hermes-agent-operator/plugins"
mkdir -p "/opt/data/.hermes-agent-operator"

# Remove plugins present in manifest but no longer desired
if [ -f "$MANIFEST" ]; then
  while IFS= read -r name; do
    [ -z "$name" ] && continue
    case "$name" in
      %s) ;;
      *) /hermes plugins remove "$name" || true ;;
    esac
  done < "$MANIFEST"
fi

# Install desired plugins
%s

# Update manifest
cat > "$MANIFEST" << 'PLUGINS_EOF'
%s
PLUGINS_EOF
`, casePattern, installScript, manifestContent)

	return script
}

func skillName(s agentsv1alpha1.HermesSkill) string {
	if s.Name != "" {
		return s.Name
	}
	parts := strings.Split(s.Identifier, "/")
	return strings.TrimSuffix(parts[len(parts)-1], ".md")
}

func buildSkillsScript(skills []agentsv1alpha1.HermesSkill) string {
	desiredNames := make([]string, 0, len(skills))
	installLines := make([]string, 0, len(skills))

	for _, s := range skills {
		name := skillName(s)
		desiredNames = append(desiredNames, name)

		var cmd strings.Builder
		cmd.WriteString("hermes skills install --yes")
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
	}

	casePattern := `"` + strings.Join(desiredNames, `"|"`) + `"`
	installScript := strings.Join(installLines, "\n")
	manifestContent := strings.Join(desiredNames, "\n")

	return fmt.Sprintf(`set -eu
MANIFEST="/opt/data/.hermes-agent-operator/skills"
mkdir -p "/opt/data/.hermes-agent-operator"

# Remove skills present in manifest but no longer desired
if [ -f "$MANIFEST" ]; then
  while IFS= read -r name; do
    [ -z "$name" ] && continue
    case "$name" in
      %s) ;;
      *) hermes skills uninstall "$name" || true ;;
    esac
  done < "$MANIFEST"
fi

# Install desired skills
%s

# Update manifest
cat > "$MANIFEST" << 'SKILLS_EOF'
%s
SKILLS_EOF
`, casePattern, installScript, manifestContent)
}

func buildBundlesScript(bundles []agentsv1alpha1.HermesBundle) string {
	desiredNames := make([]string, 0, len(bundles))
	createLines := make([]string, 0, len(bundles))

	for _, b := range bundles {
		desiredNames = append(desiredNames, b.Name)

		var cmd strings.Builder
		cmd.WriteString("hermes bundles create")
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
		createLines = append(createLines, cmd.String())
	}

	casePattern := `"` + strings.Join(desiredNames, `"|"`) + `"`
	createScript := strings.Join(createLines, "\n")
	manifestContent := strings.Join(desiredNames, "\n")

	return fmt.Sprintf(`set -eu
MANIFEST="/opt/data/.hermes-agent-operator/bundles"
mkdir -p "/opt/data/.hermes-agent-operator"

# Remove bundles present in manifest but no longer desired
if [ -f "$MANIFEST" ]; then
  while IFS= read -r name; do
    [ -z "$name" ] && continue
    case "$name" in
      %s) ;;
      *) hermes bundles delete "$name" || true ;;
    esac
  done < "$MANIFEST"
fi

# Create desired bundles
%s

# Update manifest
cat > "$MANIFEST" << 'BUNDLES_EOF'
%s
BUNDLES_EOF
`, casePattern, createScript, manifestContent)
}

func buildCronsScript(crons []agentsv1alpha1.HermesCron) string {
	desiredNames := make([]string, 0, len(crons))
	createLines := make([]string, 0, len(crons))

	for _, c := range crons {
		desiredNames = append(desiredNames, c.Name)

		var cmd strings.Builder
		cmd.WriteString("hermes cron create")
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
MANIFEST="/opt/data/.hermes-agent-operator/crons"
mkdir -p "/opt/data/.hermes-agent-operator"

get_job_id() {
  python3 - "$1" <<'PY'
import json, os, sys
p = "/opt/data/cron/jobs.json"
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
    hermes cron remove "$id" || true
  done < "$MANIFEST"
fi

# Create desired crons
%s

# Update manifest
cat > "$MANIFEST" << 'CRONS_EOF'
%s
CRONS_EOF
`, createScript, manifestContent)
}
