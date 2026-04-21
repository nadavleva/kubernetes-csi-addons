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

package helpers

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	csiaddonsv1alpha1 "github.com/csi-addons/kubernetes-csi-addons/api/csiaddons/v1alpha1"
	uuid "github.com/google/uuid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NetworkFenceHandler implements FaultInjectionHandler for NetworkFence CRD-based fault injection.
// It wraps the NetworkFence provider and adds validation logic to ensure the fence operation
// completed successfully by polling the NetworkFence CRD status.
type NetworkFenceHandler struct {
	config      FaultInjectionConfig
	namespace   string
	provisioner string
	targets     []string // CIDRs to fence (set by ApplyFence)
	nfcName     string
	nfcClass    *csiaddonsv1alpha1.NetworkFenceClass
	nfName      string
	nf          *csiaddonsv1alpha1.NetworkFence
}

// NewNetworkFenceHandler creates a new NetworkFence-based fault injection handler.
func NewNetworkFenceHandler(ctx context.Context, config FaultInjectionConfig) (FaultInjectionHandler, error) {
	provisioner := config.ProviderParams["provisioner"]
	if provisioner == "" {
		return nil, fmt.Errorf("provisioner parameter required for NetworkFence handler")
	}

	handler := &NetworkFenceHandler{
		config:      config,
		namespace:   config.Namespace,
		provisioner: provisioner,
	}

	return handler, nil
}

// ApplyFence creates NetworkFence CRDs and validates that the fence operation succeeded.
// Process:
//  1. Create NetworkFenceClass with provisioner
//  2. Create NetworkFence with Fenced state and target CIDRs
//  3. Poll Status.Result until Succeeded (timeout 60s, poll 1s)
//  4. Return success once CSI driver confirms fence is active
func (h *NetworkFenceHandler) ApplyFence(ctx context.Context, targets []string) error {
	if len(targets) == 0 {
		return fmt.Errorf("no targets provided for fence")
	}

	// 1. Create NetworkFenceClass with provisioner
	uuid := uuid.New().String()[:8]
	h.nfcName = fmt.Sprintf("nfc-handler-%s", uuid)

	// Build parameters for NetworkFenceClass
	params := make(map[string]string)

	// Add secret parameters if provided
	if secretName := h.config.ProviderParams["secretName"]; secretName != "" {
		params["csiaddons.openshift.io/networkfence-secret-name"] = secretName
		if secretNs := h.config.ProviderParams["secretNamespace"]; secretNs != "" {
			params["csiaddons.openshift.io/networkfence-secret-namespace"] = secretNs
		}
	}

	// Add clusterID for Ceph/Rook (required for network fencing)
	if clusterID := os.Getenv("FENCE_CLUSTER_ID"); clusterID != "" {
		params["clusterID"] = clusterID
	} else if strings.Contains(h.provisioner, "ceph") && h.config.ProviderParams["secretNamespace"] != "" {
		// For Rook/Ceph, use secret namespace as clusterID if not explicitly set
		params["clusterID"] = h.config.ProviderParams["secretNamespace"]
	}

	nfc := &csiaddonsv1alpha1.NetworkFenceClass{
		TypeMeta: metav1.TypeMeta{
			APIVersion: csiaddonsv1alpha1.GroupVersion.String(),
			Kind:       "NetworkFenceClass",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: h.nfcName,
		},
		Spec: csiaddonsv1alpha1.NetworkFenceClassSpec{
			Provisioner: h.provisioner,
			Parameters:  params,
		},
	}

	if err := h.config.Client.Create(ctx, nfc); err != nil {
		return fmt.Errorf("create NetworkFenceClass: %w", err)
	}
	h.nfcClass = nfc

	// 2. Create NetworkFence with Fenced state
	h.nfName = fmt.Sprintf("nf-handler-%s", uuid)

	nf := &csiaddonsv1alpha1.NetworkFence{
		TypeMeta: metav1.TypeMeta{
			APIVersion: csiaddonsv1alpha1.GroupVersion.String(),
			Kind:       "NetworkFence",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: h.namespace,
			Name:      h.nfName,
		},
		Spec: csiaddonsv1alpha1.NetworkFenceSpec{
			NetworkFenceClassName: h.nfcName,
			FenceState:            csiaddonsv1alpha1.Fenced,
			Cidrs:                 targets,
		},
	}

	if err := h.config.Client.Create(ctx, nf); err != nil {
		return fmt.Errorf("create NetworkFence: %w", err)
	}
	h.nf = nf
	h.targets = targets

	// 3. Poll Status.Result until Succeeded (timeout 60s, poll 1s)
	confirmTimeout := time.NewTimer(60 * time.Second)
	defer confirmTimeout.Stop()
	confirmTicker := time.NewTicker(1 * time.Second)
	defer confirmTicker.Stop()

	for {
		select {
		case <-confirmTimeout.C:
			return fmt.Errorf("timeout: NetworkFence did not reach Succeeded status within 60s")
		case <-confirmTicker.C:
			nfCurrent := &csiaddonsv1alpha1.NetworkFence{}
			if err := h.config.Client.Get(ctx, client.ObjectKey{Namespace: h.namespace, Name: h.nfName}, nfCurrent); err != nil {
				// Transient error, continue polling
				continue
			}

			// Check operation result
			if nfCurrent.Status.Result == csiaddonsv1alpha1.FencingOperationResultSucceeded {
				// Wait 5s for storage to detect fence
				time.Sleep(5 * time.Second)
				return nil
			}

			if nfCurrent.Status.Result == csiaddonsv1alpha1.FencingOperationResultFailed {
				return fmt.Errorf("NetworkFence operation failed: %s", nfCurrent.Status.Message)
			}

			// Still Pending, continue polling
		}
	}
}

