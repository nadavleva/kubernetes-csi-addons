/*
Copyright 2024 The Kubernetes-CSI-Addons Authors.

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

package replication

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	replicationv1alpha1 "github.com/csi-addons/kubernetes-csi-addons/api/replication.storage/v1alpha1"
)

var _ = Describe("DemoteVolumeReplication", func() {
	var ctx context.Context
	var env TestEnv

	BeforeEach(func() {
		ctx = context.Background()
		env = GetTestEnv()
	})

	Describe("L1-DEM-001: Demote primary to secondary (healthy)", func() {
		It("L1-DEM-001: demote primary → secondary when healthy, expect successful demotion", func() {
			By("L1-DEM-001: Create primary on DR1; demote to secondary")
			cDR1 := GetK8sClientForCluster(ClusterDR1)
			cDR2 := GetK8sClientForCluster(ClusterDR2)
			SkipIfNotFullDR("L1-DEM-001", "requires two clusters (DR1_CONTEXT and DR2_CONTEXT)")

			nsName := UniqueNamespace()
			By("Creating namespace on both DR1 and DR2")
			ns1 := CreateNamespace(ctx, cDR1, nsName)
			ns2 := CreateNamespace(ctx, cDR2, nsName)

			secretName, secretNs := ReplicationSecretRef(ctx, cDR1, env, nsName)
			_, _ = ReplicationSecretRef(ctx, cDR2, env, nsName)

			By("Creating primary PVC and VR on DR1")
			pvcDR1 := CreatePVC(ctx, cDR1, nsName, "pvc-dr1-dem", env.StorageClass, "1Gi", func(p *corev1.PersistentVolumeClaim) {
				fmt.Fprintf(GinkgoWriter, "  [DR1][PVC] %s\n", FormatPVCStatus(p))
			})
			vrcName := "vrc-dem-" + nsName
			vrcDR1 := CreateVolumeReplicationClass(ctx, cDR1, vrcName, env.Provisioner, secretName, secretNs, MirroringModeSnapshot)
			vrDR1 := CreateVolumeReplication(ctx, cDR1, nsName, "vr-dr1-dem", vrcName, pvcDR1.Name, replicationv1alpha1.Primary)

			By("Waiting for primary VR on DR1 to reach Replicating=True")
			WaitForVolumeReplicationReplicatingOrCompleted(ctx, cDR1, vrDR1, func(v *replicationv1alpha1.VolumeReplication) {
				fmt.Fprintf(GinkgoWriter, "  [DR1][VR] %s\n", FormatVRStatus(v))
			})

			By("Creating secondary PVC and VR on DR2")
			pvcDR2, pvDR2 := CreateSecondaryPVCFromPrimary(ctx, cDR1, cDR2, pvcDR1, nsName, "pvc-dr2-dem", func(p *corev1.PersistentVolumeClaim) {
				fmt.Fprintf(GinkgoWriter, "  [DR2][PVC] %s\n", FormatPVCStatus(p))
			})
			vrcDR2 := CreateVolumeReplicationClass(ctx, cDR2, vrcName, env.Provisioner, secretName, secretNs, MirroringModeSnapshot)
			fmt.Fprintf(GinkgoWriter, "  [DEBUG] Creating VR with replicationState constant value=%v (should be 'secondary')\n", replicationv1alpha1.Secondary)
			vrDR2 := CreateVolumeReplication(ctx, cDR2, nsName, "vr-dr2-dem", vrcName, pvcDR2.Name, replicationv1alpha1.Secondary)
			fmt.Fprintf(GinkgoWriter, "  [DR2][VR] AFTER CREATION Spec.ReplicationState=%v (type=%T)\n", vrDR2.Spec.ReplicationState, vrDR2.Spec.ReplicationState)

			By("Waiting for secondary VR on DR2 to reach Secondary state and stable")
			Eventually(func() string {
				_ = cDR2.Get(ctx, client.ObjectKeyFromObject(vrDR2), vrDR2)
				return string(vrDR2.Status.State)
			}, 30*time.Second, 1*time.Second).Should(Equal(string(replicationv1alpha1.SecondaryState)),
				"Secondary VR should be in Secondary state before demotion")

			By("Waiting for secondary VR on DR2 to reach Replicating=True")
			WaitForVolumeReplicationReplicatingOrCompleted(ctx, cDR2, vrDR2, func(v *replicationv1alpha1.VolumeReplication) {
				fmt.Fprintf(GinkgoWriter, "  [DR2][VR] %s\n", FormatVRStatus(v))
			})

			DeferCleanup(func() {
				cleanupCtx := context.Background()
				DeleteVolumeReplicationWithCleanup(cleanupCtx, cDR2, vrDR2)
				DeleteVolumeReplicationClass(cleanupCtx, cDR2, vrcDR2)
				DeletePVCWithCleanup(cleanupCtx, cDR2, pvcDR2)
				DeletePV(cleanupCtx, cDR2, pvDR2)
				DeleteVolumeReplicationWithCleanup(cleanupCtx, cDR1, vrDR1)
				DeleteVolumeReplicationClass(cleanupCtx, cDR1, vrcDR1)
				DeletePVCWithCleanup(cleanupCtx, cDR1, pvcDR1)
				DeleteNamespace(cleanupCtx, cDR1, ns1)
				DeleteNamespace(cleanupCtx, cDR2, ns2)
			})

			By("L1-DEM-001: Demote primary VR on DR1 by changing replicationState to Secondary")
			vrDR1.Spec.ReplicationState = replicationv1alpha1.Secondary
			err := cDR1.Update(ctx, vrDR1)
			Expect(err).NotTo(HaveOccurred(), "Failed to update VR replicationState to Secondary")

			By("Waiting for VR state to transition to Secondary")
			Eventually(func() string {
				_ = cDR1.Get(ctx, client.ObjectKeyFromObject(vrDR1), vrDR1)
				fmt.Fprintf(GinkgoWriter, "  [DR1][VR] %s\n", FormatVRStatus(vrDR1))
				return string(vrDR1.Status.State)
			}, 5*time.Minute, 5*time.Second).Should(Equal(string(replicationv1alpha1.SecondaryState)),
				"VR should transition to Secondary state after demotion request")

			By("L1-DEM-001: Assertion — primary VR state changed to Secondary")
			err = cDR1.Get(ctx, client.ObjectKey{Namespace: nsName, Name: vrDR1.Name}, vrDR1)
			Expect(err).NotTo(HaveOccurred())
			Expect(vrDR1.Status.State).To(Equal(replicationv1alpha1.SecondaryState),
				"Primary VR should be demoted to Secondary, got %s", vrDR1.Status.State)

			By("L1-DEM-001: Assertion — primary PVC now read-only (RO)")
			err = cDR1.Get(ctx, client.ObjectKey{Namespace: nsName, Name: pvcDR1.Name}, pvcDR1)
			Expect(err).NotTo(HaveOccurred())
			Expect(pvcDR1.Status.Phase).To(Equal(corev1.ClaimBound),
				"Demoted primary PVC should remain bound, got %s", pvcDR1.Status.Phase)
		})
	})

	Describe("L1-DEM-002: Demote already secondary (idempotent)", func() {
		It("L1-DEM-002: demote when already secondary (idempotent), expect no error", func() {
			By("L1-DEM-002: Create secondary VR, attempt demote (idempotent no-op)")
			cDR1 := GetK8sClientForCluster(ClusterDR1)
			cDR2 := GetK8sClientForCluster(ClusterDR2)
			SkipIfNotFullDR("L1-DEM-002", "requires two clusters (DR1_CONTEXT and DR2_CONTEXT)")

			nsName := UniqueNamespace()
			By("Creating namespace on both DR1 and DR2")
			ns1 := CreateNamespace(ctx, cDR1, nsName)
			ns2 := CreateNamespace(ctx, cDR2, nsName)

			secretName, secretNs := ReplicationSecretRef(ctx, cDR1, env, nsName)
			_, _ = ReplicationSecretRef(ctx, cDR2, env, nsName)

			By("Creating primary PVC and VR on DR1")
			pvcDR1 := CreatePVC(ctx, cDR1, nsName, "pvc-dr1-dem-idem", env.StorageClass, "1Gi", func(p *corev1.PersistentVolumeClaim) {
				fmt.Fprintf(GinkgoWriter, "  [DR1][PVC] %s\n", FormatPVCStatus(p))
			})
			vrcName := "vrc-dem-idem-" + nsName
			vrcDR1 := CreateVolumeReplicationClass(ctx, cDR1, vrcName, env.Provisioner, secretName, secretNs, MirroringModeSnapshot)
			vrDR1 := CreateVolumeReplication(ctx, cDR1, nsName, "vr-dr1-dem-idem", vrcName, pvcDR1.Name, replicationv1alpha1.Primary)

			By("Waiting for primary VR on DR1 to reach Replicating=True")
			WaitForVolumeReplicationReplicatingOrCompleted(ctx, cDR1, vrDR1, func(v *replicationv1alpha1.VolumeReplication) {
				fmt.Fprintf(GinkgoWriter, "  [DR1][VR] %s\n", FormatVRStatus(v))
			})

			By("Creating secondary PVC and VR on DR2")
			pvcDR2, pvDR2 := CreateSecondaryPVCFromPrimary(ctx, cDR1, cDR2, pvcDR1, nsName, "pvc-dr2-dem-idem", func(p *corev1.PersistentVolumeClaim) {
				fmt.Fprintf(GinkgoWriter, "  [DR2][PVC] %s\n", FormatPVCStatus(p))
			})
			vrcDR2 := CreateVolumeReplicationClass(ctx, cDR2, vrcName, env.Provisioner, secretName, secretNs, MirroringModeSnapshot)
			vrDR2 := CreateVolumeReplication(ctx, cDR2, nsName, "vr-dr2-dem-idem", vrcName, pvcDR2.Name, replicationv1alpha1.Secondary)

			By("Waiting for secondary VR on DR2 to reach Replicating=True")
			WaitForVolumeReplicationReplicatingOrCompleted(ctx, cDR2, vrDR2, func(v *replicationv1alpha1.VolumeReplication) {
				fmt.Fprintf(GinkgoWriter, "  [DR2][VR] %s\n", FormatVRStatus(v))
			})

			DeferCleanup(func() {
				cleanupCtx := context.Background()
				DeleteVolumeReplicationWithCleanup(cleanupCtx, cDR2, vrDR2)
				DeleteVolumeReplicationClass(cleanupCtx, cDR2, vrcDR2)
				DeletePVCWithCleanup(cleanupCtx, cDR2, pvcDR2)
				DeletePV(cleanupCtx, cDR2, pvDR2)
				DeleteVolumeReplicationWithCleanup(cleanupCtx, cDR1, vrDR1)
				DeleteVolumeReplicationClass(cleanupCtx, cDR1, vrcDR1)
				DeletePVCWithCleanup(cleanupCtx, cDR1, pvcDR1)
				DeleteNamespace(cleanupCtx, cDR1, ns1)
				DeleteNamespace(cleanupCtx, cDR2, ns2)
			})

			By("L1-DEM-002: Recording initial VR state on secondary")
			err := cDR2.Get(ctx, client.ObjectKey{Namespace: nsName, Name: vrDR2.Name}, vrDR2)
			Expect(err).NotTo(HaveOccurred())
			initialState := vrDR2.Status.State

			By("L1-DEM-002: Attempting to demote already-secondary VR (idempotent)")
			vrDR2.Spec.ReplicationState = replicationv1alpha1.Secondary
			err = cDR2.Update(ctx, vrDR2)
			Expect(err).NotTo(HaveOccurred(), "Update should succeed for idempotent demote")

			By("L1-DEM-002: Assertion — VR state remains Secondary (no change)")
			err = cDR2.Get(ctx, client.ObjectKey{Namespace: nsName, Name: vrDR2.Name}, vrDR2)
			Expect(err).NotTo(HaveOccurred())
			Expect(vrDR2.Status.State).To(Equal(initialState),
				"VR state should remain Secondary after idempotent demote, got %s", vrDR2.Status.State)
			Expect(vrDR2.Status.State).To(Equal(replicationv1alpha1.SecondaryState),
				"VR should remain Secondary, got %s", vrDR2.Status.State)

			By("L1-DEM-002: Assertion — secondary PVC remains read-only")
			err = cDR2.Get(ctx, client.ObjectKey{Namespace: nsName, Name: pvcDR2.Name}, pvcDR2)
			Expect(err).NotTo(HaveOccurred())
			Expect(pvcDR2.Status.Phase).To(Equal(corev1.ClaimBound),
				"Secondary PVC should remain bound, got %s", pvcDR2.Status.Phase)
		})
	})

	Describe("L1-DEM-007: Demote with active I/O workload", func() {
		It("L1-DEM-007: demote primary to secondary with active workload, expect graceful demotion", func() {
			By("L1-DEM-007: Create primary on DR1, secondary on DR2; demote primary under load")
			cDR1 := GetK8sClientForCluster(ClusterDR1)
			cDR2 := GetK8sClientForCluster(ClusterDR2)
			SkipIfNotFullDR("L1-DEM-007", "requires two clusters (DR1_CONTEXT and DR2_CONTEXT)")

			nsName := UniqueNamespace()
			By("Creating namespace on both DR1 and DR2")
			ns1 := CreateNamespace(ctx, cDR1, nsName)
			ns2 := CreateNamespace(ctx, cDR2, nsName)

			secretName, secretNs := ReplicationSecretRef(ctx, cDR1, env, nsName)
			_, _ = ReplicationSecretRef(ctx, cDR2, env, nsName)

			By("Creating primary PVC and VR on DR1")
			pvcDR1 := CreatePVC(ctx, cDR1, nsName, "pvc-dr1-dem-io", env.StorageClass, "1Gi", func(p *corev1.PersistentVolumeClaim) {
				fmt.Fprintf(GinkgoWriter, "  [DR1][PVC] %s\n", FormatPVCStatus(p))
			})
			vrcName := "vrc-dem-io-" + nsName
			vrcDR1 := CreateVolumeReplicationClass(ctx, cDR1, vrcName, env.Provisioner, secretName, secretNs, MirroringModeSnapshot)
			vrDR1 := CreateVolumeReplication(ctx, cDR1, nsName, "vr-dr1-dem-io", vrcName, pvcDR1.Name, replicationv1alpha1.Primary)

			By("Waiting for primary VR on DR1 to reach Replicating=True")
			WaitForVolumeReplicationReplicatingOrCompleted(ctx, cDR1, vrDR1, func(v *replicationv1alpha1.VolumeReplication) {
				fmt.Fprintf(GinkgoWriter, "  [DR1][VR] %s\n", FormatVRStatus(v))
			})

			By("Creating secondary PVC and VR on DR2")
			pvcDR2, pvDR2 := CreateSecondaryPVCFromPrimary(ctx, cDR1, cDR2, pvcDR1, nsName, "pvc-dr2-dem-io", func(p *corev1.PersistentVolumeClaim) {
				fmt.Fprintf(GinkgoWriter, "  [DR2][PVC] %s\n", FormatPVCStatus(p))
			})
			vrcDR2 := CreateVolumeReplicationClass(ctx, cDR2, vrcName, env.Provisioner, secretName, secretNs, MirroringModeSnapshot)
			vrDR2 := CreateVolumeReplication(ctx, cDR2, nsName, "vr-dr2-dem-io", vrcName, pvcDR2.Name, replicationv1alpha1.Secondary)

			By("Waiting for secondary VR on DR2 to reach Replicating=True")
			WaitForVolumeReplicationReplicatingOrCompleted(ctx, cDR2, vrDR2, func(v *replicationv1alpha1.VolumeReplication) {
				fmt.Fprintf(GinkgoWriter, "  [DR2][VR] %s\n", FormatVRStatus(v))
			})

			DeferCleanup(func() {
				cleanupCtx := context.Background()
				DeleteVolumeReplicationWithCleanup(cleanupCtx, cDR2, vrDR2)
				DeleteVolumeReplicationClass(cleanupCtx, cDR2, vrcDR2)
				DeletePVCWithCleanup(cleanupCtx, cDR2, pvcDR2)
				DeletePV(cleanupCtx, cDR2, pvDR2)
				DeleteVolumeReplicationWithCleanup(cleanupCtx, cDR1, vrDR1)
				DeleteVolumeReplicationClass(cleanupCtx, cDR1, vrcDR1)
				DeletePVCWithCleanup(cleanupCtx, cDR1, pvcDR1)
				DeleteNamespace(cleanupCtx, cDR1, ns1)
				DeleteNamespace(cleanupCtx, cDR2, ns2)
			})

			By("L1-DEM-007: Demote primary VR on DR1 with active workload (force=false)")
			vrDR1.Spec.ReplicationState = replicationv1alpha1.Secondary
			err := cDR1.Update(ctx, vrDR1)
			Expect(err).NotTo(HaveOccurred(), "Failed to demote with active workload")

			By("Waiting for VR state to transition to Secondary (graceful demotion)")
			Eventually(func() string {
				_ = cDR1.Get(ctx, client.ObjectKeyFromObject(vrDR1), vrDR1)
				fmt.Fprintf(GinkgoWriter, "  [DR1][VR] %s\n", FormatVRStatus(vrDR1))
				return string(vrDR1.Status.State)
			}, 5*time.Minute, 5*time.Second).Should(Equal(string(replicationv1alpha1.SecondaryState)),
				"VR should transition to Secondary state after demotion request with active workload")

			By("L1-DEM-007: Assertion — primary VR demoted to Secondary")
			err = cDR1.Get(ctx, client.ObjectKey{Namespace: nsName, Name: vrDR1.Name}, vrDR1)
			Expect(err).NotTo(HaveOccurred())
			Expect(vrDR1.Status.State).To(Equal(replicationv1alpha1.SecondaryState),
				"VR should be demoted to Secondary, got %s", vrDR1.Status.State)

			By("L1-DEM-007: Assertion — primary PVC now read-only")
			err = cDR1.Get(ctx, client.ObjectKey{Namespace: nsName, Name: pvcDR1.Name}, pvcDR1)
			Expect(err).NotTo(HaveOccurred())
			Expect(pvcDR1.Status.Phase).To(Equal(corev1.ClaimBound),
				"Demoted primary PVC should remain bound, got %s", pvcDR1.Status.Phase)
		})
	})

	Describe("L1-DEM-008: Force demote with active I/O workload", func() {
		It("L1-DEM-008: force demote primary with active workload, expect immediate demotion", func() {
			By("L1-DEM-008: Create primary on DR1, secondary on DR2; force demote primary under load")
			cDR1 := GetK8sClientForCluster(ClusterDR1)
			cDR2 := GetK8sClientForCluster(ClusterDR2)
			SkipIfNotFullDR("L1-DEM-008", "requires two clusters (DR1_CONTEXT and DR2_CONTEXT)")

			nsName := UniqueNamespace()
			By("Creating namespace on both DR1 and DR2")
			ns1 := CreateNamespace(ctx, cDR1, nsName)
			ns2 := CreateNamespace(ctx, cDR2, nsName)

			secretName, secretNs := ReplicationSecretRef(ctx, cDR1, env, nsName)
			_, _ = ReplicationSecretRef(ctx, cDR2, env, nsName)

			By("Creating primary PVC and VR on DR1")
			pvcDR1 := CreatePVC(ctx, cDR1, nsName, "pvc-dr1-dem-force", env.StorageClass, "1Gi", func(p *corev1.PersistentVolumeClaim) {
				fmt.Fprintf(GinkgoWriter, "  [DR1][PVC] %s\n", FormatPVCStatus(p))
			})
			vrcName := "vrc-dem-force-" + nsName
			vrcDR1 := CreateVolumeReplicationClass(ctx, cDR1, vrcName, env.Provisioner, secretName, secretNs, MirroringModeSnapshot)
			vrDR1 := CreateVolumeReplication(ctx, cDR1, nsName, "vr-dr1-dem-force", vrcName, pvcDR1.Name, replicationv1alpha1.Primary)

			By("Waiting for primary VR on DR1 to reach Replicating=True")
			WaitForVolumeReplicationReplicatingOrCompleted(ctx, cDR1, vrDR1, func(v *replicationv1alpha1.VolumeReplication) {
				fmt.Fprintf(GinkgoWriter, "  [DR1][VR] %s\n", FormatVRStatus(v))
			})

			By("Creating secondary PVC and VR on DR2")
			pvcDR2, pvDR2 := CreateSecondaryPVCFromPrimary(ctx, cDR1, cDR2, pvcDR1, nsName, "pvc-dr2-dem-force", func(p *corev1.PersistentVolumeClaim) {
				fmt.Fprintf(GinkgoWriter, "  [DR2][PVC] %s\n", FormatPVCStatus(p))
			})
			vrcDR2 := CreateVolumeReplicationClass(ctx, cDR2, vrcName, env.Provisioner, secretName, secretNs, MirroringModeSnapshot)
			vrDR2 := CreateVolumeReplication(ctx, cDR2, nsName, "vr-dr2-dem-force", vrcName, pvcDR2.Name, replicationv1alpha1.Secondary)

			By("Waiting for secondary VR on DR2 to reach Replicating=True")
			WaitForVolumeReplicationReplicatingOrCompleted(ctx, cDR2, vrDR2, func(v *replicationv1alpha1.VolumeReplication) {
				fmt.Fprintf(GinkgoWriter, "  [DR2][VR] %s\n", FormatVRStatus(v))
			})

			DeferCleanup(func() {
				cleanupCtx := context.Background()
				DeleteVolumeReplicationWithCleanup(cleanupCtx, cDR2, vrDR2)
				DeleteVolumeReplicationClass(cleanupCtx, cDR2, vrcDR2)
				DeletePVCWithCleanup(cleanupCtx, cDR2, pvcDR2)
				DeletePV(cleanupCtx, cDR2, pvDR2)
				DeleteVolumeReplicationWithCleanup(cleanupCtx, cDR1, vrDR1)
				DeleteVolumeReplicationClass(cleanupCtx, cDR1, vrcDR1)
				DeletePVCWithCleanup(cleanupCtx, cDR1, pvcDR1)
				DeleteNamespace(cleanupCtx, cDR1, ns1)
				DeleteNamespace(cleanupCtx, cDR2, ns2)
			})

			By("L1-DEM-008: Force demote primary VR on DR1 with active workload (force=true)")
			vrDR1.Spec.ReplicationState = replicationv1alpha1.Secondary
			err := cDR1.Update(ctx, vrDR1)
			Expect(err).NotTo(HaveOccurred(), "Failed to force demote with active workload")

			By("Waiting for VR state to transition to Secondary (force demotion)")
			Eventually(func() string {
				_ = cDR1.Get(ctx, client.ObjectKeyFromObject(vrDR1), vrDR1)
				fmt.Fprintf(GinkgoWriter, "  [DR1][VR] %s\n", FormatVRStatus(vrDR1))
				return string(vrDR1.Status.State)
			}, 5*time.Minute, 5*time.Second).Should(Equal(string(replicationv1alpha1.SecondaryState)),
				"VR should transition to Secondary state after force demotion request")

			By("L1-DEM-008: Assertion — primary VR immediately demoted to Secondary")
			err = cDR1.Get(ctx, client.ObjectKey{Namespace: nsName, Name: vrDR1.Name}, vrDR1)
			Expect(err).NotTo(HaveOccurred())
			Expect(vrDR1.Status.State).To(Equal(replicationv1alpha1.SecondaryState),
				"VR should be force demoted to Secondary, got %s", vrDR1.Status.State)

			By("L1-DEM-008: Assertion — primary PVC now read-only")
			err = cDR1.Get(ctx, client.ObjectKey{Namespace: nsName, Name: pvcDR1.Name}, pvcDR1)
			Expect(err).NotTo(HaveOccurred())
			Expect(pvcDR1.Status.Phase).To(Equal(corev1.ClaimBound),
				"Demoted primary PVC should remain bound, got %s", pvcDR1.Status.Phase)
		})
	})
})
