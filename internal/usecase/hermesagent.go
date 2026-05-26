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
	sigsyaml "sigs.k8s.io/yaml"
)

const (
	domain                 = "hermes-agent-operator.xyz"
	workspacePathSeparator = "--"
)

func skillName(s agentsv1alpha1.HermesSkill) string {
	if s.Name != "" {
		return s.Name
	}
	parts := strings.Split(s.Identifier, "/")
	return strings.TrimSuffix(parts[len(parts)-1], ".md")
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

type HermesAgentUseCase struct {
	kube Kubernetes
}

type ReconcileParam struct {
	NamespacedName types.NamespacedName
}

func NewHermesAgentUseCase(kube Kubernetes) *HermesAgentUseCase {
	return &HermesAgentUseCase{
		kube,
	}
}

func (u *HermesAgentUseCase) Reconcile(ctx context.Context, param ReconcileParam) error {
	ha, err := u.kube.GetHermesAgent(ctx, GetHermesAgentParam(param))
	if err != nil {
		return err
	}
	if ha == nil {
		return nil
	}

	if err := u.reconcileConfigMap(ctx, ha); err != nil {
		return err
	}

	return u.reconcileStatefulSet(ctx, ha)
}

func (u *HermesAgentUseCase) reconcileConfigMap(ctx context.Context, ha *agentsv1alpha1.HermesAgent) error {
	cmName := ha.GetConfigMapName()
	cm, err := u.kube.GetConfigMap(ctx, GetConfigMapParam{
		NamespacedName: types.NamespacedName{Name: cmName, Namespace: ha.Namespace},
	})
	if err != nil {
		return err
	}

	desired, err := u.buildConfigMap(ha)
	if err != nil {
		return err
	}

	if cm != nil {
		desired.ResourceVersion = cm.ResourceVersion
		return u.kube.UpdateConfigMap(ctx, UpdateConfigMapParam{ConfigMap: desired})
	}

	return u.kube.CreateConfigMapOwnedByHermesAgent(ctx, CreateConfigMapOfHermesAgentParam{
		HermesAgent: ha,
		ConfigMap:   desired,
	})
}

func (u *HermesAgentUseCase) buildConfigMap(ha *agentsv1alpha1.HermesAgent) (*corev1.ConfigMap, error) {
	data := map[string]string{}
	if hc := ha.GetHermes().GetConfig(); hc != nil {
		yamlBytes, err := sigsyaml.JSONToYAML(hc.Raw)
		if err != nil {
			return nil, fmt.Errorf("converting config to YAML: %w", err)
		}
		data["config.yaml"] = string(yamlBytes)
	}
	if hw := ha.GetHermes().GetWorkspace(); hw != nil {
		for path, content := range hw.Files {
			key := "workspace." + strings.ReplaceAll(path, "/", workspacePathSeparator)
			data[key] = content
		}
	}
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ha.GetConfigMapName(),
			Namespace: ha.Namespace,
		},
		Data: data,
	}, nil
}

func (u *HermesAgentUseCase) reconcileStatefulSet(ctx context.Context, ha *agentsv1alpha1.HermesAgent) error {
	sts, err := u.kube.GetStatefulSet(ctx, GetStatefulSetParam{
		NamespacedName: types.NamespacedName{Name: ha.Name, Namespace: ha.Namespace},
	})
	if err != nil {
		return err
	}

	desired := u.buildStatefulSet(ha)

	if sts != nil {
		desired.ResourceVersion = sts.ResourceVersion
		return u.kube.UpdateStatefulSet(ctx, UpdateStatefulSetParam{StatefulSet: desired})
	}

	return u.kube.CreateStatefulSetOwnedByHermesAgent(ctx, CreateStatefulSetOfHermesAgentParam{
		HermesAgent: ha,
		StatefulSet: desired,
	})
}