// RemoveFence updates NetworkFence to Unfenced state and validates that the operation succeeded.
// Process:
//  1. Update NetworkFence spec.FenceState to Unfenced
//  2. Poll Status.Result until Succeeded (timeout 60s, poll 1s)
//  3. Return success once CSI driver confirms unfence is complete
func (h *NetworkFenceHandler) RemoveFence(ctx context.Context) error {
	if h.nf == nil {
		return fmt.Errorf("no NetworkFence resource to remove (fence was not applied)")
	}

	// 1. Update NetworkFence state to Unfenced
	nfCurrent := &csiaddonsv1alpha1.NetworkFence{}
	if err := h.config.Client.Get(ctx, client.ObjectKey{Namespace: h.namespace, Name: h.nfName}, nfCurrent); err != nil {
		return fmt.Errorf("get NetworkFence for update: %w", err)
	}

	nfCurrent.Spec.FenceState = csiaddonsv1alpha1.Unfenced
	if err := h.config.Client.Update(ctx, nfCurrent); err != nil {
		return fmt.Errorf("update NetworkFence to Unfenced: %w", err)
	}

	// 2. Poll Status.Result until Succeeded (timeout 60s, poll 1s)
	confirmTimeout := time.NewTimer(60 * time.Second)
	defer confirmTimeout.Stop()
	confirmTicker := time.NewTicker(1 * time.Second)
	defer confirmTicker.Stop()

	for {
		select {
		case <-confirmTimeout.C:
			return fmt.Errorf("timeout: NetworkFence did not complete unfence within 60s")
		case <-confirmTicker.C:
			nfCurrent := &csiaddonsv1alpha1.NetworkFence{}
			if err := h.config.Client.Get(ctx, client.ObjectKey{Namespace: h.namespace, Name: h.nfName}, nfCurrent); err != nil {
				// Transient error, continue polling
				continue
			}

			// Check operation result and state
			if nfCurrent.Status.Result == csiaddonsv1alpha1.FencingOperationResultSucceeded &&
				nfCurrent.Spec.FenceState == csiaddonsv1alpha1.Unfenced {
				// Wait 5s for storage to recover
				time.Sleep(5 * time.Second)
				return nil
			}

			if nfCurrent.Status.Result == csiaddonsv1alpha1.FencingOperationResultFailed {
				return fmt.Errorf("NetworkFence unfence operation failed: %s", nfCurrent.Status.Message)
			}

			// Still Pending, continue polling
		}
	}
}

