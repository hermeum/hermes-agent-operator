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

package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	agentsv1alpha1 "hermeum/hermes-agent-operator/api/v1alpha1"
	"hermeum/hermes-agent-operator/internal/infras"
)

var _ = Describe("HermesAgent Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default", // TODO(user):Modify as needed
		}
		hermesagent := &agentsv1alpha1.HermesAgent{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind HermesAgent")
			err := k8sClient.Get(ctx, typeNamespacedName, hermesagent)
			if err != nil && errors.IsNotFound(err) {
				resource := &agentsv1alpha1.HermesAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					// TODO(user): Specify other spec details if needed.
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &agentsv1alpha1.HermesAgent{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance HermesAgent")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &HermesAgentReconciler{
				Client:    k8sClient,
				Scheme:    k8sClient.Scheme(),
				Telemetry: infras.NewPrometheusTelemetry(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			// TODO(user): Add more specific assertions depending on your controller's reconciliation logic.
			// Example: If you expect a certain status condition after reconciliation, verify it here.
		})
	})
})

var _ = Describe("Mapping a ConfigMap to the agents that read a config from it", func() {
	const namespace = "default"

	ctx := context.Background()
	reconciler := &HermesAgentReconciler{}

	// agent creates a HermesAgent whose default profile and named profile read
	// their config from defaultRef and profileRef, either of which may be "".
	agent := func(name, defaultRef, profileRef string) {
		hermes := &agentsv1alpha1.Hermes{}
		if defaultRef != "" {
			hermes.Config = &agentsv1alpha1.HermesConfig{
				ConfigMapRef: &agentsv1alpha1.HermesConfigMapKeyRef{Name: defaultRef},
			}
		}
		if profileRef != "" {
			hermes.Profiles = map[string]agentsv1alpha1.HermesProfile{
				"coder": {Config: &agentsv1alpha1.HermesProfileConfig{
					ConfigMapRef: &agentsv1alpha1.HermesConfigMapKeyRef{Name: profileRef},
				}},
			}
		}
		resource := &agentsv1alpha1.HermesAgent{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			Spec:       agentsv1alpha1.HermesAgentSpec{Hermes: hermes},
		}
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		DeferCleanup(func() {
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
	}

	configMap := func(name, ns string) *corev1.ConfigMap {
		return &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}}
	}

	names := func(requests []reconcile.Request) []string {
		out := make([]string, 0, len(requests))
		for _, r := range requests {
			out = append(out, r.Name)
		}
		return out
	}

	BeforeEach(func() {
		reconciler.Client = k8sClient
		reconciler.Scheme = k8sClient.Scheme()
	})

	It("enqueues every agent that references it, from the default or a named profile", func() {
		agent("maps-default", "shared-config", "")
		agent("maps-profile", "", "shared-config")
		agent("maps-neither", "other-config", "other-config")

		Expect(names(reconciler.agentsReferencingConfigMap(ctx, configMap("shared-config", namespace)))).
			To(ConsistOf("maps-default", "maps-profile"))
	})

	It("enqueues nothing for a ConfigMap no agent references", func() {
		agent("maps-unrelated", "some-config", "")

		Expect(reconciler.agentsReferencingConfigMap(ctx, configMap("nobody-reads-this", namespace))).To(BeEmpty())
	})

	It("enqueues nothing for a ConfigMap in another namespace", func() {
		agent("maps-elsewhere", "shared-config", "")

		Expect(reconciler.agentsReferencingConfigMap(ctx, configMap("shared-config", "kube-system"))).To(BeEmpty())
	})
})
