package usecase

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"maps"
	"path"
	"regexp"
	"slices"

	agentsv1alpha1 "hermeum/hermes-agent-operator/api/v1alpha1"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

// egressUID/egressGID are the uid/gid of the iron-proxy image user (distroless
// nonroot).
const (
	egressUID = int64(65532)
	egressGID = int64(65532)
)

// egressTokenKey matches the egress Secret keys that hold proxy tokens: env
// var names, unlike proxy.yaml.
var egressTokenKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// errEgressInputsPending reports that a Secret the egress wiring reads is not
// visible yet, typically one the egress reconcilers created moments ago that
// has not reached the cache. Building the pod without it would roll it out
// without the agent's proxy tokens, then restart it once they appear.
var errEgressInputsPending = errors.New("egress Secrets not visible yet")

// egressPodInputs reads what the egress wiring needs beyond the spec, from the
// Secrets the egress reconcilers already ensured: the env var names that carry
// proxy tokens, and a hash of everything the pod reads only at start (the CA
// certificate, the egress Secret and the real keys) so that rotating any of
// them restarts it. It returns errEgressInputsPending while any of those
// Secrets is missing.
func (u *HermesAgentUseCase) egressPodInputs(ctx context.Context, ha *agentsv1alpha1.HermesAgent) (tokenNames []string, hash string, err error) {
	caName, caCertKey, _ := ha.GetEgressCASource()

	// The real keys only enter a truncated SHA-256 over every input, which
	// does not disclose them.
	h := sha256.New()
	for _, src := range []struct {
		name string
		keys []string // nil hashes every key
	}{
		{name: caName, keys: []string{caCertKey}},
		{name: ha.GetEgressSecretName()},
		{name: ha.GetEgress().GetCredentialsSecretName()},
	} {
		if src.name == "" {
			continue
		}
		secret, err := u.kube.GetSecret(ctx, GetSecretParam{NamespacedName: types.NamespacedName{Name: src.name, Namespace: ha.Namespace}})
		if err != nil {
			return nil, "", fmt.Errorf("reading egress input Secret %q: %w", src.name, err)
		}
		if secret == nil {
			return nil, "", fmt.Errorf("%w: %q", errEgressInputsPending, src.name)
		}
		keys := src.keys
		if keys == nil {
			keys = slices.Sorted(maps.Keys(secret.Data))
		}
		for _, k := range keys {
			_, _ = fmt.Fprintf(h, "%s\x00%s\x00%s\x00", src.name, k, secret.Data[k])
		}
		if src.name == ha.GetEgressSecretName() {
			for _, k := range keys {
				if egressTokenKey.MatchString(k) {
					tokenNames = append(tokenNames, k)
				}
			}
		}
	}
	return tokenNames, fmt.Sprintf("%x", h.Sum(nil))[:16], nil
}

// buildEgressContainerSecurityContext returns a hardened security context for
// the iron-proxy container. Every listener binds an unprivileged loopback
// port, so no capability is needed.
func buildEgressContainerSecurityContext() *corev1.SecurityContext {
	ape := false
	rot := true
	uid := egressUID
	gid := egressGID
	rofs := true
	return &corev1.SecurityContext{
		AllowPrivilegeEscalation: &ape,
		RunAsNonRoot:             &rot,
		RunAsUser:                &uid,
		RunAsGroup:               &gid,
		ReadOnlyRootFilesystem:   &rofs,
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{"ALL"},
		},
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}
}

