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
	"strings"
	"time"
)

// IptablesHandler implements FaultInjectionHandler for iptables-based network fault injection.
// It wraps the PeerFenceProvider and adds validation logic to ensure fault injection
// is actually working before returning success to the test.
type IptablesHandler struct {
	config        FaultInjectionConfig
	faultProvider PeerFenceProvider
	targets       []string
	baseline      *ConnectivityBaseline
}

// NewIptablesHandler creates a new iptables-based fault injection handler.
func NewIptablesHandler(ctx context.Context, config FaultInjectionConfig) (FaultInjectionHandler, error) {
	// Create the underlying iptables fault provider
	provider, err := NewIptablesFaultProvider(config)
	if err != nil {
		return nil, fmt.Errorf("create iptables fault provider: %w", err)
	}

	return &IptablesHandler{
		config:        config,
		faultProvider: provider,
		targets:       []string{},
	}, nil
}

// extractIPFromTarget extracts plain IP from "IP:port" or "IP/CIDR:port" format.
// Examples: "192.168.1.10:6800" -> "192.168.1.10", "192.168.1.10/32:6800" -> "192.168.1.10/32"
func extractIPFromTarget(target string) string {
	if idx := strings.LastIndex(target, ":"); idx > 0 && target[0:idx] != "[" { // avoid [ipv6]:port
		return target[:idx]
	}
	return target
}

// ApplyFence applies iptables fence rules and validates that connectivity is actually blocked.
// Process:
//  1. Establish connectivity baseline (before fence)
//  2. Apply FenceIP rules for each target
//  3. Allow 5s for iptables rules to settle on all pods
//  4. Validate that targets are no longer reachable (poll 120s timeout, 2s interval)
//  5. Return success once connectivity blocked
func (h *IptablesHandler) ApplyFence(ctx context.Context, targets []string) error {
	if len(targets) == 0 {
		return fmt.Errorf("no targets provided for fence")
	}

	// 1. Establish baseline connectivity (before fence) using first target as probe target
	// Extract plain IP from "IP:port" format if present
	probeTarget := extractIPFromTarget(targets[0])
	baseline, err := h.faultProvider.EstablishConnectivityBaseline(ctx, probeTarget)
	if err != nil {
		return fmt.Errorf("establish connectivity baseline: %w", err)
	}
	h.baseline = baseline

	// 2. Apply FenceIP rules for each target
	// Note: This internally calls ensureDaemonSet
	for _, cidr := range targets {
		if err := h.faultProvider.FenceIP(ctx, cidr, nil); err != nil {
			return fmt.Errorf("fence CIDR %s: %w", cidr, err)
		}
	}

	// 3. Allow iptables rules to settle (5s is the existing value from iptables_provider)
	// This includes the Sleep that FenceIP already does, so total is ~5s per FenceIP call
	// Additional wait would be redundant, but we ensure it here for clarity

	// 4. Validate fence is active: verify connectivity is blocked (timeout 120s, poll 2s)
	validationTimeout := time.NewTimer(120 * time.Second)
	defer validationTimeout.Stop()
	validationTicker := time.NewTicker(2 * time.Second)
	defer validationTicker.Stop()

	for {
		select {
		case <-validationTimeout.C:
			return fmt.Errorf("timeout: iptables fence validation failed after 120s (targets still reachable)")
		case <-validationTicker.C:
			// Use VerifyConnectivity with expectedFenced=true to check if blocking works
			// If all targets are unreachable (fenced), blocked=true
			// Use plain IP from first target for validation
			probeIP := extractIPFromTarget(targets[0])
			blocked, err := h.faultProvider.VerifyConnectivity(ctx, probeIP, true, baseline)
			if err == nil && blocked {
				// Validation succeeded: connectivity is blocked as expected
				h.targets = targets // Store for RemoveFence
				return nil
			}
		}
	}
}

