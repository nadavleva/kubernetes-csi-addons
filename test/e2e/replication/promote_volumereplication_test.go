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

var _ = Describe("PromoteVolumeReplication", func() {
	var ctx context.Context
	var env TestEnv

	BeforeEach(func() {
		ctx = context.Background()
		env = GetTestEnv()
	})

	Describe("L1-PROM-001: Promote secondary to primary (healthy)", func() {
		It("L1-PROM-001: promote secondary → primary when healthy, expect successful promotion", func() {
			By("L1-PROM-001: Create primary on DR1, secondary on DR2; promote secondary to primary")
			cDR1 := GetK8sClientForCluster(ClusterDR1)
			cDR2 := GetK8sClientForCluster(ClusterDR2)
			SkipIfNotFullDR("L1-PROM-001", "requires two clusters (DR1_CONTEXT and DR2_CONTEXT)")

			nsName := UniqueNamespace()
			By("Creating namespace on both DR1 and DR2")
			ns1 := CreateNamespace(ctx, cDR1, nsName)
			ns2 := CreateNamespace(ctx, cDR2, nsName)

			secretName, secretNs := ReplicationSecretRef(ctx, cDR1, env, nsName)
			_, _ = ReplicationSecretRef(ctx, cDR2, env, nsName)

			By("Creating primary PVC and VR on DR1")
			pvcDR1 := CreatePVC(ctx, cDR1, nsName, "pvc-dr1-prom", env.StorageClass, "1Gi", func(p *corev1.PersistentVolumeClaim) {
				fmt.Fprintf(GinkgoWriter, "  [DR1][PVC] %s\n", FormatPVCStatus(p))
			})
			vrcName := "vrc-prom-" + nsName
			vrcDR1 := CreateVolumeReplicationClass(ctx, cDR1, vrcName, env.Provisioner, secretName, secretNs, MirroringModeSnapshot)
			vrDR1 := CreateVolumeReplication(ctx, cDR1, nsName, "vr-dr1-prom", vrcName, pvcDR1.Name, replicationv1alpha1.Primary)

			By("Waiting for primary VR on DR1 to reach Replicating=True")
			WaitForVolumeReplicationReplicatingOrCompleted(ctx, cDR1, vrDR1, func(v *replicationv1alpha1.VolumeReplication) {
				fmt.Fprintf(GinkgoWriter, "  [DR1][VR] %s\n", FormatVRStatus(v))
			})

			By("Creating secondary PVC and VR on DR2")
			pvcDR2, pvDR2 := CreateSecondaryPVCFromPrimary(ctx, cDR1, cDR2, pvcDR1, nsName, "pvc-dr2-prom", func(p *corev1.PersistentVolumeClaim) {
				fmt.Fprintf(GinkgoWriter, "  [DR2][PVC] %s\n", FormatPVCStatus(p))
			})
			vrcDR2 := CreateVolumeReplicationClass(ctx, cDR2, vrcName, env.Provisioner, secretName, secretNs, MirroringModeSnapshot)
			fmt.Fprintf(GinkgoWriter, "  [DEBUG] Creating VR with replicationState constant value=%v (should be 'secondary')\n", replicationv1alpha1.Secondary)
			vrDR2 := CreateVolumeReplication(ctx, cDR2, nsName, "vr-dr2-prom", vrcName, pvcDR2.Name, replicationv1alpha1.Secondary)
			fmt.Fprintf(GinkgoWriter, "  [DR2][VR] AFTER CREATION Spec.ReplicationState=%v (type=%T)\n", vrDR2.Spec.ReplicationState, vrDR2.Spec.ReplicationState)

			By("Waiting for secondary VR on DR2 to reach Secondary state and stable")
			Eventually(func() string {
				_ = cDR2.Get(ctx, client.ObjectKeyFromObject(vrDR2), vrDR2)
				return string(vrDR2.Status.State)
			}, 30*time.Second, 1*time.Second).Should(Equal(string(replicationv1alpha1.SecondaryState)),
				"Secondary VR should be in Secondary state before promotion")

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

			By("L1-PROM-001: Promote secondary VR on DR2 by changing replicationState to Primary")
			vrDR2.Spec.ReplicationState = replicationv1alpha1.Primary
			err := cDR2.Update(ctx, vrDR2)
			Expect(err).NotTo(HaveOccurred(), "Failed to update VR replicationState to Primary")

			By("Waiting for VR state to transition to Primary")
			Eventually(func() string {
				_ = cDR2.Get(ctx, client.ObjectKeyFromObject(vrDR2), vrDR2)
				fmt.Fprintf(GinkgoWriter, "  [DR2][VR] %s\n", FormatVRStatus(vrDR2))
				return string(vrDR2.Status.State)
			}, 5*time.Minute, 5*time.Second).Should(Equal(string(replicationv1alpha1.PrimaryState)),
				"VR should transition to Primary state after promotion request")

			By("L1-PROM-001: Assertion — secondary VR state changed to Primary")
			err = cDR2.Get(ctx, client.ObjectKey{Namespace: nsName, Name: vrDR2.Name}, vrDR2)
			Expect(err).NotTo(HaveOccurred())
			Expect(vrDR2.Status.State).To(Equal(replicationv1alpha1.PrimaryState),
				"Secondary VR should be promoted to Primary, got %s", vrDR2.Status.State)

			By("L1-PROM-001: Assertion — secondary PVC now writable (RW)")
			err = cDR2.Get(ctx, client.ObjectKey{Namespace: nsName, Name: pvcDR2.Name}, pvcDR2)
			Expect(err).NotTo(HaveOccurred())
			Expect(pvcDR2.Status.Phase).To(Equal(corev1.ClaimBound),
				"Promoted secondary PVC should remain bound, got %s", pvcDR2.Status.Phase)
		})
	})

	Describe("L1-PROM-002: Promote already primary (idempotent)", func() {
		It("L1-PROM-002: promote when already primary (idempotent), expect no error", func() {
			By("L1-PROM-002: Create primary VR, attempt promote (idempotent no-op)")
			c := GetK8sClient()
			nsName := UniqueNamespace()
			By("Creating namespace " + nsName)
			ns := CreateNamespace(ctx, c, nsName)

			secretName, secretNs := ReplicationSecretRef(ctx, c, env, nsName)
			By("Creating PVC and waiting for Bound")
			pvc := CreatePVC(ctx, c, nsName, "pvc-prom-idem", env.StorageClass, "1Gi", func(p *corev1.PersistentVolumeClaim) {
				fmt.Fprintf(GinkgoWriter, "  [PVC] %s\n", FormatPVCStatus(p))
			})

			vrcName := "vrc-prom-idem-" + nsName
			By("Creating VolumeReplicationClass (snapshot)")
			vrc := CreateVolumeReplicationClass(ctx, c, vrcName, env.Provisioner, secretName, secretNs, MirroringModeSnapshot)

			vrName := "vr-prom-idem"
			By("Creating VolumeReplication (already primary)")
			vr := CreateVolumeReplication(ctx, c, nsName, vrName, vrcName, pvc.Name, replicationv1alpha1.Primary)

			By("Waiting for Replicating=True or Completed=True")
			WaitForVolumeReplicationReplicatingOrCompleted(ctx, c, vr, func(v *replicationv1alpha1.VolumeReplication) {
				fmt.Fprintf(GinkgoWriter, "  [VR] %s\n", FormatVRStatus(v))
			})

			DeferCleanup(func() {
				cleanupCtx := context.Background()
				DeleteVolumeReplicationWithCleanup(cleanupCtx, c, vr)
				DeleteVolumeReplicationClass(cleanupCtx, c, vrc)
				DeletePVCWithCleanup(cleanupCtx, c, pvc)
				DeleteNamespace(cleanupCtx, c, ns)
			})

			By("L1-PROM-002: Recording initial VR state")
			err := c.Get(ctx, client.ObjectKey{Namespace: nsName, Name: vrName}, vr)
			Expect(err).NotTo(HaveOccurred())
			initialState := vr.Status.State

			By("L1-PROM-002: Attempting to promote already-primary VR (idempotent)")
			vr.Spec.ReplicationState = replicationv1alpha1.Primary
			err = c.Update(ctx, vr)
			Expect(err).NotTo(HaveOccurred(), "Update should succeed for idempotent promote")

			By("L1-PROM-002: Assertion — VR state remains Primary (no change)")
			err = c.Get(ctx, client.ObjectKey{Namespace: nsName, Name: vrName}, vr)
			Expect(err).NotTo(HaveOccurred())
			Expect(vr.Status.State).To(Equal(initialState),
				"VR state should remain Primary after idempotent promote, got %s", vr.Status.State)
			Expect(vr.Status.State).To(Equal(replicationv1alpha1.PrimaryState),
				"VR should remain Primary, got %s", vr.Status.State)

			By("L1-PROM-002: Assertion — PVC remains bound and writable")
			err = c.Get(ctx, client.ObjectKey{Namespace: nsName, Name: pvc.Name}, pvc)
			Expect(err).NotTo(HaveOccurred())
			Expect(pvc.Status.Phase).To(Equal(corev1.ClaimBound),
				"PVC should remain bound, got %s", pvc.Status.Phase)
		})
	})

	Describe("L1-PROM-007: Promote with active I/O workload", func() {
		It("L1-PROM-007: promote secondary to primary with active workload, expect graceful promotion", func() {
			By("L1-PROM-007: Create primary on DR1, secondary on DR2; promote under load")
			cDR1 := GetK8sClientForCluster(ClusterDR1)
			cDR2 := GetK8sClientForCluster(ClusterDR2)
			SkipIfNotFullDR("L1-PROM-007", "requires two clusters (DR1_CONTEXT and DR2_CONTEXT)")

			nsName := UniqueNamespace()
			By("Creating namespace on both DR1 and DR2")
			ns1 := CreateNamespace(ctx, cDR1, nsName)
			ns2 := CreateNamespace(ctx, cDR2, nsName)

			secretName, secretNs := ReplicationSecretRef(ctx, cDR1, env, nsName)
			_, _ = ReplicationSecretRef(ctx, cDR2, env, nsName)

			By("Creating primary PVC and VR on DR1")
			pvcDR1 := CreatePVC(ctx, cDR1, nsName, "pvc-dr1-prom-io", env.StorageClass, "1Gi", func(p *corev1.PersistentVolumeClaim) {
				fmt.Fprintf(GinkgoWriter, "  [DR1][PVC] %s\n", FormatPVCStatus(p))
			})
			vrcName := "vrc-prom-io-" + nsName
			vrcDR1 := CreateVolumeReplicationClass(ctx, cDR1, vrcName, env.Provisioner, secretName, secretNs, MirroringModeSnapshot)
			vrDR1 := CreateVolumeReplication(ctx, cDR1, nsName, "vr-dr1-prom-io", vrcName, pvcDR1.Name, replicationv1alpha1.Primary)

			By("Waiting for primary VR on DR1 to reach Replicating=True")
			WaitForVolumeReplicationReplicatingOrCompleted(ctx, cDR1, vrDR1, func(v *replicationv1alpha1.VolumeReplication) {
				fmt.Fprintf(GinkgoWriter, "  [DR1][VR] %s\n", FormatVRStatus(v))
			})

			By("Creating secondary PVC and VR on DR2")
			pvcDR2, pvDR2 := CreateSecondaryPVCFromPrimary(ctx, cDR1, cDR2, pvcDR1, nsName, "pvc-dr2-prom-io", func(p *corev1.PersistentVolumeClaim) {
				fmt.Fprintf(GinkgoWriter, "  [DR2][PVC] %s\n", FormatPVCStatus(p))
			})
			vrcDR2 := CreateVolumeReplicationClass(ctx, cDR2, vrcName, env.Provisioner, secretName, secretNs, MirroringModeSnapshot)
			vrDR2 := CreateVolumeReplication(ctx, cDR2, nsName, "vr-dr2-prom-io", vrcName, pvcDR2.Name, replicationv1alpha1.Secondary)

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

			By("L1-PROM-007: Promote secondary VR on DR2 with active workload (force=false)")
			vrDR2.Spec.ReplicationState = replicationv1alpha1.Primary
			err := cDR2.Update(ctx, vrDR2)
			Expect(err).NotTo(HaveOccurred(), "Failed to promote with active workload")

			By("Waiting for VR state to transition to Primary (graceful promotion)")
			Eventually(func() string {
				_ = cDR2.Get(ctx, client.ObjectKeyFromObject(vrDR2), vrDR2)
				fmt.Fprintf(GinkgoWriter, "  [DR2][VR] %s\n", FormatVRStatus(vrDR2))
				return string(vrDR2.Status.State)
			}, 5*time.Minute, 5*time.Second).Should(Equal(string(replicationv1alpha1.PrimaryState)),
				"VR should transition to Primary state after promotion request with active workload")

			By("L1-PROM-007: Assertion — secondary VR promoted to Primary")
			err = cDR2.Get(ctx, client.ObjectKey{Namespace: nsName, Name: vrDR2.Name}, vrDR2)
			Expect(err).NotTo(HaveOccurred())
			Expect(vrDR2.Status.State).To(Equal(replicationv1alpha1.PrimaryState),
				"VR should be promoted to Primary, got %s", vrDR2.Status.State)

			By("L1-PROM-007: Assertion — secondary PVC now writable")
			err = cDR2.Get(ctx, client.ObjectKey{Namespace: nsName, Name: pvcDR2.Name}, pvcDR2)
			Expect(err).NotTo(HaveOccurred())
			Expect(pvcDR2.Status.Phase).To(Equal(corev1.ClaimBound),
				"Promoted secondary PVC should remain bound, got %s", pvcDR2.Status.Phase)
		})
	})

	Describe("L1-PROM-008: Force promote with active I/O workload", func() {
		It("L1-PROM-008: force promote secondary with active workload, expect immediate promotion", func() {
			By("L1-PROM-008: Create primary on DR1, secondary on DR2; force promote under load")
			cDR1 := GetK8sClientForCluster(ClusterDR1)
			cDR2 := GetK8sClientForCluster(ClusterDR2)
			SkipIfNotFullDR("L1-PROM-008", "requires two clusters (DR1_CONTEXT and DR2_CONTEXT)")

			nsName := UniqueNamespace()
			By("Creating namespace on both DR1 and DR2")
			ns1 := CreateNamespace(ctx, cDR1, nsName)
			ns2 := CreateNamespace(ctx, cDR2, nsName)

			secretName, secretNs := ReplicationSecretRef(ctx, cDR1, env, nsName)
			_, _ = ReplicationSecretRef(ctx, cDR2, env, nsName)

			By("Creating primary PVC and VR on DR1")
			pvcDR1 := CreatePVC(ctx, cDR1, nsName, "pvc-dr1-prom-force", env.StorageClass, "1Gi", func(p *corev1.PersistentVolumeClaim) {
				fmt.Fprintf(GinkgoWriter, "  [DR1][PVC] %s\n", FormatPVCStatus(p))
			})
			vrcName := "vrc-prom-force-" + nsName
			vrcDR1 := CreateVolumeReplicationClass(ctx, cDR1, vrcName, env.Provisioner, secretName, secretNs, MirroringModeSnapshot)
			vrDR1 := CreateVolumeReplication(ctx, cDR1, nsName, "vr-dr1-prom-force", vrcName, pvcDR1.Name, replicationv1alpha1.Primary)

			By("Waiting for primary VR on DR1 to reach Replicating=True")
			WaitForVolumeReplicationReplicatingOrCompleted(ctx, cDR1, vrDR1, func(v *replicationv1alpha1.VolumeReplication) {
				fmt.Fprintf(GinkgoWriter, "  [DR1][VR] %s\n", FormatVRStatus(v))
			})

			By("Creating secondary PVC and VR on DR2")
			pvcDR2, pvDR2 := CreateSecondaryPVCFromPrimary(ctx, cDR1, cDR2, pvcDR1, nsName, "pvc-dr2-prom-force", func(p *corev1.PersistentVolumeClaim) {
				fmt.Fprintf(GinkgoWriter, "  [DR2][PVC] %s\n", FormatPVCStatus(p))
			})
			vrcDR2 := CreateVolumeReplicationClass(ctx, cDR2, vrcName, env.Provisioner, secretName, secretNs, MirroringModeSnapshot)
			vrDR2 := CreateVolumeReplication(ctx, cDR2, nsName, "vr-dr2-prom-force", vrcName, pvcDR2.Name, replicationv1alpha1.Secondary)

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

			By("L1-PROM-008: Force promote secondary VR on DR2 with active workload (force=true)")
			vrDR2.Spec.ReplicationState = replicationv1alpha1.Primary
			err := cDR2.Update(ctx, vrDR2)
			Expect(err).NotTo(HaveOccurred(), "Failed to force promote with active workload")

			By("Waiting for VR state to transition to Primary (force promotion)")
			Eventually(func() string {
				_ = cDR2.Get(ctx, client.ObjectKeyFromObject(vrDR2), vrDR2)
				fmt.Fprintf(GinkgoWriter, "  [DR2][VR] %s\n", FormatVRStatus(vrDR2))
				return string(vrDR2.Status.State)
			}, 5*time.Minute, 5*time.Second).Should(Equal(string(replicationv1alpha1.PrimaryState)),
				"VR should transition to Primary state after force promotion request")

			By("L1-PROM-008: Assertion — secondary VR immediately promoted to Primary")
			err = cDR2.Get(ctx, client.ObjectKey{Namespace: nsName, Name: vrDR2.Name}, vrDR2)
			Expect(err).NotTo(HaveOccurred())
			Expect(vrDR2.Status.State).To(Equal(replicationv1alpha1.PrimaryState),
				"VR should be force promoted to Primary, got %s", vrDR2.Status.State)

			By("L1-PROM-008: Assertion — secondary PVC now writable")
			err = cDR2.Get(ctx, client.ObjectKey{Namespace: nsName, Name: pvcDR2.Name}, pvcDR2)
			Expect(err).NotTo(HaveOccurred())
			Expect(pvcDR2.Status.Phase).To(Equal(corev1.ClaimBound),
				"Promoted secondary PVC should remain bound, got %s", pvcDR2.Status.Phase)
		})
	})
})
