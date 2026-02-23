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
	"os"
	"strconv"
	"time"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/uuid"
	"sigs.k8s.io/controller-runtime/pkg/client"

	replicationv1alpha1 "github.com/csi-addons/kubernetes-csi-addons/api/replication.storage/v1alpha1"
)

const (
	replicationSecretName      = "replication-secret"
	replicationParameterPrefix = "replication.storage.openshift.io/"
	secretNameKey              = replicationParameterPrefix + "replication-secret-name"
	secretNamespaceKey         = replicationParameterPrefix + "replication-secret-namespace"
	pvcDataSourceKind          = "PersistentVolumeClaim"
	defaultStorageClass        = "rook-ceph-block"
	defaultProvisioner         = "rook-ceph.rbd.csi.ceph.com"
	pollInterval               = 2 * time.Second
	defaultReplicationPollSec  = 300
	pvcBindTimeout             = 120 * time.Second
	cleanupWaitTimeout         = 45 * time.Second

	// Finalizer names must match internal/controller/replication.storage/finalizers.go
	volumeReplicationFinalizer = "replication.storage.openshift.io"
	pvcReplicationFinalizer    = "replication.storage.openshift.io/pvc-protection"
)

// MirroringMode is the replication mirroring mode (snapshot or journal).
type MirroringMode string

const (
	MirroringModeSnapshot MirroringMode = "snapshot"
	MirroringModeJournal  MirroringMode = "journal"
)

// TestEnv holds configuration for the e2e replication tests.
type TestEnv struct {
	StorageClass          string
	Provisioner           string
	ReplicationSecretName string // if set with ReplicationSecretNamespace, use existing secret
	ReplicationSecretNs   string
	// Full DR: when DR1_CONTEXT and DR2_CONTEXT are both set, tests can create resources on both clusters.
	DR1Context string
	DR2Context string
	FullDR     bool
}

// getReplicationPollTimeout returns the timeout for waiting on VolumeReplication conditions.
// REPLICATION_POLL_TIMEOUT (seconds) overrides the default (300s). Used for Replicating=True and similar.
func getReplicationPollTimeout() time.Duration {
	s := os.Getenv("REPLICATION_POLL_TIMEOUT")
	if s == "" {
		return defaultReplicationPollSec * time.Second
	}
	sec, err := strconv.Atoi(s)
	if err != nil || sec <= 0 {
		return defaultReplicationPollSec * time.Second
	}
	return time.Duration(sec) * time.Second
}

// GetTestEnv returns TestEnv from environment or defaults.
func GetTestEnv() TestEnv {
	sc := os.Getenv("STORAGE_CLASS")
	if sc == "" {
		sc = defaultStorageClass
	}
	provisioner := os.Getenv("CSI_PROVISIONER")
	if provisioner == "" {
		provisioner = defaultProvisioner
	}
	dr1 := os.Getenv("DR1_CONTEXT")
	dr2 := os.Getenv("DR2_CONTEXT")
	return TestEnv{
		StorageClass:          sc,
		Provisioner:           provisioner,
		ReplicationSecretName: os.Getenv("REPLICATION_SECRET_NAME"),
		ReplicationSecretNs:   os.Getenv("REPLICATION_SECRET_NAMESPACE"),
		DR1Context:            dr1,
		DR2Context:            dr2,
		FullDR:                dr1 != "" && dr2 != "",
	}
}

// UniqueNamespace returns a unique namespace name for e2e tests.
func UniqueNamespace() string {
	return fmt.Sprintf("e2e-replication-%s", uuid.NewUUID()[:8])
}

// CreateNamespace creates a namespace with the given name.
func CreateNamespace(ctx context.Context, c client.Client, name string) *corev1.Namespace {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: name},
	}
	err := c.Create(ctx, ns)
	Expect(err).NotTo(HaveOccurred())
	return ns
}

// CreateSecret creates a secret for replication. For Ceph RBD the driver expects
// "userID" and "userKey" in the secret data; we include them so the driver does not
// return "missing ID field 'userID' in secrets". For real Ceph clusters use an
// existing secret via REPLICATION_SECRET_NAME and REPLICATION_SECRET_NAMESPACE instead.
func CreateSecret(ctx context.Context, c client.Client, namespace, name string) *corev1.Secret {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Type:       corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"userID":  []byte("admin"),
			"userKey": []byte("dummy"),
		},
	}
	err := c.Create(ctx, secret)
	Expect(err).NotTo(HaveOccurred())
	return secret
}