// RemoveFence removes iptables fence rules and validates that connectivity is restored.
// Process:
//  1. Remove UnfenceIP rules for each target
//  2. Allow 5s for iptables rules to be removed on all pods
//  3. Validate that targets are reachable again (poll 120s timeout, 2s interval)
//  4. Return success once connectivity restored
func (h *IptablesHandler) RemoveFence(ctx context.Context) error {
	if len(h.targets) == 0 {
		return fmt.Errorf("no targets to unfence (fence was not applied)")
	}

	// 1. Remove UnfenceIP rules for each target
	for _, cidr := range h.targets {
		if err := h.faultProvider.UnfenceIP(ctx, cidr, nil); err != nil {
			return fmt.Errorf("unfence CIDR %s: %w", cidr, err)
		}
	}

	// 2. Allow iptables rules to be removed on all DaemonSet pods (existing value: 5s)
	time.Sleep(5 * time.Second)

	// 3. Validate unfence is complete: verify connectivity is restored (timeout 120s, poll 2s)
	validationTimeout := time.NewTimer(120 * time.Second)
	defer validationTimeout.Stop()
	validationTicker := time.NewTicker(2 * time.Second)
	defer validationTicker.Stop()

	for {
		select {
		case <-validationTimeout.C:
			return fmt.Errorf("timeout: iptables unfence validation failed after 120s (targets still blocked)")
		case <-validationTicker.C:
			// Use VerifyConnectivity with expectedFenced=false to check if unblocking works
			// If all targets are reachable (unfenced), restored=true
			// Use plain IP from first target for validation
			probeIP := extractIPFromTarget(h.targets[0])
			restored, err := h.faultProvider.VerifyConnectivity(ctx, probeIP, false, h.baseline)
			if err == nil && restored {
				// Validation succeeded: connectivity is restored as expected
				return nil
			}
		}
	}
}

// IsSupported checks if iptables fault injection is supported in the cluster.
// Returns false if the cluster lacks privileged DaemonSet support.
func (h *IptablesHandler) IsSupported(ctx context.Context) (bool, string) {
	if !h.faultProvider.IsSupported(ctx) {
		return false, "iptables fault injection requires privileged DaemonSet support"
	}
	return true, ""
}

// Cleanup performs cleanup of iptables DaemonSet and iptables rules.
// Removes any active iptables REJECT rules (with surgical cleanup to preserve other rules)
// and deletes the DaemonSet and ConfigMaps created during fault injection.
func (h *IptablesHandler) Cleanup(ctx context.Context) error {
	if h.faultProvider != nil {
		return h.faultProvider.Cleanup(ctx)
	}
	return nil
}

// DiscoverFenceTargets discovers fence targets for iptables fault injection.
// Implements the unified discovery logic from L1-E-003:
// - For single-cluster: calls DiscoverFenceCIDRsForIptables (GetFenceCIDRsForFaultInjection logic)
// - For full-DR: calls DiscoverFenceCIDRsForIptablesPeer (GetFenceCIDRsForFaultInjectionPeer logic)
//
// Order of discovery:
//  1. FENCE_CIDRS environment variable (explicit targets)
//  2. FENCE_TARGET_SERVICES backend IPs (port-aware service discovery)
//  3. Auto-discovered endpoints from default namespaces
//  4. Returns empty list if nothing found (test caller should skip)
//
// DiscoverFenceTargets discovers fence targets for iptables fault injection.
// Implements the unified discovery logic from L1-E-003:
// - For single-cluster: calls DiscoverFenceCIDRsForIptables (GetFenceCIDRsForFaultInjection logic)
// - For full-DR: calls DiscoverFenceCIDRsForIptablesPeer (GetFenceCIDRsForFaultInjectionPeer logic)
//
// Order of discovery:
//  1. FENCE_CIDRS environment variable (explicit targets)
//  2. FENCE_TARGET_SERVICES backend IPs (port-aware service discovery)
//  3. Auto-discovered endpoints from default namespaces
//  4. Returns empty list if nothing found (test caller should skip)
//
// Note: For full-DR iptables, GetFenceCIDRsForIptablesPeer requires access to both
// fencing-cluster and peer-cluster clients. PeerClient must be set in config for full-DR.
func (h *IptablesHandler) DiscoverFenceTargets(ctx context.Context) []string {
	// Check if full-DR mode is enabled
	if IsFullDRMode() {
		// For full-DR iptables, use both fencing and peer clients
		if h.config.PeerClient != nil {
			return DiscoverFenceCIDRsForIptablesPeer(ctx, h.config.Client, h.config.PeerClient)
		}
		// If peer client not provided, fall back to FENCE_CIDRS env
		Logf("[WARN]", "IptablesHandler.DiscoverFenceTargets: Full-DR mode detected but PeerClient not set in config; falling back to FENCE_CIDRS environment variable")
		return parseFenceCIDRSFromEnv()
	}

	// Single-cluster iptables: use full discovery chain
	// This is the same logic as L1-E-003 for single-cluster:
	// FENCE_CIDRS env → service backends → auto-discovered endpoints → none
	return DiscoverFenceCIDRsForIptables(ctx, h.config.Client)
}
