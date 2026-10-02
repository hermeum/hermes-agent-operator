package usecase

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	agentsv1alpha1 "hermeum/hermes-agent-operator/api/v1alpha1"
	"maps"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	sigsyaml "sigs.k8s.io/yaml"
)

func (u *HermesAgentUseCase) reconcileHermesConfigMap(ctx context.Context, ha *agentsv1alpha1.HermesAgent) (result ctrl.Result, err error) {
	defer func() {
		if err != nil {
			err = u.markReconcileFailed(ctx, ha, condReasonHermesConfigMapFailed, err)
		}
	}()

	cmName := ha.GetHermesName()
	cm, err := u.kube.GetConfigMap(ctx, GetConfigMapParam{
		NamespacedName: types.NamespacedName{Name: cmName, Namespace: ha.Namespace},
	})
	if err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}

	refs, err := u.resolveConfigDocuments(ctx, ha)
	if err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}

	desired, err := buildHermesConfigMap(ha, refs)
	if err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}

	if cm != nil {
		if configMapDataEqual(desired, cm) {
			return ctrl.Result{}, nil
		}
		desired.ResourceVersion = cm.ResourceVersion
		err := u.kube.UpdateConfigMapOwnedByHermesAgent(ctx, UpdateConfigMapParam{HermesAgent: ha, ConfigMap: desired})
		if err != nil {
			return ctrl.Result{RequeueAfter: 30 * time.Second}, err
		}
		u.tel.Debug(ctx, "Hermes ConfigMap updated")
		return ctrl.Result{}, nil
	}

	err = u.kube.CreateConfigMapOwnedByHermesAgent(ctx, CreateConfigMapOfHermesAgentParam{HermesAgent: ha, ConfigMap: desired})
	if err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}
	u.tel.Debug(ctx, "Hermes ConfigMap created")
	ha.Status.ManagedResources.HermesConfigMap = ha.GetHermesName()
	if err := u.kube.UpdateHermesAgentStatus(ctx, UpdateHermesAgentStatusParam{HermesAgent: ha}); err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}
	return ctrl.Result{}, nil
}

func configMapDataEqual(a, b *corev1.ConfigMap) bool {
	return maps.Equal(a.Data, b.Data)
}

// `resolvedConfigDocuments` holds the config documents read from the
// `ConfigMap` objects the spec references, converted to JSON so they flow
// through the same defaulting as an inline raw config.
type resolvedConfigDocuments struct {
	// Default is the default profile's document, nil when the default profile
	// declares its config inline or declares none.
	Default []byte
	// Profiles holds each named profile's document, keyed by profile name.
	Profiles map[string][]byte
}

// `resolveConfigDocuments` reads every `config.configMapRef` the spec declares.
// A `ConfigMap` or key that does not exist fails the reconcile: the agent's
// config
// is the user's declared intent, and quietly starting the agent without it
// would be worse than waiting for the `ConfigMap` to appear.
// `validateConfigSources` reports every config that sets both `raw` and
// `configMapRef`.  One pass names them all, so a spec with several invalid
// fields takes one round trip to correct instead of one per field.
func validateConfigSources(ha *agentsv1alpha1.HermesAgent) error {
	var both []string
	if h := ha.GetHermes(); h.GetConfigMapRef() != nil && h.GetConfig() != nil {
		both = append(both, "hermes.config")
	}
	profiles := ha.GetHermes().GetProfiles()
	for _, name := range sortedProfileNames(profiles) {
		cfg := profiles[name].Config
		if cfg.GetConfigMapRef() != nil && cfg.GetRaw() != nil {
			both = append(both, fmt.Sprintf("profile %q config", name))
		}
	}
	if len(both) > 0 {
		return fmt.Errorf("raw and configMapRef are mutually exclusive, and both are set in %s",
			strings.Join(both, ", "))
	}
	return nil
}

