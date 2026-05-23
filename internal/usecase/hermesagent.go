package usecase

import (
	"context"
	"fmt"
	agentsv1alpha1 "noahingh/hermes-agent-operator/api/v1alpha1"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	sigsyaml "sigs.k8s.io/yaml"
)

const workspacePathSeparator = "--"

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
	if hc := ha.GetHermesConfig(); hc != nil {
		yamlBytes, err := sigsyaml.JSONToYAML(hc.Raw)
		if err != nil {
			return nil, fmt.Errorf("converting config to YAML: %w", err)
		}
		data["config.yaml"] = string(yamlBytes)
	}
	if hw := ha.GetHermesWorkspace(); hw != nil {
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
	sizeLimit := resource.MustParse("1Gi")

	initContainers := []corev1.Container{}
	containers := []corev1.Container{
		// The main container runs the Hermes Agent.
		{
			Name:            "hermes-agent",
			Image:           "nousresearch/hermes-agent:latest",
			ImagePullPolicy: corev1.PullIfNotPresent,
			Args:            []string{"gateway", "run"},
			WorkingDir:      "/opt/hermes",
			Env: append([]corev1.EnvVar{
				{Name: "HERMES_HOME", Value: "/opt/data"},
				{Name: "HOME", Value: "/opt/data/home"},
			}, ha.GetHermesEnv()...),
			EnvFrom: ha.GetHermesEnvFrom(),
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

	// If persistence is enabled, use a PersistentVolumeClaim. Otherwise, use an EmptyDir volume.
	hp := ha.GetHermesPersistence()
	if hp != nil && hp.Enabled {
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

	// If config is provided, add an init container to copy config.yaml to the shared volume.
	if hc := ha.GetHermesConfig(); hc != nil {
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

	// If workspace files are provided, add an init container to copy them to the shared volume.
	// The file keys in the ConfigMap are in the format "workspace.<path>" where "/" in the path is replaced with "--" to be a valid ConfigMap key.
	if hw := ha.GetHermesWorkspace(); hw != nil && len(hw.Files) > 0 {
		initContainers = append(initContainers, corev1.Container{
			Name:            "init-workspace",
			Image:           "nousresearch/hermes-agent:latest",
			ImagePullPolicy: corev1.PullIfNotPresent,
			Command:         []string{"/bin/sh", "-ec"},
			Args: []string{fmt.Sprintf(`set -eu
mkdir -p "/opt/data/home"
for f in /bootstrap/workspace.*; do
  [ -f "$f" ] || continue
  relpath=$(basename "$f" | sed 's/^workspace\.//' | sed 's/%s/\//g')
  target="/opt/data/home/$relpath"
  mkdir -p "$(dirname "$target")"
  cp "$f" "$target"
done
`, workspacePathSeparator)},
			Env: []corev1.EnvVar{
				{Name: "HERMES_HOME", Value: "/opt/data"},
			},
			VolumeMounts: []corev1.VolumeMount{
				{Name: "data", MountPath: "/opt/data"},
				{Name: "bootstrap", MountPath: "/bootstrap", ReadOnly: true},
			},
		})
	}

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
				},
				Spec: corev1.PodSpec{
					InitContainers: initContainers,
					Containers:     containers,
					Volumes:        volumes,
				},
			},
			VolumeClaimTemplates: pvc,
		},
	}

	return sts
}