// ReplicationSecretRef returns (secretName, secretNamespace) for use in VolumeReplicationClass.
// If REPLICATION_SECRET_NAME and REPLICATION_SECRET_NAMESPACE are set, those are returned and no secret is created.
// Otherwise a secret with userID/userKey is created in the given namespace and (replicationSecretName, namespace) is returned.
func ReplicationSecretRef(ctx context.Context, c client.Client, env TestEnv, namespace string) (name, ns string) {
	if env.ReplicationSecretName != "" && env.ReplicationSecretNs != "" {
		return env.ReplicationSecretName, env.ReplicationSecretNs
	}
	CreateSecret(ctx, c, namespace, replicationSecretName)
	return replicationSecretName, namespace
}

// CreatePVC creates a PVC in the given namespace and waits for it to be Bound.
// If onPoll is non-nil, it is called after each poll with the current PVC so tests can log progress (e.g. phase=Pending).
func CreatePVC(ctx context.Context, c client.Client, namespace, name, storageClass string, size string, onPoll func(*corev1.PersistentVolumeClaim)) *corev1.PersistentVolumeClaim {
	if size == "" {
		size = "1Gi"
	}
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			StorageClassName: &storageClass,
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: mustParseQuantity(size),
				},
			},
		},
	}
	err := c.Create(ctx, pvc)
	Expect(err).NotTo(HaveOccurred())
	Eventually(func() bool {
		err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, pvc)
		if err != nil {
			return false
		}
		if onPoll != nil {
			onPoll(pvc)
		}
		return pvc.Status.Phase == corev1.ClaimBound
	}, pvcBindTimeout, pollInterval).Should(BeTrue(), "PVC %s/%s should become Bound", namespace, name)
	err = c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, pvc)
	Expect(err).NotTo(HaveOccurred())
	return pvc
}

// FormatPVCStatus returns a one-line status for logging (e.g. phase=Pending).
func FormatPVCStatus(pvc *corev1.PersistentVolumeClaim) string {
	phase := string(pvc.Status.Phase)
	if phase == "" {
		phase = "<none>"
	}
	return fmt.Sprintf("%s/%s phase=%s", pvc.Namespace, pvc.Name, phase)
}

func mustParseQuantity(s string) resource.Quantity {
	return resource.MustParse(s)
}

// CreateVolumeReplicationClass creates a VolumeReplicationClass with the given mirroring mode and provisioner.
func CreateVolumeReplicationClass(ctx context.Context, c client.Client, name, provisioner, secretName, secretNamespace string, mode MirroringMode) *replicationv1alpha1.VolumeReplicationClass {
	params := map[string]string{
		secretNameKey:      secretName,
		secretNamespaceKey: secretNamespace,
	}
	switch mode {
	case MirroringModeSnapshot:
		params["mirroringMode"] = "snapshot"
		params["schedulingInterval"] = "1m"
	case MirroringModeJournal:
		params["mirroringMode"] = "journal"
	default:
		params["mirroringMode"] = "snapshot"
		params["schedulingInterval"] = "1m"
	}
	vrc := &replicationv1alpha1.VolumeReplicationClass{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: replicationv1alpha1.VolumeReplicationClassSpec{
			Provisioner: provisioner,
			Parameters:  params,
		},
	}
	err := c.Create(ctx, vrc)
	Expect(err).NotTo(HaveOccurred())
	return vrc
}

// CreateVolumeReplication creates a VolumeReplication for the given PVC.
func CreateVolumeReplication(ctx context.Context, c client.Client, namespace, name, vrcName, pvcName string, state replicationv1alpha1.ReplicationState) *replicationv1alpha1.VolumeReplication {
	vr := &replicationv1alpha1.VolumeReplication{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: replicationv1alpha1.VolumeReplicationSpec{
			VolumeReplicationClass: vrcName,
			ReplicationState:       state,
			DataSource: corev1.TypedLocalObjectReference{
				APIGroup: nil,
				Kind:     pvcDataSourceKind,
				Name:     pvcName,
			},
		},
	}
	err := c.Create(ctx, vr)
	Expect(err).NotTo(HaveOccurred())
	return vr
}

// FormatVRStatus returns a one-line status summary for logging (no newline).
func FormatVRStatus(vr *replicationv1alpha1.VolumeReplication) string {
	state := string(vr.Status.State)
	if state == "" {
		state = "<none>"
	}
	condStr := ""
	for _, c := range vr.Status.Conditions {
		if condStr != "" {
			condStr += " "
		}
		condStr += c.Type + "=" + string(c.Status)
	}
	if condStr == "" {
		condStr = "<none>"
	}
	return fmt.Sprintf("%s/%s state=%s conditions=[%s] message=%q", vr.Namespace, vr.Name, state, condStr, vr.Status.Message)
}

