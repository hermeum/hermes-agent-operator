package usecase

import (
	"fmt"
	"strconv"

	agentsv1alpha1 "hermeum/hermes-agent-operator/api/v1alpha1"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// buildInitContainerSecurityContext returns a security context with hermes user.
func buildInitContainerSecurityContext() *corev1.SecurityContext {
	// 10000 is hermes user and group ID in the official container image.
	uid, gid := int64(10000), int64(10000)
	rnt := true
	ape := false

	return &corev1.SecurityContext{
		RunAsNonRoot:             &rnt,
		RunAsUser:                &uid,
		RunAsGroup:               &gid,
		AllowPrivilegeEscalation: &ape,
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{"ALL"},
		},
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}
}

// buildHermesContainer populates the StatefulSet with all resources driven by the hermes spec:
// the main hermes-agent container (env, envFrom), init containers for config and workspace,
// and volumes/PVCs for persistence, bootstrap config, and shared memory.
//
//nolint:gocyclo // inherent to the breadth of init containers and volume wiring
func buildHermesContainer(ha *agentsv1alpha1.HermesAgent, sts *appsv1.StatefulSet) *appsv1.StatefulSet {
	const (
		hermesHomeMount       = "/opt/data"
		hermesDSHMVolume      = "dshm"
		hermesDSHMMount       = "/dev/shm"
		hermesTmpVolume       = "tmp"
		hermesTmpMount        = "/tmp"
		hermesBootstrapVolume = "bootstrap"
		hermesBootstrapMount  = "/bootstrap"
	)

	initContainer := func(name, script string) corev1.Container {
		return corev1.Container{
			Name:            name,
			Image:           ha.GetHermes().GetImage(),
			ImagePullPolicy: corev1.PullIfNotPresent,
			Command:         []string{"/bin/sh", "-ec"},
			Args:            []string{script},
			// A failing step is diagnosed from `status.conditions`, so fall back
			// to the tail of the container's log when the script itself wrote
			// no termination message.
			TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
			Env: append([]corev1.EnvVar{
				{Name: "HERMES_HOME", Value: hermesHomeMount},
				{Name: "HOME", Value: hermesHomeMount + "/home"},
			}, ha.GetHermes().GetEnv()...),
			EnvFrom:         ha.GetHermes().GetEnvFrom(),
			SecurityContext: buildInitContainerSecurityContext(),
			VolumeMounts: []corev1.VolumeMount{
				{Name: hermesHomeVolume, MountPath: hermesHomeMount},
				{Name: hermesBootstrapVolume, MountPath: hermesBootstrapMount, ReadOnly: true},
				{Name: hermesTmpVolume, MountPath: hermesTmpMount},
			},
		}
	}

	sts = sts.DeepCopy()
	sizeLimit := resource.MustParse("1Gi")
	apiServer := ha.GetHermes().GetAPIServer()

	initContainers := []corev1.Container{}
	container := corev1.Container{
		Name:            hermesContainerName,
		Image:           ha.GetHermes().GetImage(),
		ImagePullPolicy: corev1.PullIfNotPresent,
		Args:            []string{"gateway", "run"},
		WorkingDir:      "/opt/hermes",
		Ports:           ha.GetHermes().GetPorts(),
		Env: append([]corev1.EnvVar{
			{Name: "HERMES_HOME", Value: hermesHomeMount},
			{Name: "HOME", Value: hermesHomeMount + "/home"},
			{Name: "PYTHONPATH", Value: hermesHomeMount + "/.python-packages"},
			{Name: "NODE_PATH", Value: hermesHomeMount + "/.npm-packages/lib/node_modules"},
		}, ha.GetHermes().GetEnv()...),
		EnvFrom:   ha.GetHermes().GetEnvFrom(),
		Resources: ha.GetHermes().GetResources(),
		// The Hermes container intentionally starts as root so s6-overlay's /init (PID 1) can
		// remap UID/GID and chown /opt/data before dropping to the hermes user (UID 10000)
		// via s6-setuidgid. This prevents setting runAsNonRoot, allowPrivilegeEscalation, and readOnlyRootFilesystem.
		// RuntimeDefault seccomp is the strongest isolation available without changing the upstream image.
		SecurityContext: &corev1.SecurityContext{
			SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
		},
		LivenessProbe: ha.GetHermes().GetProbes().GetLiveness().GetProbe(hermesHealthCheckCommand, corev1.Probe{
			InitialDelaySeconds: 15, PeriodSeconds: 20, TimeoutSeconds: 5, FailureThreshold: 3,
		}),
		ReadinessProbe: ha.GetHermes().GetProbes().GetReadiness().GetProbe(hermesHealthCheckCommand, corev1.Probe{
			InitialDelaySeconds: 5, PeriodSeconds: 10, TimeoutSeconds: 5, FailureThreshold: 3,
		}),
		StartupProbe: ha.GetHermes().GetProbes().GetStartup().GetProbe(hermesHealthCheckCommand, corev1.Probe{
			InitialDelaySeconds: 0, PeriodSeconds: 10, TimeoutSeconds: 5, FailureThreshold: 10,
		}),
		VolumeMounts: append([]corev1.VolumeMount{
			{Name: hermesDSHMVolume, MountPath: hermesDSHMMount},
			{Name: hermesHomeVolume, MountPath: hermesHomeMount},
			{Name: hermesTmpVolume, MountPath: hermesTmpMount},
		}, ha.GetExtraVolumeMounts()...),
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

	// API server configuration
	if apiServer.IsEnabled() {
		container.Ports = append(container.Ports, corev1.ContainerPort{
			Name:          apiServer.GetPortName(),
			ContainerPort: apiServer.GetPort(),
			Protocol:      corev1.ProtocolTCP,
		})
	}

	// Webhook configuration
	webhook := ha.GetHermes().GetWebhook()
	if webhook.IsEnabled() {
		container.Ports = append(container.Ports, corev1.ContainerPort{
			Name:          webhook.GetPortName(),
			ContainerPort: webhook.GetPort(),
			Protocol:      corev1.ProtocolTCP,
		})
	}

	// persistence: existingClaim > existingSnapshot PVC > enabled PVC > emptyDir fallback.
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
	} else if es := hp.GetExistingSnapshot(); es != "" {
		volumes = append(volumes, corev1.Volume{
			Name: hermesHomeVolume,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: agentsv1alpha1.RestoredPVCName(es),
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

	// ensures the data volume is owned by the hermes user
	if ha.GetHermes().ShouldInitChownData() {
		initContainers = append(initContainers, corev1.Container{
			Name:            "init-chown-data",
			Image:           ha.GetHermes().GetImage(),
			ImagePullPolicy: corev1.PullIfNotPresent,
			Command:         []string{"/bin/sh", "-ec"},
			Args:            []string{"chown -R 10000:10000 /opt/data"},
			// See the hermes init containers: a failure is diagnosed from
			// `status.conditions`, which needs the tail of this container's log.
			TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
			Env: append([]corev1.EnvVar{
				{Name: "HERMES_HOME", Value: hermesHomeMount},
				{Name: "HOME", Value: hermesHomeMount + "/home"},
			}, ha.GetHermes().GetEnv()...),
			EnvFrom: ha.GetHermes().GetEnvFrom(),
			VolumeMounts: []corev1.VolumeMount{
				{Name: hermesHomeVolume, MountPath: hermesHomeMount},
			},
		})
	}

	// A single consolidated init container configures the default profile:
	// config → workspace → dotenv → packages → plugins → skills → bundles →
	// crons → stale-profile cleanup. Steps run in subshells so a skipped step
	// (e.g. "packages up-to-date, exit 0") cannot abort the remaining steps.
	//
	// dotenv: operator-managed env vars (API_SERVER_*, WEBHOOK_*, SEARXNG_URL,
	// CAMOFOX_URL) are stored as keys in the Hermes ConfigMap and Secret, then
	// mounted into the init container with Items filters so the existing
	// `for f in .../*` loop dumps them as KEY=VALUE lines — the same mechanism
	// as user workspace.dotEnv. Operator mounts come first so user dotEnv keys
	// override on collision (user wins).
	var defaultSteps []string
	var defaultMounts []corev1.VolumeMount

	// config: copy config.yaml from the bootstrap ConfigMap to the data volume.
	if ha.GetHermes().HasConfigDocument() {
		defaultSteps = append(defaultSteps, buildConfigScript(hermesDefaultProfile))
	}

	// workspace: copy workspace files from the bootstrap ConfigMap.
	// ConfigMap keys use the format "profile.default.workspace.<path>" with "/" replaced by "--".
	defaultSteps = append(defaultSteps, buildWorkspaceScript(hermesDefaultProfile))

	// dotenv: write the default profile .env.
	de := ha.GetHermes().GetWorkspace().GetDotEnv()
	operatorItems := buildOperatorDotEnvItems(ha)
	operatorSecretItems := buildOperatorDotEnvSecretItems(ha)
	if de != nil || len(operatorItems) > 0 || len(operatorSecretItems) > 0 {
		var mountPaths []string
		var mounts []corev1.VolumeMount

		// Operator-managed volumes first (user keys override on collision).
		if len(operatorItems) > 0 {
			const dotenvConfigMapVolume = "hermes-operator-dotenv-configmap"
			const dotenvConfigMapMount = "/hermes-operator-dotenv-configmap"
			volumes = append(volumes, corev1.Volume{
				Name: dotenvConfigMapVolume,
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: ha.GetHermesName()},
						Items:                operatorItems,
					},
				},
			})
			mounts = append(mounts, corev1.VolumeMount{Name: dotenvConfigMapVolume, MountPath: dotenvConfigMapMount, ReadOnly: true})
			mountPaths = append(mountPaths, dotenvConfigMapMount)
		}

		if len(operatorSecretItems) > 0 {
			const dotenvVolume = "hermes-operator-dotenv-secret"
			const dotenvMount = "/hermes-operator-dotenv-secret"
			volumes = append(volumes, corev1.Volume{
				Name: dotenvVolume,
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: ha.GetHermesName(),
						Items:      operatorSecretItems,
					},
				},
			})
			mounts = append(mounts, corev1.VolumeMount{Name: dotenvVolume, MountPath: dotenvMount, ReadOnly: true})
			mountPaths = append(mountPaths, dotenvMount)
		}

		if de != nil {
			// ConfigMaps: singular first, then plural in order (last-wins on
			// key collision; Secrets override ConfigMaps).
			if de.ConfigMapRef != nil {
				const dotenvConfigMapVolume = "hermes-dotenv-configmap"
				const dotenvConfigMapMount = "/hermes-dotenv-configmap"
				volumes = append(volumes, corev1.Volume{
					Name: dotenvConfigMapVolume,
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{Name: de.ConfigMapRef.Name},
						},
					},
				})
				mounts = append(mounts, corev1.VolumeMount{Name: dotenvConfigMapVolume, MountPath: dotenvConfigMapMount, ReadOnly: true})
				mountPaths = append(mountPaths, dotenvConfigMapMount)
			}

			for i, ref := range de.ConfigMapRefs {
				volName := fmt.Sprintf("hermes-dotenv-configmap-%d", i)
				mountPath := "/hermes-dotenv-configmap-" + strconv.Itoa(i)
				volumes = append(volumes, corev1.Volume{
					Name: volName,
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{Name: ref.Name},
						},
					},
				})
				mounts = append(mounts, corev1.VolumeMount{Name: volName, MountPath: mountPath, ReadOnly: true})
				mountPaths = append(mountPaths, mountPath)
			}

			// Secrets: singular first, then plural in order.
			if de.SecretRef != nil {
				const dotenvVolume = "hermes-dotenv-secret"
				const dotenvMount = "/hermes-dotenv-secret"
				volumes = append(volumes, corev1.Volume{
					Name: dotenvVolume,
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: de.SecretRef.Name,
						},
					},
				})
				mounts = append(mounts, corev1.VolumeMount{Name: dotenvVolume, MountPath: dotenvMount, ReadOnly: true})
				mountPaths = append(mountPaths, dotenvMount)
			}

			for i, ref := range de.SecretRefs {
				volName := fmt.Sprintf("hermes-dotenv-secret-%d", i)
				mountPath := "/hermes-dotenv-secret-" + strconv.Itoa(i)
				volumes = append(volumes, corev1.Volume{
					Name: volName,
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{SecretName: ref.Name},
					},
				})
				mounts = append(mounts, corev1.VolumeMount{Name: volName, MountPath: mountPath, ReadOnly: true})
				mountPaths = append(mountPaths, mountPath)
			}
		}

		defaultSteps = append(defaultSteps, buildDotEnvScript(hermesDefaultProfile, mountPaths...))
		defaultMounts = mounts
	}

	// python-packages: install desired packages into $HERMES_HOME/.python-packages.
	defaultSteps = append(defaultSteps, buildPythonPackagesScript(ha.GetHermes().GetPackages().GetPip()))

	// npm-packages: install desired packages into $HERMES_HOME/.npm-packages.
	defaultSteps = append(defaultSteps, buildNPMPackagesScript(ha.GetHermes().GetPackages().GetNpm()))

	// plugins: install desired plugins and remove stale ones.
	defaultSteps = append(defaultSteps, buildPluginsScript(hermesDefaultProfile, ha.GetHermes().GetPlugins()))

	// skills: install/uninstall skills via the hermes CLI.
	defaultSteps = append(defaultSteps, buildSkillsScript(hermesDefaultProfile, ha.GetHermes().GetSkills()))

	// bundles: reconcile bundles via the hermes CLI.
	defaultSteps = append(defaultSteps, buildBundlesScript(hermesDefaultProfile, ha.GetHermes().GetBundles()))

	// crons: reconcile scheduled jobs via the hermes CLI.
	defaultSteps = append(defaultSteps, buildCronsScript(hermesDefaultProfile, ha.GetHermes().GetCrons()))

	// profiles cleanup: remove named profiles no longer desired and write the
	// desired profiles manifest. Profile creation happens in the per-profile
	// init containers below (after the default profile is fully configured, so
	// --clone copies complete state).
	profiles := ha.GetHermes().GetProfiles()
	if len(profiles) > 0 {
		defaultSteps = append(defaultSteps, buildProfilesCleanupScript(profiles))
	}

	initContainers = append(initContainers, func() corev1.Container {
		ic := initContainer("init-hermes", combineInitSteps(defaultSteps...))
		ic.VolumeMounts = append(ic.VolumeMounts, defaultMounts...)
		return ic
	}())

	// One init container per named profile: create the profile, then configure
	// it (config → workspace → dotenv → plugins → skills → bundles → crons).
	sidecarItemsForProfiles := buildProfileSidecarDotEnvItems(ha)
	for _, name := range ha.GetHermes().GetSortedProfileNames() {
		profile := profiles[name]
		var steps []string
		var profileMounts []corev1.VolumeMount

		steps = append(steps, buildProfileCreationScript(name, profile.Clone))

		if profile.Config.HasDocument() {
			steps = append(steps, buildConfigScript(name))
		}

		steps = append(steps, buildWorkspaceScript(name))

		// dotenv: SEARXNG_URL / CAMOFOX_URL point at shared sidecars and are
		// written to every named profile's .env. API_SERVER_* / WEBHOOK_* are
		// intentionally excluded — they belong to the default profile's
		// gateway.
		if de := profile.Workspace.GetDotEnv(); de != nil || len(sidecarItemsForProfiles) > 0 {
			var mountPaths []string

			// Operator sidecar keys first (user keys override on collision).
			if len(sidecarItemsForProfiles) > 0 {
				volName := "hermes-operator-dotenv-profile-" + name
				mountPath := "/hermes-operator-dotenv-profile-" + name
				volumes = append(volumes, corev1.Volume{
					Name: volName,
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{Name: ha.GetHermesName()},
							Items:                sidecarItemsForProfiles,
						},
					},
				})
				profileMounts = append(profileMounts, corev1.VolumeMount{Name: volName, MountPath: mountPath, ReadOnly: true})
				mountPaths = append(mountPaths, mountPath)
			}

			if de != nil {
				// ConfigMaps: singular first, then plural in order.
				if de.ConfigMapRef != nil {
					volName := "hermes-dotenv-configmap-profile-" + name
					mountPath := "/hermes-dotenv-configmap-profile-" + name
					volumes = append(volumes, corev1.Volume{
						Name: volName,
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: de.ConfigMapRef.Name},
							},
						},
					})
					profileMounts = append(profileMounts, corev1.VolumeMount{Name: volName, MountPath: mountPath, ReadOnly: true})
					mountPaths = append(mountPaths, mountPath)
				}

				for i, ref := range de.ConfigMapRefs {
					volName := "hermes-dotenv-configmap-profile-" + name + "-" + strconv.Itoa(i)
					mountPath := "/hermes-dotenv-configmap-profile-" + name + "-" + strconv.Itoa(i)
					volumes = append(volumes, corev1.Volume{
						Name: volName,
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: ref.Name},
							},
						},
					})
					profileMounts = append(profileMounts, corev1.VolumeMount{Name: volName, MountPath: mountPath, ReadOnly: true})
					mountPaths = append(mountPaths, mountPath)
				}

				// Secrets: singular first, then plural in order.
				if de.SecretRef != nil {
					volName := "hermes-dotenv-secret-profile-" + name
					mountPath := "/hermes-dotenv-secret-profile-" + name
					volumes = append(volumes, corev1.Volume{
						Name: volName,
						VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{SecretName: de.SecretRef.Name},
						},
					})
					profileMounts = append(profileMounts, corev1.VolumeMount{Name: volName, MountPath: mountPath, ReadOnly: true})
					mountPaths = append(mountPaths, mountPath)
				}

				for i, ref := range de.SecretRefs {
					volName := "hermes-dotenv-secret-profile-" + name + "-" + strconv.Itoa(i)
					mountPath := "/hermes-dotenv-secret-profile-" + name + "-" + strconv.Itoa(i)
					volumes = append(volumes, corev1.Volume{
						Name: volName,
						VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{SecretName: ref.Name},
						},
					})
					profileMounts = append(profileMounts, corev1.VolumeMount{Name: volName, MountPath: mountPath, ReadOnly: true})
					mountPaths = append(mountPaths, mountPath)
				}
			}

			steps = append(steps, buildDotEnvScript(name, mountPaths...))
		}

		steps = append(steps, buildPluginsScript(name, profile.Plugins))
		steps = append(steps, buildSkillsScript(name, profile.Skills))
		steps = append(steps, buildBundlesScript(name, profile.Bundles))
		steps = append(steps, buildCronsScript(name, profile.Crons))

		ic := initContainer("init-profile-"+name, combineInitSteps(steps...))
		ic.VolumeMounts = append(ic.VolumeMounts, profileMounts...)
		initContainers = append(initContainers, ic)
	}

	// initScripts: user-provided scripts run as init containers after all managed ones.
	for _, is := range ha.GetHermes().GetInitScripts() {
		initContainers = append(initContainers, corev1.Container{
			Name:            is.Name,
			Image:           ha.GetHermes().GetImage(),
			ImagePullPolicy: corev1.PullIfNotPresent,
			Command:         []string{"/bin/sh", "-ec"},
			Args:            []string{is.Script},
			// See the operator-managed init containers above.
			TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
			Env: append([]corev1.EnvVar{
				{Name: "HERMES_HOME", Value: hermesHomeMount},
				{Name: "HOME", Value: hermesHomeMount + "/home"},
			}, ha.GetHermes().GetEnv()...),
			EnvFrom:         ha.GetHermes().GetEnvFrom(),
			SecurityContext: buildInitContainerSecurityContext(),
			VolumeMounts: []corev1.VolumeMount{
				{Name: hermesHomeVolume, MountPath: hermesHomeMount},
				{Name: hermesTmpVolume, MountPath: hermesTmpMount},
			},
		})
	}

	sts.Spec.Template.Spec.InitContainers = append(sts.Spec.Template.Spec.InitContainers, initContainers...)
	sts.Spec.Template.Spec.Containers = append(sts.Spec.Template.Spec.Containers, container)
	sts.Spec.Template.Spec.Volumes = append(sts.Spec.Template.Spec.Volumes, volumes...)
	sts.Spec.VolumeClaimTemplates = append(sts.Spec.VolumeClaimTemplates, pvc...)

	return sts
}

