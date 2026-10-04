package usecase

import (
	"maps"

	corev1 "k8s.io/api/core/v1"
)

// configMapDataEqual reports whether two ConfigMaps carry the same data, so
// reconcilers can skip no-op updates. It is shared by every ConfigMap
// reconciler instead of living in one resource's file.
func configMapDataEqual(a, b *corev1.ConfigMap) bool {
	return maps.Equal(a.Data, b.Data)
}