// WaitForVolumeReplicationCondition waits until the VR has a condition matching the given type and status, or times out.
// If onPoll is non-nil, it is called after each poll with the current VR so tests can log progress.
func WaitForVolumeReplicationCondition(ctx context.Context, c client.Client, vr *replicationv1alpha1.VolumeReplication, conditionType string, status metav1.ConditionStatus, onPoll func(*replicationv1alpha1.VolumeReplication)) {
	key := client.ObjectKeyFromObject(vr)
	timeout := getReplicationPollTimeout()
	Eventually(func() bool {
		err := c.Get(ctx, key, vr)
		if err != nil {
			return false
		}
		if onPoll != nil {
			onPoll(vr)
		}
		for _, cond := range vr.Status.Conditions {
			if cond.Type == conditionType && cond.Status == status {
				return true
			}
		}
		return false
	}, timeout, pollInterval).Should(BeTrue(),
		"VolumeReplication %s/%s should get condition %s=%s (state=%s)", vr.Namespace, vr.Name, conditionType, status, vr.Status.State)
}

// hasReplicationSuccessCondition returns true when the VR indicates replication is enabled successfully:
// either Replicating=True or Completed=True (some controllers set Completed when replication is enabled).
func hasReplicationSuccessCondition(vr *replicationv1alpha1.VolumeReplication) bool {
	for _, cond := range vr.Status.Conditions {
		if cond.Status != metav1.ConditionTrue {
			continue
		}
		switch cond.Type {
		case replicationv1alpha1.Replicating, replicationv1alpha1.ConditionCompleted:
			return true
		}
	}
	return false
}

// WaitForVolumeReplicationReplicatingOrCompleted waits until the VR has Replicating=True or Completed=True, or times out.
// Use this for "replication enabled" success: some controllers set Replicating, others set Completed when replication is on.
func WaitForVolumeReplicationReplicatingOrCompleted(ctx context.Context, c client.Client, vr *replicationv1alpha1.VolumeReplication, onPoll func(*replicationv1alpha1.VolumeReplication)) {
	key := client.ObjectKeyFromObject(vr)
	timeout := getReplicationPollTimeout()
	Eventually(func() bool {
		err := c.Get(ctx, key, vr)
		if err != nil {
			return false
		}
		if onPoll != nil {
			onPoll(vr)
		}
		return hasReplicationSuccessCondition(vr)
	}, timeout, pollInterval).Should(BeTrue(),
		"VolumeReplication %s/%s should get Replicating=True or Completed=True (state=%s)", vr.Namespace, vr.Name, vr.Status.State)
}

// hasVolumeReplicationErrorCondition returns true if the VR has an error (message set or a failure condition).
func hasVolumeReplicationErrorCondition(vr *replicationv1alpha1.VolumeReplication) bool {
	if vr.Status.Message != "" {
		return true
	}
	for _, cond := range vr.Status.Conditions {
		if cond.Status == metav1.ConditionTrue && (cond.Type == replicationv1alpha1.VolumeDegraded || cond.Type == replicationv1alpha1.Error ||
			cond.Type == replicationv1alpha1.FailedToPromote || cond.Type == replicationv1alpha1.FailedToDemote || cond.Type == replicationv1alpha1.FailedToResync) {
			return true
		}
	}
	return false
}

// WaitForVolumeReplicationReplicatingOrCompletedUntil waits up to timeout for Replicating=True or Completed=True.
// Returns true if success was seen, false on timeout. Use for idempotent second VR: if false, assert no error.
func WaitForVolumeReplicationReplicatingOrCompletedUntil(ctx context.Context, c client.Client, vr *replicationv1alpha1.VolumeReplication, timeout time.Duration, onPoll func(*replicationv1alpha1.VolumeReplication)) bool {
	key := client.ObjectKeyFromObject(vr)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		err := c.Get(ctx, key, vr)
		if err != nil {
			time.Sleep(pollInterval)
			continue
		}
		if onPoll != nil {
			onPoll(vr)
		}
		if hasReplicationSuccessCondition(vr) {
			return true
		}
		time.Sleep(pollInterval)
	}
	return false
}

// WaitForVolumeReplicationError waits until the VR has any error condition or message indicating failure.
func WaitForVolumeReplicationError(ctx context.Context, c client.Client, vr *replicationv1alpha1.VolumeReplication) {
	key := client.ObjectKeyFromObject(vr)
	timeout := getReplicationPollTimeout()
	Eventually(func() bool {
		err := c.Get(ctx, key, vr)
		if err != nil {
			return false
		}
		if vr.Status.Message != "" {
			return true
		}
		for _, cond := range vr.Status.Conditions {
			if cond.Status == metav1.ConditionFalse && cond.Reason != "" {
				return true
			}
		}
		return false
	}, timeout, pollInterval).Should(BeTrue(),
		"VolumeReplication %s/%s should report an error", vr.Namespace, vr.Name)
}

