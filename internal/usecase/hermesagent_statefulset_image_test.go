package usecase

import (
	"testing"

	agentsv1alpha1 "hermeum/hermes-agent-operator/api/v1alpha1"
)

const testImageDigest = "@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestBuildStatefulSetImageDigest(t *testing.T) {
	const digestRepo = "nousresearch/hermes-agent" + testImageDigest
	ha := minimalHA()
	ha.Spec.Hermes = &agentsv1alpha1.Hermes{
		Image: &agentsv1alpha1.HermesImage{Repository: digestRepo},
	}
	sts := buildStatefulSet(ha)

	t.Run("hermes-agent container runs the pinned image", func(t *testing.T) {
		c := findContainer(sts, hermesContainerName)
		if c == nil {
			t.Fatal("expected hermes-agent container")
		}
		if c.Image != digestRepo {
			t.Errorf("hermes-agent image = %q, want %q", c.Image, digestRepo)
		}
	})

	t.Run("operator-managed init containers run the pinned image", func(t *testing.T) {
		if len(sts.Spec.Template.Spec.InitContainers) == 0 {
			t.Fatal("expected init containers")
		}
		for _, ic := range sts.Spec.Template.Spec.InitContainers {
			if ic.Image != digestRepo {
				t.Errorf("init container %s image = %q, want %q", ic.Name, ic.Image, digestRepo)
			}
		}
	})

	t.Run("init scripts run the pinned image", func(t *testing.T) {
		ha := minimalHA()
		ha.Spec.Hermes = &agentsv1alpha1.Hermes{
			Image: &agentsv1alpha1.HermesImage{Repository: digestRepo},
			InitScripts: []agentsv1alpha1.HermesInitScript{
				{Name: "wait-for-db", Script: "true"},
			},
		}
		sts := buildStatefulSet(ha)
		ic := findInitContainer(sts, "wait-for-db")
		if ic == nil {
			t.Fatal("expected wait-for-db init container")
		}
		if ic.Image != digestRepo {
			t.Errorf("wait-for-db image = %q, want %q", ic.Image, digestRepo)
		}
	})
}

func TestBuildStatefulSetSidecarImageDigest(t *testing.T) {
	const (
		searxngDigestRepo = "searxng/searxng" + testImageDigest
		camofoxDigestRepo = "ghcr.io/jo-inc/camofox-browser" + testImageDigest
	)

	ha := minimalHA()
	ha.Spec.SearXNG = &agentsv1alpha1.SearXNG{
		Enabled: true,
		Image:   &agentsv1alpha1.SearXNGImage{Repository: searxngDigestRepo},
	}
	ha.Spec.Camofox = &agentsv1alpha1.Camofox{
		Enabled: true,
		Image:   agentsv1alpha1.CamofoxImageSpec{Repository: camofoxDigestRepo},
	}
	sts := buildStatefulSet(ha)

	t.Run("searxng container runs the pinned image", func(t *testing.T) {
		c := findContainer(sts, "searxng")
		if c == nil {
			t.Fatal("expected searxng container")
		}
		if c.Image != searxngDigestRepo {
			t.Errorf("searxng image = %q, want %q", c.Image, searxngDigestRepo)
		}
	})

	t.Run("init-searxng-config runs the pinned image", func(t *testing.T) {
		ic := findInitContainer(sts, "init-searxng-config")
		if ic == nil {
			t.Fatal("expected init-searxng-config init container")
		}
		if ic.Image != searxngDigestRepo {
			t.Errorf("init-searxng-config image = %q, want %q", ic.Image, searxngDigestRepo)
		}
	})

	t.Run("camofox container runs the pinned image", func(t *testing.T) {
		c := findContainer(sts, "camofox")
		if c == nil {
			t.Fatal("expected camofox container")
		}
		if c.Image != camofoxDigestRepo {
			t.Errorf("camofox image = %q, want %q", c.Image, camofoxDigestRepo)
		}
	})
}