// buildEgressContainer adds the iron-proxy sidecar, which reads the real keys
// from the credentials Secret. The agent reaches it through the proxy env vars
// and proxy tokens every profile's .env carries (see buildHermesContainer).
// The agent's env gets the tokens as well, appended last, so that a real key
// passed through spec.hermes.env or envFrom never reaches its environment.
func buildEgressContainer(ha *agentsv1alpha1.HermesAgent, sts *appsv1.StatefulSet, tokenNames []string, inputsHash string) *appsv1.StatefulSet {
	sts = sts.DeepCopy()

	eg := ha.GetEgress()
	if !eg.IsEnabled() {
		return sts
	}

	const (
		egressContainerName = "iron-proxy"
		egressConfigVolume  = "egress-config"
		egressAgentCAVolume = "egress-agent-ca"
	)
	caName, caCertKey, caKeyKey := ha.GetEgressCASource()

	if sts.Spec.Template.Annotations == nil {
		sts.Spec.Template.Annotations = map[string]string{}
	}
	sts.Spec.Template.Annotations[domain+"/egress-hash"] = inputsHash

	if c := findContainer(sts, hermesContainerName); c != nil {
		for _, name := range tokenNames {
			c.Env = append(c.Env, corev1.EnvVar{
				Name: name,
				ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: ha.GetEgressSecretName()},
					Key:                  name,
				}},
			})
		}
		c.VolumeMounts = append(c.VolumeMounts, corev1.VolumeMount{
			Name:      egressAgentCAVolume,
			MountPath: path.Dir(agentsv1alpha1.EgressAgentCAPath),
			ReadOnly:  true,
		})
	}

	// proxy.yaml reads each real key from the env var named like its key in
	// the credentials Secret.
	var envFrom []corev1.EnvFromSource
	if name := eg.GetCredentialsSecretName(); name != "" {
		envFrom = []corev1.EnvFromSource{{SecretRef: &corev1.SecretEnvSource{
			LocalObjectReference: corev1.LocalObjectReference{Name: name},
		}}}
	}

	sts.Spec.Template.Spec.Containers = append(sts.Spec.Template.Spec.Containers, corev1.Container{
		Name:            egressContainerName,
		Image:           eg.GetImage(),
		ImagePullPolicy: corev1.PullIfNotPresent,
		Args:            []string{"-config", agentsv1alpha1.EgressConfigDir + "/" + agentsv1alpha1.EgressProxyConfigKey},
		EnvFrom:         envFrom,
		Env:             eg.GetEnv(),
		Resources:       eg.GetResources(),
		SecurityContext: buildEgressContainerSecurityContext(),
		VolumeMounts: []corev1.VolumeMount{
			{Name: egressConfigVolume, MountPath: agentsv1alpha1.EgressConfigDir, ReadOnly: true},
		},
	})

	defaultMode := int32(0o444)
	sts.Spec.Template.Spec.Volumes = append(sts.Spec.Template.Spec.Volumes,
		corev1.Volume{
			// Sidecar side: proxy.yaml plus the CA certificate and key, under
			// the fixed names proxy.yaml refers to.
			Name: egressConfigVolume,
			VolumeSource: corev1.VolumeSource{
				Projected: &corev1.ProjectedVolumeSource{
					Sources: []corev1.VolumeProjection{
						{Secret: &corev1.SecretProjection{
							LocalObjectReference: corev1.LocalObjectReference{Name: ha.GetEgressSecretName()},
							Items:                []corev1.KeyToPath{{Key: agentsv1alpha1.EgressProxyConfigKey, Path: agentsv1alpha1.EgressProxyConfigKey}},
						}},
						{Secret: &corev1.SecretProjection{
							LocalObjectReference: corev1.LocalObjectReference{Name: caName},
							Items: []corev1.KeyToPath{
								{Key: caCertKey, Path: agentsv1alpha1.EgressCAManagedCertKey},
								{Key: caKeyKey, Path: agentsv1alpha1.EgressCAManagedKeyKey},
							},
						}},
					},
				},
			},
		},
		corev1.Volume{
			// Agent side: the CA certificate only, never its key.
			Name: egressAgentCAVolume,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  caName,
					Items:       []corev1.KeyToPath{{Key: caCertKey, Path: path.Base(agentsv1alpha1.EgressAgentCAPath)}},
					DefaultMode: &defaultMode,
				},
			},
		},
	)

	return sts
}