// RemoveFinalizerFromVR patches the VR to remove the replication finalizer so it can be deleted.
// Use when the controller is unable to remove it (e.g. driver unreachable).
func RemoveFinalizerFromVR(ctx context.Context, c client.Client, vr *replicationv1alpha1.VolumeReplication) {
	if vr == nil {
		return
	}
	key := client.ObjectKeyFromObject(vr)
	err := c.Get(ctx, key, vr)
	if err != nil {
		if errors.IsNotFound(err) {
			return
		}
		Expect(err).NotTo(HaveOccurred())
	}
	if !containsString(vr.Finalizers, volumeReplicationFinalizer) {
		return
	}
	vr.Finalizers = removeString(vr.Finalizers, volumeReplicationFinalizer)
	Expect(c.Update(ctx, vr)).To(Succeed())
}

// RemoveFinalizerFromPVC patches the PVC to remove the replication finalizer so it can be deleted.
func RemoveFinalizerFromPVC(ctx context.Context, c client.Client, pvc *corev1.PersistentVolumeClaim) {
	if pvc == nil {
		return
	}
	key := client.ObjectKeyFromObject(pvc)
	err := c.Get(ctx, key, pvc)
	if err != nil {
		if errors.IsNotFound(err) {
			return
		}
		Expect(err).NotTo(HaveOccurred())
	}
	if !containsString(pvc.Finalizers, pvcReplicationFinalizer) {
		return
	}
	pvc.Finalizers = removeString(pvc.Finalizers, pvcReplicationFinalizer)
	Expect(c.Update(ctx, pvc)).To(Succeed())
}

func containsString(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

func removeString(slice []string, s string) []string {
	out := make([]string, 0, len(slice))
	for _, v := range slice {
		if v != s {
			out = append(out, v)
		}
	}
	return out
}

// DeleteVolumeReplicationWithCleanup deletes the VR, waits for it to be gone, and removes
// its finalizer if it is still present after the timeout (e.g. controller/driver not cleaning up).
func DeleteVolumeReplicationWithCleanup(ctx context.Context, c client.Client, vr *replicationv1alpha1.VolumeReplication) {
	if vr == nil {
		return
	}
	key := client.ObjectKeyFromObject(vr)
	_ = c.Delete(ctx, vr)
	deadline := time.Now().Add(cleanupWaitTimeout)
	for time.Now().Before(deadline) {
		err := c.Get(ctx, key, vr)
		if errors.IsNotFound(err) {
			return
		}
		time.Sleep(pollInterval)
	}
	// Still present (e.g. finalizer blocking); remove finalizer so it can be deleted
	if err := c.Get(ctx, key, vr); err == nil {
		RemoveFinalizerFromVR(ctx, c, vr)
	}
}

// DeletePVCWithCleanup deletes the PVC, waits for it to be gone, and removes the replication
// finalizer if it is still present after the timeout.
func DeletePVCWithCleanup(ctx context.Context, c client.Client, pvc *corev1.PersistentVolumeClaim) {
	if pvc == nil {
		return
	}
	key := client.ObjectKeyFromObject(pvc)
	_ = c.Delete(ctx, pvc)
	deadline := time.Now().Add(cleanupWaitTimeout)
	for time.Now().Before(deadline) {
		err := c.Get(ctx, key, pvc)
		if errors.IsNotFound(err) {
			return
		}
		time.Sleep(pollInterval)
	}
	if err := c.Get(ctx, key, pvc); err == nil {
		RemoveFinalizerFromPVC(ctx, c, pvc)
	}
}

// DeleteVolumeReplication deletes a VR and ignores NotFound.
func DeleteVolumeReplication(ctx context.Context, c client.Client, vr *replicationv1alpha1.VolumeReplication) {
	err := c.Delete(ctx, vr)
	if err != nil && !errors.IsNotFound(err) {
		Expect(err).NotTo(HaveOccurred())
	}
}

// DeleteVolumeReplicationClass deletes a VRC and ignores NotFound.
func DeleteVolumeReplicationClass(ctx context.Context, c client.Client, vrc *replicationv1alpha1.VolumeReplicationClass) {
	err := c.Delete(ctx, vrc)
	if err != nil && !errors.IsNotFound(err) {
		Expect(err).NotTo(HaveOccurred())
	}
}

// DeletePVC deletes a PVC and ignores NotFound.
func DeletePVC(ctx context.Context, c client.Client, pvc *corev1.PersistentVolumeClaim) {
	err := c.Delete(ctx, pvc)
	if err != nil && !errors.IsNotFound(err) {
		Expect(err).NotTo(HaveOccurred())
	}
}

// DeleteNamespace deletes a namespace and ignores NotFound.
func DeleteNamespace(ctx context.Context, c client.Client, ns *corev1.Namespace) {
	err := c.Delete(ctx, ns)
	if err != nil && !errors.IsNotFound(err) {
		Expect(err).NotTo(HaveOccurred())
	}
}
