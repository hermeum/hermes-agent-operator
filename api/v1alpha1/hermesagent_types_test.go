/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import "testing"

// testDigest is a syntactically valid sha256 digest suffix for image-pinning tests.
const testDigest = "@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestGetImage(t *testing.T) {
	hermes := func(repo, tag string) string {
		return (&Hermes{Image: &HermesImage{Repository: repo, Tag: tag}}).GetImage()
	}
	searxng := func(repo, tag string) string {
		return (&SearXNG{Image: &SearXNGImage{Repository: repo, Tag: tag}}).GetImage()
	}
	camofox := func(repo, tag string) string {
		return (&Camofox{Image: CamofoxImageSpec{Repository: repo, Tag: tag}}).GetImage()
	}

	tests := []struct {
		name string
		got  func() string
		want string
	}{
		{"hermes defaults when spec image is nil", func() string {
			return (&Hermes{}).GetImage()
		}, "nousresearch/hermes-agent:latest"},
		{"hermes defaults when receiver is nil", func() string {
			var h *Hermes
			return h.GetImage()
		}, "nousresearch/hermes-agent:latest"},
		{"hermes custom repository and tag", func() string {
			return hermes("example.com/agent", "v1.2.3")
		}, "example.com/agent:v1.2.3"},
		{"hermes repository only keeps default tag", func() string {
			return hermes("example.com/agent", "")
		}, "example.com/agent:latest"},
		{"hermes tag only keeps default repository", func() string {
			return hermes("", "v1.2.3")
		}, "nousresearch/hermes-agent:v1.2.3"},
		{"hermes pinned by digest returns repository verbatim", func() string {
			return hermes("nousresearch/hermes-agent"+testDigest, "")
		}, "nousresearch/hermes-agent" + testDigest},
		{"hermes digest wins over explicit tag", func() string {
			return hermes("nousresearch/hermes-agent"+testDigest, "v1.2.3")
		}, "nousresearch/hermes-agent" + testDigest},
		{"searxng defaults when receiver is nil", func() string {
			var s *SearXNG
			return s.GetImage()
		}, "searxng/searxng:latest"},
		{"searxng custom repository and tag", func() string {
			return searxng("example.com/searxng", "v1.2.3")
		}, "example.com/searxng:v1.2.3"},
		{"searxng pinned by digest returns repository verbatim", func() string {
			return searxng("searxng/searxng"+testDigest, "")
		}, "searxng/searxng" + testDigest},
		{"searxng digest wins over explicit tag", func() string {
			return searxng("searxng/searxng"+testDigest, "v9")
		}, "searxng/searxng" + testDigest},
		{"camofox defaults when receiver is nil", func() string {
			var c *Camofox
			return c.GetImage()
		}, "ghcr.io/jo-inc/camofox-browser:latest"},
		{"camofox custom repository and tag", func() string {
			return camofox("example.com/camofox", "v1.2.3")
		}, "example.com/camofox:v1.2.3"},
		{"camofox pinned by digest returns repository verbatim", func() string {
			return camofox("ghcr.io/jo-inc/camofox-browser"+testDigest, "")
		}, "ghcr.io/jo-inc/camofox-browser" + testDigest},
		{"camofox digest wins over explicit tag", func() string {
			return camofox("ghcr.io/jo-inc/camofox-browser"+testDigest, "v9")
		}, "ghcr.io/jo-inc/camofox-browser" + testDigest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.got(); got != tt.want {
				t.Errorf("GetImage() = %q, want %q", got, tt.want)
			}
		})
	}
}