func (u *HermesAgentUseCase) resolveConfigDocuments(ctx context.Context, ha *agentsv1alpha1.HermesAgent) (resolvedConfigDocuments, error) {
	var out resolvedConfigDocuments

	if err := validateConfigSources(ha); err != nil {
		return out, err
	}

	if ref := ha.GetHermes().GetConfigMapRef(); ref != nil {
		doc, err := u.readConfigDocument(ctx, ha.Namespace, ref)
		if err != nil {
			return out, fmt.Errorf("resolving hermes.config.configMapRef: %w", err)
		}
		out.Default = doc
	}

	for _, name := range sortedProfileNames(ha.GetHermes().GetProfiles()) {
		ref := ha.GetHermes().GetProfiles()[name].Config.GetConfigMapRef()
		if ref == nil {
			continue
		}
		doc, err := u.readConfigDocument(ctx, ha.Namespace, ref)
		if err != nil {
			return out, fmt.Errorf("resolving profile %q config.configMapRef: %w", name, err)
		}
		if out.Profiles == nil {
			out.Profiles = map[string][]byte{}
		}
		out.Profiles[name] = doc
	}

	return out, nil
}

// `readConfigDocument` reads one referenced key and converts the YAML document
// it holds to JSON.
func (u *HermesAgentUseCase) readConfigDocument(ctx context.Context, namespace string, ref *agentsv1alpha1.HermesConfigMapKeyRef) ([]byte, error) {
	cm, err := u.kube.GetConfigMap(ctx, GetConfigMapParam{
		NamespacedName: types.NamespacedName{Name: ref.Name, Namespace: namespace},
	})
	if err != nil {
		return nil, err
	}
	if cm == nil {
		return nil, fmt.Errorf("ConfigMap %q not found", ref.Name)
	}
	doc, ok := cm.Data[ref.GetKey()]
	if !ok {
		return nil, fmt.Errorf("ConfigMap %q has no key %q", ref.Name, ref.GetKey())
	}
	j, err := sigsyaml.YAMLToJSON([]byte(doc))
	if err != nil {
		return nil, fmt.Errorf("parsing key %q of ConfigMap %q: %w", ref.GetKey(), ref.Name, err)
	}
	// A document holding nothing but whitespace or comments is a valid
	// `config.yaml` with every setting defaulted, but it parses to JSON null,
	// which the defaulting cannot merge into.
	if isJSONNull(j) {
		return []byte("{}"), nil
	}
	return j, nil
}

// isJSONNull reports whether a parsed document is the JSON null literal.
func isJSONNull(document []byte) bool {
	return string(bytes.TrimSpace(document)) == "null"
}

// applySearXNGConfigDefaults applies default values to the SearXNG config if they are not set by the user.
func applySearXNGConfigDefaults(raw []byte) ([]byte, error) {
	cfg := map[string]any{}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshaling config: %w", err)
	}
	if cfg == nil {
		// Unmarshaling JSON null sets the map to nil, which every assignment
		// below would panic on.
		cfg = map[string]any{}
	}

	web, _ := cfg["web"].(map[string]any)
	if web == nil {
		web = map[string]any{}
		cfg["web"] = web
	}
	if _, ok := web["search_backend"]; !ok {
		web["search_backend"] = "searxng"
	}

	out, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshaling config: %w", err)
	}
	return out, nil
}

// applyMultiplexProfilesDefault injects gateway.multiplex_profiles: true into the Hermes
// config when named profiles are declared, unless already set.
func applyMultiplexProfilesDefault(raw []byte) ([]byte, error) {
	cfg := map[string]any{}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshaling config: %w", err)
	}
	if cfg == nil {
		// Unmarshaling JSON null sets the map to nil, which every assignment
		// below would panic on.
		cfg = map[string]any{}
	}

	gateway, _ := cfg["gateway"].(map[string]any)
	if gateway == nil {
		gateway = map[string]any{}
		cfg["gateway"] = gateway
	}
	if _, ok := gateway["multiplex_profiles"]; !ok {
		gateway["multiplex_profiles"] = true
	}

	out, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshaling config: %w", err)
	}
	return out, nil
}

// applyCamofoxConfigDefaults injects browser.camofox.managed_persistence: true into
// the Hermes config when Camofox managed persistence is active, unless already set.
func applyCamofoxConfigDefaults(raw []byte) ([]byte, error) {
	cfg := map[string]any{}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshaling config: %w", err)
	}
	if cfg == nil {
		// Unmarshaling JSON null sets the map to nil, which every assignment
		// below would panic on.
		cfg = map[string]any{}
	}

	browser, _ := cfg["browser"].(map[string]any)
	if browser == nil {
		browser = map[string]any{}
		cfg["browser"] = browser
	}
	camofox, _ := browser["camofox"].(map[string]any)
	if camofox == nil {
		camofox = map[string]any{}
		browser["camofox"] = camofox
	}
	if _, ok := camofox["managed_persistence"]; !ok {
		camofox["managed_persistence"] = true
	}

	out, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshaling config: %w", err)
	}
	return out, nil
}