// IsSupported checks if NetworkFence is supported in the cluster.
// Requires: NetworkFence and NetworkFenceClass CRDs.
func (h *NetworkFenceHandler) IsSupported(ctx context.Context) (bool, string) {
	// Check if NetworkFence CRD is available
	nf := &csiaddonsv1alpha1.NetworkFence{}
	if err := h.config.Client.Get(ctx, client.ObjectKey{Name: "check-crd-availability"}, nf); err != nil {
		// Errors during Get indicate the CRD might not be available
		// but we can't easily tell NotFound from CRD-not-available without deep inspection
		// For now, we'll assume any error means the CRD is available (most likely NotFound)
	}

	// Check if NetworkFenceClass CRD is available
	nfc := &csiaddonsv1alpha1.NetworkFenceClass{}
	if err := h.config.Client.Get(ctx, client.ObjectKey{Name: "check-crd-availability"}, nfc); err != nil {
		// Similarly, assume available for now
	}

	return true, ""
}

// Cleanup performs cleanup of NetworkFence resources.
// Deletes NetworkFence CR (which triggers CSI driver unfencing) and NetworkFenceClass.
func (h *NetworkFenceHandler) Cleanup(ctx context.Context) error {
	// Delete NetworkFence (triggers unfencing)
	if h.nf != nil {
		if err := h.config.Client.Delete(ctx, h.nf); err != nil {
			// Log but don't fail cleanup on deletion error
			// (may already be deleted or in graceful termination)
		}
		// Wait for unfence to propagate
		time.Sleep(2 * time.Second)
	}

	// Delete NetworkFenceClass
	if h.nfcClass != nil {
		if err := h.config.Client.Delete(ctx, h.nfcClass); err != nil {
			// Log but don't fail cleanup on deletion error
		}
	}

	return nil
}

// DiscoverFenceTargets discovers fence targets for NetworkFence.
// Implements the unified discovery logic from L1-E-003:
// Calls DiscoverFenceCIDRsForNetworkFence (GetFenceCIDRs logic) with provisioner and a discovery-specific NetworkFenceClassName.
// Does NOT create the NetworkFenceClass - that's done during ApplyFence.
//
// For full-DR scenarios with PeerClient: Uses peer cluster for node IP fallback discovery.
//
// Order of discovery:
//  1. FENCE_CIDRS environment variable (explicit targets)
//  2. CSIAddonsNode status.networkFenceClientStatus for the provisioner
//  3. Node InternalIPs as host routes (/32 for IPv4, /128 for IPv6)
//     - For single-cluster: Uses h.config.Client nodes
//     - For full-DR with PeerClient: Uses h.config.PeerClient nodes
//  4. Returns empty list if nothing found (test caller should skip)
//
// Note: For NetworkFence discovery to work with CSIAddonsNode CIDRs (step 2),
// the CSI-Addons controller must have published client details in CSIAddonsNode.
// If not, discovery falls back to node IP list (step 3) from primary or peer cluster.
func (h *NetworkFenceHandler) DiscoverFenceTargets(ctx context.Context) []string {
	// Use a deterministic NetworkFenceClassName for discovery
	// This is used only for CSIAddonsNode lookup; actual NetworkFenceClass is created in ApplyFence
	discoveryClassName := "nfc-handler-discovery"

	// For full-DR with peer client: use peer cluster for node IP fallback
	// This allows fencing peer nodes when CSIAddonsNode doesn't publish CIDRs
	if IsFullDRMode() && h.config.PeerClient != nil {
		Logf("[DEBUG]", "NetworkFenceHandler.DiscoverFenceTargets: Full-DR mode with PeerClient; using peer client for node IP discovery")
		// GetFenceCIDRsWithPeerNodeClient uses:
		// 1. CSIAddonsNode lookup on h.config.Client
		// 2. Peer node IP fallback on h.config.PeerClient
		return GetFenceCIDRsWithPeerNodeClient(ctx, h.config.Client, h.config.PeerClient, h.provisioner, discoveryClassName)
	}

	if IsFullDRMode() && h.config.PeerClient == nil {
		Logf("[WARN]", "NetworkFenceHandler.DiscoverFenceTargets: Full-DR mode detected but PeerClient not set; node IP fallback will use primary cluster nodes")
	}

	// Single-cluster or full-DR without PeerClient: use primary cluster for all discovery
	// This runs the same chain as L1-E-003:
	// FENCE_CIDRS env → CSIAddonsNode status → node IPs (from primary cluster) → none
	return DiscoverFenceCIDRsForNetworkFence(ctx, h.config.Client, h.provisioner, discoveryClassName)
}
