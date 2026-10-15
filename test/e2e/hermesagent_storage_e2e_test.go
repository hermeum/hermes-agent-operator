//go:build e2e
// +build e2e

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

package e2e

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	"hermeum/hermes-agent-operator/test/utils"
)

// agentNamespace is the namespace the scenario agents are isolated in. The
// operator's --watch-namespace flags scope its cache and RBAC to the e2e
// namespaces, so a scenario cannot touch agents outside them; every
// HermesAgent of this suite lives in this namespace.
const agentNamespace = "hermes-agent-e2e"

// hermesDataVolume is the StatefulSet volumeClaimTemplate / volume name the
// operator provisions the agent data volume under.
const hermesDataVolume = "hermes-data"

// applyStdin pipes rendered YAML into kubectl apply -f -.
func applyStdin(manifest string) error {
	cmd := exec.Command("kubectl", "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(manifest)
	_, err := utils.Run(cmd)
	return err
}

// getManagerWatchNamespaces reads the --watch-namespace arguments of the
// controller manager — the namespace scoping guarantee the suite asserts.
func getManagerWatchNamespaces() string {
	cmd := exec.Command("kubectl", "get", "deploy", "hermes-agent-operator-controller-manager",
		"-n", namespace,
		"-o", "jsonpath={.spec.template.spec.containers[0].args[*]}")
	out, err := utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred(), "Failed to read the manager args")
	return out
}

// waitForManagerDeployed blocks until the Manager suite's deployment is
// available. Ginkgo shuffles top-level Describe containers, so this suite
// can run before the Manager suite has deployed the controller; its own
// BeforeAll then fails on the missing namespace/deployment. Waiting here
// keeps the scenario order-independent.
func waitForManagerDeployed() {
	Eventually(func(g Gomega) {
		cmd := exec.Command("kubectl", "get", "deploy", "hermes-agent-operator-controller-manager",
			"-n", namespace, "-o", "jsonpath={.status.availableReplicas}")
		out, err := utils.Run(cmd)
		g.Expect(err).NotTo(HaveOccurred(), "manager namespace/deployment not found yet")
		g.Expect(out).To(Equal("1"))
	}, 5*time.Minute, 5*time.Second).Should(Succeed())
}

