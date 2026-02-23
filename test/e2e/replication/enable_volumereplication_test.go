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

var _ = Describe("EnableVolumeReplication", func() {
	var ctx context.Context
	var env TestEnv

	BeforeEach(func() {
		ctx = context.Background()
		env = GetTestEnv()
	})

	Describe("L1-E-001: Enable snapshot mode", func() {
		It("L1-E-001 enables replication with snapshot mode and VR gets success condition", func() {
			By("Starting L1-E-001: Enable snapshot mode")
			c := GetK8sClient()
			nsName := UniqueNamespace()
			By("Creating namespace " + nsName)
			ns := CreateNamespace(ctx, c, nsName)

			secretName, secretNs := ReplicationSecretRef(ctx, c, env, nsName)
			By("Creating PVC and waiting for Bound (poll every 2s, timeout 120s)")
			pvc := CreatePVC(ctx, c, nsName, "pvc-rep", env.StorageClass, "1Gi", func(p *corev1.PersistentVolumeClaim) {
				fmt.Fprintf(GinkgoWriter, "  [PVC] %s\n", FormatPVCStatus(p))
			})

			vrcName := "vrc-snapshot-" + nsName
			By("Creating VolumeReplicationClass (snapshot, 1m interval) " + vrcName)
			vrc := CreateVolumeReplicationClass(ctx, c, vrcName, env.Provisioner, secretName, secretNs, MirroringModeSnapshot)

			vrName := "vr-snapshot"
			By("Creating VolumeReplication " + vrName)
			vr := CreateVolumeReplication(ctx, c, nsName, vrName, vrcName, pvc.Name, replicationv1alpha1.Primary)

			DeferCleanup(func() {
				cleanupCtx := context.Background()
				DeleteVolumeReplicationWithCleanup(cleanupCtx, c, vr)
				DeleteVolumeReplicationClass(cleanupCtx, c, vrc)
				DeletePVCWithCleanup(cleanupCtx, c, pvc)
				DeleteNamespace(cleanupCtx, c, ns)
			})

			By("Waiting for Replicating=True or Completed=True (timeout from REPLICATION_POLL_TIMEOUT)")
			WaitForVolumeReplicationReplicatingOrCompleted(ctx, c, vr, func(v *replicationv1alpha1.VolumeReplication) {
				fmt.Fprintf(GinkgoWriter, "  [VR] %s\n", FormatVRStatus(v))
			})
			err := c.Get(ctx, client.ObjectKey{Namespace: nsName, Name: vrName}, vr)
			Expect(err).NotTo(HaveOccurred())
			By("VR state: " + string(vr.Status.State))
			// State is set by controller after successful enable; some drivers may leave it Unknown
			Expect(vr.Status.State).To(Or(Equal(replicationv1alpha1.PrimaryState), Equal(replicationv1alpha1.UnknownState)))
		})
	})

	Describe("L1-E-002: Enable journal mode", func() {
		It("L1-E-002 enables replication with journal mode and VR gets success condition", func() {
			By("Starting L1-E-002: Enable journal mode")
			c := GetK8sClient()
			nsName := UniqueNamespace()
			By("Creating namespace " + nsName)
			ns := CreateNamespace(ctx, c, nsName)

			secretName, secretNs := ReplicationSecretRef(ctx, c, env, nsName)
			By("Creating PVC and waiting for Bound (poll every 2s, timeout 120s)")
			pvc := CreatePVC(ctx, c, nsName, "pvc-journal", env.StorageClass, "1Gi", func(p *corev1.PersistentVolumeClaim) {
				fmt.Fprintf(GinkgoWriter, "  [PVC] %s\n", FormatPVCStatus(p))
			})

			vrcName := "vrc-journal-" + nsName
			By("Creating VolumeReplicationClass (journal mode) " + vrcName)
			vrc := CreateVolumeReplicationClass(ctx, c, vrcName, env.Provisioner, secretName, secretNs, MirroringModeJournal)

			vrName := "vr-journal"
			By("Creating VolumeReplication " + vrName)
			vr := CreateVolumeReplication(ctx, c, nsName, vrName, vrcName, pvc.Name, replicationv1alpha1.Primary)

			DeferCleanup(func() {
				cleanupCtx := context.Background()
				DeleteVolumeReplicationWithCleanup(cleanupCtx, c, vr)
				DeleteVolumeReplicationClass(cleanupCtx, c, vrc)
				DeletePVCWithCleanup(cleanupCtx, c, pvc)
				DeleteNamespace(cleanupCtx, c, ns)
			})

			By("Waiting for Replicating=True or Completed=True (journal may take longer than snapshot)")
			WaitForVolumeReplicationReplicatingOrCompleted(ctx, c, vr, func(v *replicationv1alpha1.VolumeReplication) {
				fmt.Fprintf(GinkgoWriter, "  [VR] %s\n", FormatVRStatus(v))
			})
			err := c.Get(ctx, client.ObjectKey{Namespace: nsName, Name: vrName}, vr)
			Expect(err).NotTo(HaveOccurred())
			By("VR state: " + string(vr.Status.State))
			Expect(vr.Status.State).To(Or(Equal(replicationv1alpha1.PrimaryState), Equal(replicationv1alpha1.UnknownState)))
		})
	})

	Describe("L1-E-005: Idempotent enable", func() {
		It("L1-E-005 enabling replication on already enabled volume is idempotent", func() {
			By("Starting L1-E-005: Idempotent enable")
			c := GetK8sClient()
			nsName := UniqueNamespace()
			By("Creating namespace " + nsName)
			ns := CreateNamespace(ctx, c, nsName)

			secretName, secretNs := ReplicationSecretRef(ctx, c, env, nsName)
			By("Creating PVC and waiting for Bound (poll every 2s, timeout 120s)")
			pvc := CreatePVC(ctx, c, nsName, "pvc-idem", env.StorageClass, "1Gi", func(p *corev1.PersistentVolumeClaim) {
				fmt.Fprintf(GinkgoWriter, "  [PVC] %s\n", FormatPVCStatus(p))
			})

			vrcName := "vrc-idem-" + nsName
			By("Creating VolumeReplicationClass (snapshot, 1m interval) " + vrcName)
			vrc := CreateVolumeReplicationClass(ctx, c, vrcName, env.Provisioner, secretName, secretNs, MirroringModeSnapshot)

			vrName := "vr-idem"
			By("Creating first VolumeReplication " + vrName)
			vr := CreateVolumeReplication(ctx, c, nsName, vrName, vrcName, pvc.Name, replicationv1alpha1.Primary)

			By("Waiting for first VR Replicating=True or Completed=True")
			WaitForVolumeReplicationReplicatingOrCompleted(ctx, c, vr, func(v *replicationv1alpha1.VolumeReplication) {
				fmt.Fprintf(GinkgoWriter, "  [VR] %s\n", FormatVRStatus(v))
			})
			err := c.Get(ctx, client.ObjectKey{Namespace: nsName, Name: vrName}, vr)
			Expect(err).NotTo(HaveOccurred())
			By("First VR state: " + string(vr.Status.State))
			Expect(vr.Status.State).To(Or(Equal(replicationv1alpha1.PrimaryState), Equal(replicationv1alpha1.UnknownState)))

			// Create a second VR for the same PVC (same volume) - controller should treat as idempotent / no error
			vr2Name := "vr-idem-second"
			By("Creating second VolumeReplication " + vr2Name + " for same PVC")
			vr2 := CreateVolumeReplication(ctx, c, nsName, vr2Name, vrcName, pvc.Name, replicationv1alpha1.Primary)

			DeferCleanup(func() {
				cleanupCtx := context.Background()
				DeleteVolumeReplicationWithCleanup(cleanupCtx, c, vr2)
				DeleteVolumeReplicationWithCleanup(cleanupCtx, c, vr)
				DeleteVolumeReplicationClass(cleanupCtx, c, vrc)
				DeletePVCWithCleanup(cleanupCtx, c, pvc)
				DeleteNamespace(cleanupCtx, c, ns)
			})

			// Second VR: many controllers never set status on a duplicate VR (idempotent no-op). Wait up to 2m for success
			// (covers 1m pool; 5m pool would need longer but would make suite hit 10m timeout). If no success, require no error.
			By("Waiting for second VR Replicating=True or Completed=True (up to 2m); if none, require no error (idempotent)")
			gotSuccess := WaitForVolumeReplicationReplicatingOrCompletedUntil(ctx, c, vr2, 2*time.Minute, func(v *replicationv1alpha1.VolumeReplication) {
				fmt.Fprintf(GinkgoWriter, "  [VR] %s\n", FormatVRStatus(v))
			})
			err = c.Get(ctx, client.ObjectKey{Namespace: nsName, Name: vr2Name}, vr2)
			Expect(err).NotTo(HaveOccurred())
			if gotSuccess {
				By("Second VR state: " + string(vr2.Status.State))
				Expect(vr2.Status.State).To(Or(Equal(replicationv1alpha1.PrimaryState), Equal(replicationv1alpha1.UnknownState)))
			} else {
				By("Second VR did not get success condition; asserting no error (idempotent no-op)")
				Expect(hasVolumeReplicationErrorCondition(vr2)).To(BeFalse(), "second VR should have no error when controller does not set status")
			}
		})
	})
})