// buildOperatorDotEnvItems returns the ConfigMap Items filter for operator-managed
// non-secret env vars written to the default profile's .env: API_SERVER_ENABLED,
// API_SERVER_HOST, API_SERVER_PORT, optional API_SERVER_CORS_ORIGINS,
// WEBHOOK_ENABLED, WEBHOOK_PORT, SEARXNG_URL, CAMOFOX_URL.
func buildOperatorDotEnvItems(ha *agentsv1alpha1.HermesAgent) []corev1.KeyToPath {
	var items []corev1.KeyToPath
	if apiServer := ha.GetHermes().GetAPIServer(); apiServer.IsEnabled() {
		items = append(items,
			corev1.KeyToPath{Key: "API_SERVER_ENABLED", Path: "API_SERVER_ENABLED"},
			corev1.KeyToPath{Key: "API_SERVER_HOST", Path: "API_SERVER_HOST"},
			corev1.KeyToPath{Key: "API_SERVER_PORT", Path: "API_SERVER_PORT"},
		)
		if origins := apiServer.GetCORSOrigins(); len(origins) > 0 {
			items = append(items, corev1.KeyToPath{Key: "API_SERVER_CORS_ORIGINS", Path: "API_SERVER_CORS_ORIGINS"})
		}
	}
	if webhook := ha.GetHermes().GetWebhook(); webhook.IsEnabled() {
		items = append(items,
			corev1.KeyToPath{Key: "WEBHOOK_ENABLED", Path: "WEBHOOK_ENABLED"},
			corev1.KeyToPath{Key: "WEBHOOK_PORT", Path: "WEBHOOK_PORT"},
		)
	}
	if ha.GetSearXNG().IsEnabled() {
		items = append(items, corev1.KeyToPath{Key: "SEARXNG_URL", Path: "SEARXNG_URL"})
	}
	if ha.GetCamofox().IsEnabled() {
		items = append(items, corev1.KeyToPath{Key: "CAMOFOX_URL", Path: "CAMOFOX_URL"})
	}
	return items
}

