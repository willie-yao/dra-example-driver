//go:build e2e

/*
 * Copyright The Kubernetes Authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package e2e

import (
	"fmt"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	gpuv1alpha1 "sigs.k8s.io/dra-example-driver/api/example.com/resource/gpu/v1alpha1"
)

var _ = Describe("Test GPU allocation", func() {
	Context("GPU Test 1 - Two pods, one container each, one GPU per container", func() {
		var cleanup func()
		BeforeEach(func(ctx SpecContext) {
			cleanup = deployManifest(ctx, "gpu-test1.yaml")
		})
		AfterEach(func() {
			cleanup()
		})

		It("should allocate 1 distinct GPU per pod", func() {
			namespace := "gpu-test1"
			pods := []string{"pod0", "pod1"}
			checkPodsReadyAndRunning(namespace, pods, len(pods))

			observedGPUs := make(map[string]string)
			for _, podName := range pods {
				verifyGPUAllocation(namespace, podName, "ctr0", 1, observedGPUs)
			}
		})
	})

	Context("GPU Test 2 - One pod, one container with two GPUs", func() {
		var cleanup func()
		BeforeEach(func(ctx SpecContext) {
			cleanup = deployManifest(ctx, "gpu-test2.yaml")
		})
		AfterEach(func() {
			cleanup()
		})

		It("should allocate 2 distinct GPUs to a single container", func() {
			namespace := "gpu-test2"
			pods := []string{"pod0"}
			checkPodsReadyAndRunning(namespace, pods, len(pods))

			observedGPUs := make(map[string]string)
			verifyGPUAllocation(namespace, pods[0], "ctr0", 2, observedGPUs)
		})
	})

	Context("GPU Test 3 - One pod, two containers, one shared GPU with TimeSlicing (Default interval)", func() {
		var cleanup func()
		BeforeEach(func(ctx SpecContext) {
			cleanup = deployManifest(ctx, "gpu-test3.yaml")
		})
		AfterEach(func() {
			cleanup()
		})

		It("should share 1 GPU between containers with default timeslice interval", func() {
			namespace := "gpu-test3"
			pods := []string{"pod0"}
			containerNames := []string{"ctr0", "ctr1"}
			expectedSharingStrategy := string(gpuv1alpha1.TimeSlicingStrategy)
			expectedTimeSliceInterval := string(gpuv1alpha1.DefaultTimeSlice)

			checkPodsReadyAndRunning(namespace, pods, len(pods))

			observedGPUs := make(map[string]string)
			var gpuCtr0 string
			for _, containerName := range containerNames {
				Eventually(func(g Gomega) {
					By("checking that there is exactly 1 GPU")
					gpus, logs := getGPUsFromPodLogs(namespace, pods[0], containerName)
					verifyGPUCount(g, gpus, 1, namespace, pods[0], containerName)
					if containerName == "ctr0" {
						By(fmt.Sprintf("checking that the GPU is unclaimed for %s", containerName))
						gpuCtr0 = gpus[0]
						claimNewGPU(g, observedGPUs, gpus[0], namespace, pods[0], containerName)
					} else {
						By(fmt.Sprintf("checking that container %s claims the same GPU", containerName))
						verifySharedGPU(g, gpus[0], gpuCtr0, namespace, pods[0], containerName)
					}
					verifyGPUProperties(g, logs, namespace, pods[0], containerName, gpus, expectedSharingStrategy, "TIMESLICE_INTERVAL", expectedTimeSliceInterval)
				}, checkPodLogsTimeout, checkPodLogsInterval).Should(Succeed())
			}
		})
	})

	Context("GPU Test 4 - Two pods, one container each, one shared GPU with TimeSlicing (Default interval)", func() {
		var cleanup func()
		BeforeEach(func(ctx SpecContext) {
			cleanup = deployManifest(ctx, "gpu-test4.yaml")
		})
		AfterEach(func() {
			cleanup()
		})

		It("should share 1 GPU between pods with default timeslice interval", func() {
			namespace := "gpu-test4"
			pods := []string{"pod0", "pod1"}
			expectedSharingStrategy := string(gpuv1alpha1.TimeSlicingStrategy)
			expectedTimeSliceInterval := string(gpuv1alpha1.DefaultTimeSlice)

			checkPodsReadyAndRunning(namespace, pods, len(pods))

			observedGPUs := make(map[string]string)
			var gpuPod0 string
			for _, podName := range pods {
				Eventually(func(g Gomega) {
					By("checking that there is exactly 1 GPU")
					gpus, logs := getGPUsFromPodLogs(namespace, podName, "ctr0")
					verifyGPUCount(g, gpus, 1, namespace, podName, "ctr0")
					if podName == "pod0" {
						By(fmt.Sprintf("checking that the GPU is unclaimed for %s", podName))
						gpuPod0 = gpus[0]
						claimNewGPU(g, observedGPUs, gpuPod0, namespace, podName, "ctr0")
					} else {
						By(fmt.Sprintf("checking that %s claims the same GPU", podName))
						verifySharedGPU(g, gpus[0], gpuPod0, namespace, podName, "ctr0")
					}
					verifyGPUProperties(g, logs, namespace, podName, "ctr0", gpus, expectedSharingStrategy, "TIMESLICE_INTERVAL", expectedTimeSliceInterval)
				}, checkPodLogsTimeout, checkPodLogsInterval).Should(Succeed())
			}
		})
	})

	Context("GPU Test 5 - One pod, four containers, two shared GPUs with TimeSlicing and SpacePartitioning", func() {
		var cleanup func()
		BeforeEach(func(ctx SpecContext) {
			cleanup = deployManifest(ctx, "gpu-test5.yaml")
		})
		AfterEach(func() {
			cleanup()
		})

		It("should share GPUs with timeslicing (long interval) between ts containers", func() {
			namespace := "gpu-test5"
			pods := []string{"pod0"}
			tsContainers := []string{"ts-ctr0", "ts-ctr1"}
			expectedSharingStrategy := string(gpuv1alpha1.TimeSlicingStrategy)
			expectedTimeSliceInterval := string(gpuv1alpha1.LongTimeSlice)

			checkPodsReadyAndRunning(namespace, pods, len(pods))

			observedGPUs := make(map[string]string)
			var gpuTsCtr0 string
			for _, containerName := range tsContainers {
				Eventually(func(g Gomega) {
					By("checking that there is exactly 1 GPU")
					gpus, logs := getGPUsFromPodLogs(namespace, pods[0], containerName)
					verifyGPUCount(g, gpus, 1, namespace, pods[0], containerName)
					if containerName == "ts-ctr0" {
						By(fmt.Sprintf("checking that the GPU is unclaimed for %s", containerName))
						gpuTsCtr0 = gpus[0]
						claimNewGPU(g, observedGPUs, gpuTsCtr0, namespace, pods[0], containerName)
					} else {
						By(fmt.Sprintf("checking that container %s claims the same GPU", containerName))
						verifySharedGPU(g, gpus[0], gpuTsCtr0, namespace, pods[0], containerName)
					}
					verifyGPUProperties(g, logs, namespace, pods[0], containerName, gpus, expectedSharingStrategy, "TIMESLICE_INTERVAL", expectedTimeSliceInterval)
				}, checkPodLogsTimeout, checkPodLogsInterval).Should(Succeed())
			}
		})

		It("should share GPUs with space partitioning between sp containers", func() {
			namespace := "gpu-test5"
			pods := []string{"pod0"}
			spContainers := []string{"sp-ctr0", "sp-ctr1"}
			expectedSharingStrategy := string(gpuv1alpha1.SpacePartitioningStrategy)
			expectedPartitionCount := "10"

			observedGPUs := make(map[string]string)
			var gpuSpCtr0 string
			for _, containerName := range spContainers {
				Eventually(func(g Gomega) {
					By("checking that there is exactly 1 GPU")
					gpus, logs := getGPUsFromPodLogs(namespace, pods[0], containerName)
					verifyGPUCount(g, gpus, 1, namespace, pods[0], containerName)
					if containerName == "sp-ctr0" {
						By(fmt.Sprintf("checking that the GPU is unclaimed for %s", containerName))
						gpuSpCtr0 = gpus[0]
						claimNewGPU(g, observedGPUs, gpuSpCtr0, namespace, pods[0], containerName)
					} else {
						By(fmt.Sprintf("checking that container %s claims the same GPU", containerName))
						verifySharedGPU(g, gpus[0], gpuSpCtr0, namespace, pods[0], containerName)
					}
					verifyGPUProperties(g, logs, namespace, pods[0], containerName, gpus, expectedSharingStrategy, "PARTITION_COUNT", expectedPartitionCount)
				}, checkPodLogsTimeout, checkPodLogsInterval).Should(Succeed())
			}
		})
	})

	Context("GPU Test 6 - One pod, one init container, one container, shared GPU with TimeSlicing (Default interval)", func() {
		var cleanup func()
		BeforeEach(func(ctx SpecContext) {
			cleanup = deployManifest(ctx, "gpu-test6.yaml")
		})
		AfterEach(func() {
			cleanup()
		})

		It("should share 1 GPU between init container and regular container", func() {
			namespace := "gpu-test6"
			pods := []string{"pod0"}
			containerNames := []string{"init0", "ctr0"}
			expectedSharingStrategy := string(gpuv1alpha1.TimeSlicingStrategy)
			expectedTimeSliceInterval := string(gpuv1alpha1.DefaultTimeSlice)

			checkPodsReadyAndRunning(namespace, pods, len(pods))

			observedGPUs := make(map[string]string)
			var gpuInit0 string
			for _, containerName := range containerNames {
				Eventually(func(g Gomega) {
					By("checking that there is exactly 1 GPU")
					gpus, logs := getGPUsFromPodLogs(namespace, pods[0], containerName)
					verifyGPUCount(g, gpus, 1, namespace, pods[0], containerName)
					if containerName == "init0" {
						By(fmt.Sprintf("checking that the GPU is unclaimed for %s", containerName))
						gpuInit0 = gpus[0]
						claimNewGPU(g, observedGPUs, gpuInit0, namespace, pods[0], containerName)
					} else {
						By(fmt.Sprintf("checking that container %s claims the same GPU", containerName))
						verifySharedGPU(g, gpus[0], gpuInit0, namespace, pods[0], containerName)
					}
					verifyGPUProperties(g, logs, namespace, pods[0], containerName, gpus, expectedSharingStrategy, "TIMESLICE_INTERVAL", expectedTimeSliceInterval)
				}, checkPodLogsTimeout, checkPodLogsInterval).Should(Succeed())
			}
		})
	})

	Context("GPU Test 7 - DRAAdminAccess set to true", func() {
		var cleanup func()
		BeforeEach(func(ctx SpecContext) {
			cleanup = deployManifest(ctx, "gpu-test7.yaml")
		})
		AfterEach(func() {
			cleanup()
		})

		It("should have DRA_ADMIN_ACCESS set to true", func() {
			namespace := "gpu-test7"
			pods := []string{"pod0"}
			checkPodsReadyAndRunning(namespace, pods, len(pods))
			verifyDRAAdminAccess(namespace, pods[0], "ctr0", "true")
		})
	})

	Context("GPU Test 8 - One pod, one container with GPU selected using CEL expression", func() {
		var cleanup func()
		BeforeEach(func(ctx SpecContext) {
			cleanup = deployManifest(ctx, "gpu-test8.yaml")
		})
		AfterEach(func() {
			cleanup()
		})

		It("should allocate 1 GPU selected using CEL expression", func() {
			namespace := "gpu-test8"
			pods := []string{"pod0"}
			checkPodsReadyAndRunning(namespace, pods, len(pods))

			observedGPUs := make(map[string]string)
			verifyGPUAllocation(namespace, pods[0], "ctr0", 1, observedGPUs)
		})
	})

	Context("Webhooks", func() {
		tests := []struct {
			name     string
			fileName string
		}{
			{name: "v1 ResourceClaim", fileName: "invalid_rc_v1.yaml"},
			{name: "v1beta1 ResourceClaim", fileName: "invalid_rc_v1beta1.yaml"},
			{name: "v1 ResourceClaimTemplate", fileName: "invalid_rc_template.yaml"},
		}

		for _, testCase := range tests {
			It("should reject invalid "+testCase.name, func(ctx SpecContext) {
				manifestPath := filepath.Join(currentDir, "testdata", "webhooks", testCase.fileName)

				err := createManifestWithDryRun(ctx, dynamicClient, manifestPath)
				fmt.Fprintf(GinkgoWriter, "Error from create: %v\n", err)
				Expect(err).To(HaveOccurred(),
					"Expected webhook to reject %s, but it was accepted", testCase.fileName)
				Expect(err.Error()).To(ContainSubstring("unknown time-slice interval"),
					"Webhook did not reject %s invalid GpuConfig with the expected message. Got error: %v", testCase.name, err)
			})
		}
	})
})
