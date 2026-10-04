package usecase

import (
	"fmt"

	agentsv1alpha1 "hermeum/hermes-agent-operator/api/v1alpha1"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

// buildSearXNGContainerSecurityContext returns a hardened security context for the SearXNG container.
// The image runs as UID/GID 977 (defined via --chown=977:977 in the Dockerfile).
func buildSearXNGContainerSecurityContext() *corev1.SecurityContext {
	ape := false
	rot := true
	uid := searxngUID
	gid := searxngGID
	return &corev1.SecurityContext{
		AllowPrivilegeEscalation: &ape,
		RunAsNonRoot:             &rot,
		RunAsUser:                &uid,
		RunAsGroup:               &gid,
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{"ALL"},
		},
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}
}

// buildSearXNGChownSecurityContext returns the security context for the root
// chown step of init-searxng-config. The upstream searxng entrypoint
// requires its volumes to be owned by searxng:searxng and can only repair
// ownership itself when running as root; because the runtime container is
// hardened to run as non-root uid 977, the operator prepares the volumes
// beforehand. Running as root is required for chown, so runAsNonRoot is not
// set and the CHOWN capability is granted explicitly (all others dropped).
func buildSearXNGChownSecurityContext() *corev1.SecurityContext {
	ape := false
	uid := int64(0)
	gid := int64(0)
	return &corev1.SecurityContext{
		AllowPrivilegeEscalation: &ape,
		RunAsUser:                &uid,
		RunAsGroup:               &gid,
		Capabilities: &corev1.Capabilities{
			Add:  []corev1.Capability{"CHOWN"},
			Drop: []corev1.Capability{"ALL"},
		},
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}
}

func buildSearXNGContainer(ha *agentsv1alpha1.HermesAgent, sts *appsv1.StatefulSet) *appsv1.StatefulSet {
	sts = sts.DeepCopy()

	sx := ha.GetSearXNG()
	if !sx.IsEnabled() {
		return sts
	}

	const (
		searxngContainerName   = "searxng"
		searxngPortName        = "searxng"
		searxngPort            = int32(8080)
		searxngConfigVolume    = "searxng-config"
		searxngConfigMount     = "/etc/searxng"
		searxngBootstrapVolume = "searxng-bootstrap"
		searxngBootstrapMount  = "/bootstrap-searxng"
		searxngCacheVolume     = "searxng-cache"
		searxngCacheMount      = "/var/cache/searxng"
	)

	// Inject SEARXNG_URL into the hermes-agent container env so that the web_search tool can find it.
	if c := findContainer(sts, hermesContainerName); c != nil {
		c.Env = append(c.Env, corev1.EnvVar{Name: "SEARXNG_URL", Value: agentsv1alpha1.SearXNGURL})
	}

	// init container: copy config files from the read-only ConfigMap bootstrap volume into the
	// writable emptyDir at /etc/searxng so SearXNG can write runtime files alongside them,
	// then chown both SearXNG volumes to the searxng user. The upstream entrypoint requires
	// searxng:searxng ownership and cannot repair it itself (it runs as non-root uid 977),
	// while kubelet creates emptyDir/PVC volumes as root:root (#98). The copy runs as root
	// (files land root-owned) so the chown must run after it to cover the copied files too;
	// this mirrors upstream's FORCE_OWNERSHIP chown -R, which is skipped when the entrypoint
	// runs as non-root. See buildSearXNGChownSecurityContext for the hardened root context.
	// Both steps share a single /bin/sh -ec script operand: with Command ["sh","-ec"] and
	// multiple Args elements, only the first is the script — later elements become $0 and
	// are never executed.
	//
	// The chown also covers persistence.existingClaim volumes; it re-runs on every pod
	// start, which is cheap for the small config emptyDir but can take a while on a large
	// existing claim — accepted because searxng refuses to start otherwise (see #98).
	sts.Spec.Template.Spec.InitContainers = append(sts.Spec.Template.Spec.InitContainers, corev1.Container{
		Name:            "init-searxng-config",
		Image:           sx.GetImage(),
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{"/bin/sh", "-ec"},
		Args: []string{
			fmt.Sprintf(
				"cp -r /bootstrap-searxng/. /etc/searxng/ && chown -R %d:%d /etc/searxng /var/cache/searxng",
				searxngUID, searxngGID,
			),
		},
		VolumeMounts: []corev1.VolumeMount{
			{Name: searxngBootstrapVolume, MountPath: searxngBootstrapMount, ReadOnly: true},
			{Name: searxngConfigVolume, MountPath: searxngConfigMount},
			{Name: searxngCacheVolume, MountPath: searxngCacheMount},
		},
		SecurityContext: buildSearXNGChownSecurityContext(),
	})

	sts.Spec.Template.Spec.Containers = append(sts.Spec.Template.Spec.Containers, corev1.Container{
		Name:            searxngContainerName,
		Image:           sx.GetImage(),
		ImagePullPolicy: corev1.PullIfNotPresent,
		Ports: []corev1.ContainerPort{
			{Name: searxngPortName, ContainerPort: searxngPort, Protocol: corev1.ProtocolTCP},
		},
		Env: append([]corev1.EnvVar{
			{Name: "SEARXNG_BASE_URL", Value: agentsv1alpha1.SearXNGURL + "/"},
			{
				Name: "SEARXNG_SECRET",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: ha.GetSearXNGName()},
						Key:                  "SEARXNG_SECRET",
					},
				},
			},
		}, sx.GetEnv()...),
		Resources: sx.GetResources(),
		VolumeMounts: []corev1.VolumeMount{
			{Name: searxngConfigVolume, MountPath: searxngConfigMount},
			{Name: searxngCacheVolume, MountPath: searxngCacheMount},
		},
		SecurityContext: buildSearXNGContainerSecurityContext(),
	})

	sts.Spec.Template.Spec.Volumes = append(sts.Spec.Template.Spec.Volumes,
		corev1.Volume{
			// writable emptyDir that holds the copied config files at runtime.
			Name:         searxngConfigVolume,
			VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
		},
		corev1.Volume{
			// read-only ConfigMap source, copied into searxng-config by the init container.
			Name: searxngBootstrapVolume,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: ha.GetSearXNGName()},
				},
			},
		},
	)

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
		sts.Spec.Template.Spec.Volumes = append(sts.Spec.Template.Spec.Volumes, corev1.Volume{
			Name: searxngCacheVolume,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: ha.GetSearXNGName()},
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