// buildOperatorDotEnvSecretItems returns the Secret Items filter for
// operator-managed secret env vars written to the default profile's .env:
// API_SERVER_KEY and/or WEBHOOK_SECRET.
func buildOperatorDotEnvSecretItems(ha *agentsv1alpha1.HermesAgent) []corev1.KeyToPath {
	var items []corev1.KeyToPath
	if ha.GetHermes().GetAPIServer().IsEnabled() {
		items = append(items, corev1.KeyToPath{Key: "API_SERVER_KEY", Path: "API_SERVER_KEY"})
	}
	if ha.GetHermes().GetWebhook().IsEnabled() {
		items = append(items, corev1.KeyToPath{Key: "WEBHOOK_SECRET", Path: "WEBHOOK_SECRET"})
	}
	return items
}

// buildProfileSidecarDotEnvItems returns the ConfigMap Items filter for the
// shared sidecar URLs written to every named profile's .env. API_SERVER_* and
// WEBHOOK_* are excluded — they belong to the default profile's gateway.
func buildProfileSidecarDotEnvItems(ha *agentsv1alpha1.HermesAgent) []corev1.KeyToPath {
	var items []corev1.KeyToPath
	if ha.GetSearXNG().IsEnabled() {
		items = append(items, corev1.KeyToPath{Key: "SEARXNG_URL", Path: "SEARXNG_URL"})
	}
	if ha.GetCamofox().IsEnabled() {
		items = append(items, corev1.KeyToPath{Key: "CAMOFOX_URL", Path: "CAMOFOX_URL"})
	}
	return items
}