var _ = Describe("HermesAgent storage", Ordered, func() {
	const agentName = "storage-133"

	BeforeAll(func() {
		// The scenario needs the operator running: in CI the Manager
		// Describe (e2e_test.go) deploys it in its own BeforeAll. Ginkgo
		// shuffles top-level containers, so wait here for the manager
		// namespace and deployment regardless of which suite runs first.
		By("waiting for the controller-manager deployment from the Manager suite")
		waitForManagerDeployed()

		By("creating the isolated agent namespace")
		cmd := exec.Command("kubectl", "create", "ns", agentNamespace, "--dry-run=client", "-o", "yaml")
		out, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to render the agent namespace")
		Expect(applyStdin(out)).To(Succeed(), "Failed to create the agent namespace")

		// The hermes image remaps its own UID at start (s6-overlay), so the
		// restricted Pod Security label the manager namespace carries would
		// forbid every agent pod; the agent namespace stays unlabeled.
		By("labeling the agent namespace permissive for the agent pods")
		cmd = exec.Command("kubectl", "label", "--overwrite", "ns", agentNamespace,
			"pod-security.kubernetes.io/enforce=privileged")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to label the agent namespace")

		By("checking that the manager is scoped to the e2e namespaces")
		Expect(getManagerWatchNamespaces()).To(ContainSubstring(agentNamespace),
			"the manager must watch the isolated agent namespace")
		Expect(getManagerWatchNamespaces()).To(ContainSubstring(namespace),
			"the manager must watch the manager namespace")
	})

	AfterAll(func() {
		By("deleting the agent namespace and everything in it")
		cmd := exec.Command("kubectl", "delete", "ns", agentNamespace, "--ignore-not-found", "--wait=false")
		_, _ = utils.Run(cmd)
	})

	agentYAML := func(persistence string) string {
		storage := ""
		if persistence != "" {
			storage = fmt.Sprintf("    storage:\n%s", persistence)
		}
		return fmt.Sprintf(`apiVersion: agents.hermeum.app/v1alpha1
kind: HermesAgent
metadata:
  name: %s
spec:
  hermes:
%s
`, agentName, storage)
	}

	enabledPersistence := "      persistence:\n        enabled: true\n        size: 2Gi\n"

	agentPhase := func() string {
		cmd := exec.Command("kubectl", "get", "hermesagent", agentName,
			"-o", "jsonpath={.status.phase}", "-n", agentNamespace)
		out, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to read the agent phase")
		return out
	}

	statefulSet := func() *appsv1.StatefulSet {
		cmd := exec.Command("kubectl", "get", "statefulset", agentName, "-o", "json", "-n", agentNamespace)
		out, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to read the agent StatefulSet")
		sts := &appsv1.StatefulSet{}
		Expect(json.Unmarshal([]byte(out), sts)).To(Succeed())
		return sts
	}

	// dataVolumeShape classifies how the observed StatefulSet provisions
	// the agent data volume, mirroring the operator's own shape test.
	dataVolumeShape := func(sts *appsv1.StatefulSet) string {
		for _, t := range sts.Spec.VolumeClaimTemplates {
			if t.Name == hermesDataVolume {
				return "volumeClaimTemplate"
			}
		}
		for _, v := range sts.Spec.Template.Spec.Volumes {
			if v.Name == hermesDataVolume {
				if v.EmptyDir != nil {
					return "emptyDir"
				}
				return "explicitVolume"
			}
		}
		return "none"
	}

	It("applies persistence added to a running agent (issue #133)", func() {
		By("creating the agent without persistence (emptyDir data volume)")
		Expect(applyStdin(agentYAML(""))).To(Succeed(), "Failed to create the agent")

		Eventually(func(g Gomega) {
			g.Expect(agentPhase()).To(Equal("Running"))
		}, 5*time.Minute, 5*time.Second).Should(Succeed())

		By("checking the data volume is an emptyDir before persistence")
		Expect(dataVolumeShape(statefulSet())).To(Equal("emptyDir"))

		By("writing a marker file into the ephemeral data dir")
		markerCmd := exec.Command("kubectl", "exec", fmt.Sprintf("%s-0", agentName),
			"-n", agentNamespace, "--", "sh", "-c", "echo before-pvc > /opt/data/e2e-marker.txt")
		_, err := utils.Run(markerCmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to write the marker file")

		By("adding persistence to the running agent")
		Expect(applyStdin(agentYAML(enabledPersistence))).To(Succeed(),
			"Failed to update the agent with persistence")

		By("waiting for the operator-provisioned data PVC to bind")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "pvc",
				fmt.Sprintf("%s-%s-0", hermesDataVolume, agentName),
				"-o", "jsonpath={.status.phase}", "-n", agentNamespace)
			out, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred(),
				"PVC "+hermesDataVolume+"-"+agentName+"-0 must exist")
			g.Expect(out).To(Equal("Bound"))
		}, 5*time.Minute, 5*time.Second).Should(Succeed())

		By("checking the StatefulSet now provisions the data volume via a volumeClaimTemplate")
		Eventually(func(g Gomega) {
			g.Expect(dataVolumeShape(statefulSet())).To(Equal("volumeClaimTemplate"))
		}, 3*time.Minute, 5*time.Second).Should(Succeed())

		By("waiting for the agent to run on the PVC-backed volume")
		Eventually(func(g Gomega) {
			g.Expect(agentPhase()).To(Equal("Running"))
		}, 10*time.Minute, 10*time.Second).Should(Succeed())

		By("checking the agent pod state is clean (no crashlooping after the reshape)")
		checkNoCrashLoop := func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "pod", fmt.Sprintf("%s-0", agentName),
				"-o", "json", "-n", agentNamespace)
			out, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			pod := &corev1.Pod{}
			g.Expect(json.Unmarshal([]byte(out), pod)).To(Succeed())
			g.Expect(pod.Status.ContainerStatuses).NotTo(BeEmpty())
			waiting := pod.Status.ContainerStatuses[0].State.Waiting
			if waiting != nil {
				g.Expect(waiting.Reason).NotTo(Equal("CrashLoopBackOff"))
			}
		}
		Eventually(func(g Gomega) { checkNoCrashLoop(g) }, time.Minute, 5*time.Second).Should(Succeed())

		By("verifying the PVC provides /opt/data inside the running pod")
		mountCmd := exec.Command("kubectl", "exec", fmt.Sprintf("%s-0", agentName),
			"-n", agentNamespace, "--", "sh", "-c",
			`awk '$2 == "/opt/data" {print $1}' /proc/mounts`)
		device, err := utils.Run(mountCmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to inspect the agent pod mounts")
		GinkgoWriter.Printf("device for /opt/data: %q\n", device)
		Expect(strings.TrimSpace(device)).NotTo(BeEmpty(),
			"/opt/data must remain mounted after persistence is enabled")
		Expect(strings.TrimSpace(device)).NotTo(Equal("overlay"),
			"/opt/data must not be the container filesystem: the PVC must provide it")

		By("verifying the agent recreated its home on the PVC")
		lsCmd := exec.Command("kubectl", "exec", fmt.Sprintf("%s-0", agentName),
			"-n", agentNamespace, "--", "sh", "-c", "ls /opt/data | head -5")
		lsOut, err := utils.Run(lsCmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to list the agent data dir")
		Expect(lsOut).NotTo(BeEmpty(), "the agent home on the PVC must exist")
	})
})
