package usecase

import (
	agentsv1alpha1 "hermeum/hermes-agent-operator/api/v1alpha1"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

// buildCamofoxContainerSecurityContext returns a hardened security context for the Camofox container.
// RunAsNonRoot is not set because the image is designed to run as root (data at /root/.cache/camoufox).
func buildCamofoxContainerSecurityContext() *corev1.SecurityContext {
	ape := false
	return &corev1.SecurityContext{
		AllowPrivilegeEscalation: &ape,
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{"ALL"},
		},
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}
}

func buildCamofoxContainer(ha *agentsv1alpha1.HermesAgent, sts *appsv1.StatefulSet) *appsv1.StatefulSet {
	sts = sts.DeepCopy()

	cx := ha.GetCamofox()
	if !cx.IsEnabled() {
		return sts
	}

	const (
		camofoxContainerName = "camofox"
		camofoxDataVolume    = "camofox-data"
		camofoxDataMount     = "/root/.camofox"
	)

	// Inject CAMOFOX_URL into the hermes-agent container env so that the browser tool can find it.
	if c := findContainer(sts, hermesContainerName); c != nil {
		c.Env = append(c.Env, corev1.EnvVar{Name: "CAMOFOX_URL", Value: agentsv1alpha1.CamofoxURL})
	}

	sts.Spec.Template.Spec.Containers = append(sts.Spec.Template.Spec.Containers, corev1.Container{
		Name:            camofoxContainerName,
		Image:           cx.GetImage(),
		ImagePullPolicy: corev1.PullIfNotPresent,
		Env:             cx.GetEnv(),
		Resources:       cx.GetResources(),
		SecurityContext: buildCamofoxContainerSecurityContext(),
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