func buildHermesConfigMap(ha *agentsv1alpha1.HermesAgent, refs resolvedConfigDocuments) (*corev1.ConfigMap, error) {
	data := map[string]string{}

	// Collect the default profile's config, from `configMapRef` or raw; SearXNG
	// and Camofox defaults only apply when the user has provided a config, but
	// multiplex_profiles must also be set when profiles exist even if no
	// explicit config was given.
	defaultRaw := refs.Default
	if defaultRaw == nil {
		if hc := ha.GetHermes().GetConfig(); hc != nil {
			defaultRaw = hc.Raw
		}
	}
	if defaultRaw != nil {
		if ha.GetSearXNG().IsEnabled() {
			var err error
			defaultRaw, err = applySearXNGConfigDefaults(defaultRaw)
			if err != nil {
				return nil, err
			}
		}
		cx := ha.GetCamofox()
		if cx.IsEnabled() && cx.GetPersistence().IsEnabled() && cx.GetPersistence().GetExistingClaim() == "" {
			var err error
			defaultRaw, err = applyCamofoxConfigDefaults(defaultRaw)
			if err != nil {
				return nil, err
			}
		}
	}

	if len(ha.GetHermes().GetProfiles()) > 0 {
		if defaultRaw == nil {
			defaultRaw = []byte("{}")
		}
		var err error
		defaultRaw, err = applyMultiplexProfilesDefault(defaultRaw)
		if err != nil {
			return nil, err
		}
	}

	if defaultRaw != nil {
		yamlBytes, err := sigsyaml.JSONToYAML(defaultRaw)
		if err != nil {
			return nil, err
		}
		data["profile.default.config.yaml"] = string(yamlBytes)
	}

	if hw := ha.GetHermes().GetWorkspace(); hw != nil {
		for path, content := range hw.Files {
			key := "profile.default.workspace." + strings.ReplaceAll(path, "/", hermesWorkspacePathSeparator)
			data[key] = content
		}
	}

	for name, profile := range ha.GetHermes().GetProfiles() {
		profileRaw := refs.Profiles[name]
		if profileRaw == nil {
			if raw := profile.Config.GetRaw(); raw != nil {
				profileRaw = raw.Raw
			}
		}
		if profileRaw != nil {
			yamlBytes, err := sigsyaml.JSONToYAML(profileRaw)
			if err != nil {
				return nil, err
			}
			data["profile."+name+".config.yaml"] = string(yamlBytes)
		}
		if profile.Workspace != nil {
			for path, content := range profile.Workspace.Files {
				key := "profile." + name + ".workspace." + strings.ReplaceAll(path, "/", hermesWorkspacePathSeparator)
				data[key] = content
			}
		}
	}

	// Operator-managed env vars for the dotenv init container. These keys are
	// mounted (with an Items filter) into the consolidated init-hermes container so they land in .env.
	// Non-secret values go in the ConfigMap; secret values (API_SERVER_KEY,
	// WEBHOOK_SECRET) are in the operator Secret and mounted separately.
	if apiServer := ha.GetHermes().GetAPIServer(); apiServer.IsEnabled() {
		data["API_SERVER_ENABLED"] = "true"
		// The default bind is 127.0.0.1, which is unreachable from outside
		// the pod; bind all interfaces so the Service can route to it.
		data["API_SERVER_HOST"] = "0.0.0.0"
		data["API_SERVER_PORT"] = strconv.Itoa(int(apiServer.GetPort()))
		if origins := apiServer.GetCORSOrigins(); len(origins) > 0 {
			data["API_SERVER_CORS_ORIGINS"] = strings.Join(origins, ",")
		}
	}
	if webhook := ha.GetHermes().GetWebhook(); webhook.IsEnabled() {
		data["WEBHOOK_ENABLED"] = "true"
		data["WEBHOOK_PORT"] = strconv.Itoa(int(webhook.GetPort()))
	}
	if ha.GetSearXNG().IsEnabled() {
		data["SEARXNG_URL"] = searxngURL
	}
	if ha.GetCamofox().IsEnabled() {
		data["CAMOFOX_URL"] = camofoxURL
	}

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ha.GetHermesName(),
			Namespace: ha.Namespace,
			Labels:    resourceLabels(ha),
		},
		Data: data,
	}, nil
}