func (u *HermesAgentUseCase) buildStatefulSet(ha *agentsv1alpha1.HermesAgent) *appsv1.StatefulSet {
	replicas := int32(1)

	// The config hash annotation is used to trigger a rolling update of the StatefulSet when the config changes.
	cm, _ := u.buildConfigMap(ha)
	configHash := configMapDataHash(cm.Data)

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ha.Name,
			Namespace: ha.Namespace,
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": ha.Name},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": ha.Name},
					Annotations: map[string]string{
						domain + "/config-hash": configHash,
					},
				},
			},
		},
	}

	sts = u.buildHermesContainer(ha, sts)

	return sts
}

// buildHermesContainer populates the StatefulSet with all resources driven by the hermes spec:
// the main hermes-agent container (env, envFrom), init containers for config and workspace,
// and volumes/PVCs for persistence, bootstrap config, and shared memory.
func (u *HermesAgentUseCase) buildHermesContainer(ha *agentsv1alpha1.HermesAgent, sts *appsv1.StatefulSet) *appsv1.StatefulSet {
	sts = sts.DeepCopy()
	sizeLimit := resource.MustParse("1Gi")

	initContainers := []corev1.Container{}
	containers := []corev1.Container{
		{
			Name:            "hermes-agent",
			Image:           "nousresearch/hermes-agent:latest",
			ImagePullPolicy: corev1.PullIfNotPresent,
			Args:            []string{"gateway", "run"},
			WorkingDir:      "/opt/hermes",
			Env: append([]corev1.EnvVar{
				{Name: "HERMES_HOME", Value: "/opt/data"},
				{Name: "HOME", Value: "/opt/data/home"},
			}, ha.GetHermes().GetEnv()...),
			EnvFrom: ha.GetHermes().GetEnvFrom(),
			Resources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("2"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("500m"),
					corev1.ResourceMemory: resource.MustParse("1Gi"),
				},
			},
			VolumeMounts: []corev1.VolumeMount{
				{Name: "dshm", MountPath: "/dev/shm"},
				{Name: "data", MountPath: "/opt/data"},
			},
		},
	}
	volumes := []corev1.Volume{
		{
			Name: "dshm",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{
					Medium:    corev1.StorageMediumMemory,
					SizeLimit: &sizeLimit,
				},
			},
		},
		{
			Name: "bootstrap",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: ha.GetConfigMapName()},
				},
			},
		},
	}
	pvc := []corev1.PersistentVolumeClaim{}

	// persistence: existingClaim > enabled PVC > emptyDir fallback.
	hp := ha.GetHermes().GetPersistence()
	if ec := hp.GetExistingClaim(); ec != "" {
		volumes = append(volumes, corev1.Volume{
			Name: "data",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: ec,
				},
			},
		})
	} else if hp != nil && hp.Enabled {
		pvc = append(pvc, corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "data"},
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
			Name:         "data",
			VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
		})
	}

	// config: init container copies config.yaml from the bootstrap ConfigMap to the data volume.
	if hc := ha.GetHermes().GetConfig(); hc != nil {
		initContainers = append(initContainers, corev1.Container{
			Name:            "init-config",
			Image:           "nousresearch/hermes-agent:latest",
			ImagePullPolicy: corev1.PullIfNotPresent,
			Command:         []string{"/bin/sh", "-ec"},
			Args: []string{`set -eu
mkdir -p "/opt/data/home"
cp "/bootstrap/config.yaml" "/opt/data/config.yaml"
`},
			Env: []corev1.EnvVar{
				{Name: "HERMES_HOME", Value: "/opt/data"},
			},
			VolumeMounts: []corev1.VolumeMount{
				{Name: "data", MountPath: "/opt/data"},
				{Name: "bootstrap", MountPath: "/bootstrap", ReadOnly: true},
			},
		})
	}

	// workspace: init container copies workspace files from the bootstrap ConfigMap.
	// ConfigMap keys use the format "workspace.<path>" with "/" replaced by "--".
	if hw := ha.GetHermes().GetWorkspace(); hw != nil && len(hw.Files) > 0 {
		initContainers = append(initContainers, corev1.Container{
			Name:            "init-workspace",
			Image:           "nousresearch/hermes-agent:latest",
			ImagePullPolicy: corev1.PullIfNotPresent,
			Command:         []string{"/bin/sh", "-ec"},
			Args: []string{fmt.Sprintf(`set -eu
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
    fi
  done < "$MANIFEST_FILE"
fi

for f in /bootstrap/workspace.*; do
  [ -f "$f" ] || continue
  relpath=$(basename "$f" | sed 's/^workspace\.//' | sed 's/%s/\//g')
  target="/opt/data/$relpath"
  mkdir -p "$(dirname "$target")"
  cp "$f" "$target"
  UPDATED_MANIFEST="$UPDATED_MANIFEST$relpath
"
done

printf '%%s' "$UPDATED_MANIFEST" > "$MANIFEST_FILE"
`, workspacePathSeparator, workspacePathSeparator)},
			Env: []corev1.EnvVar{
				{Name: "HERMES_HOME", Value: "/opt/data"},
			},
			VolumeMounts: []corev1.VolumeMount{
				{Name: "data", MountPath: "/opt/data"},
				{Name: "bootstrap", MountPath: "/bootstrap", ReadOnly: true},
			},
		})
	}

	// skills: init container installs/uninstalls skills via the hermes CLI.
	if skills := ha.GetHermes().GetSkills(); len(skills) > 0 {
		// Build the new manifest content (skill names, one per line).
		names := make([]string, len(skills))
		for i, s := range skills {
			names[i] = skillName(s)
		}
		newManifest := strings.Join(names, "\n")

		// Build install commands.
		var installCmds strings.Builder
		for _, s := range skills {
			installCmds.WriteString("hermes skills install --yes")
			if s.Category != "" {
				installCmds.WriteString(" --category ")
				installCmds.WriteString(s.Category)
			}
			if s.Name != "" {
				installCmds.WriteString(" --name ")
				installCmds.WriteString(s.Name)
			}
			if s.Force {
				installCmds.WriteString(" --force")
			}
			installCmds.WriteString(" ")
			installCmds.WriteString(s.Identifier)
			installCmds.WriteString("\n")
		}

		script := fmt.Sprintf(`set -eu
UPDATED_MANIFEST=%q
MANIFEST_FILE="/opt/data/.hermes-agent-operator/skills"
mkdir -p "/opt/data/.hermes-agent-operator"

if [ -f "$MANIFEST_FILE" ]; then
  while IFS= read -r managed; do
    [ -z "$managed" ] && continue
    if ! printf '%%s\n' "$UPDATED_MANIFEST" | grep -qxF "$managed"; then
      hermes skills uninstall "$managed"
    fi
  done < "$MANIFEST_FILE"
fi

%s
printf '%%s\n' "$UPDATED_MANIFEST" > "$MANIFEST_FILE"
`, newManifest, installCmds.String())

		initContainers = append(initContainers, corev1.Container{
			Name:            "init-skills",
			Image:           "nousresearch/hermes-agent:latest",
			ImagePullPolicy: corev1.PullIfNotPresent,
			Command:         []string{"/bin/sh", "-ec"},
			Args:            []string{script},
			Env: []corev1.EnvVar{
				{Name: "HERMES_HOME", Value: "/opt/data"},
			},
			VolumeMounts: []corev1.VolumeMount{
				{Name: "data", MountPath: "/opt/data"},
			},
		})
	}

	sts.Spec.Template.Spec.InitContainers = append(sts.Spec.Template.Spec.InitContainers, initContainers...)
	sts.Spec.Template.Spec.Containers = append(sts.Spec.Template.Spec.Containers, containers...)
	sts.Spec.Template.Spec.Volumes = append(sts.Spec.Template.Spec.Volumes, volumes...)
	sts.Spec.VolumeClaimTemplates = append(sts.Spec.VolumeClaimTemplates, pvc...)

	return sts
}
