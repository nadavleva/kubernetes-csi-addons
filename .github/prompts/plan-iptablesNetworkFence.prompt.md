# Plan: Implement iptables-based NetworkFence for E2E Tests

Implement vendor-agnostic network fencing using iptables instead of NetworkFence CRDs, providing IP blocking capabilities for peer addresses, storage pods, and service IPs through a privileged DaemonSet with configurable backend selection.

## Implementation Steps

### Phase 1: Core Infrastructure and Capability Detection

1. **Add privileged DaemonSet capability detection** in [test/e2e/replication/suite_test.go](test/e2e/replication/suite_test.go)
   - ✅ Add `privilegedDaemonSetCached` variable to track capability
   - ✅ Add `IsPrivilegedDaemonSetSupportAvailable()` function 
   - 🔄 Implement `HasPrivilegedDaemonSetSupport()` function in helpers
   - 🔄 Test privileged DaemonSet creation and cleanup in BeforeSuite

2. **Create helper functions** in [test/e2e/helpers](test/e2e/helpers) directory
   - 🔄 Add `HasPrivilegedDaemonSetSupport()` - tests cluster's ability to create privileged DaemonSets
   - 🔄 Add utility functions for DaemonSet deployment and cleanup
   - 🔄 Add node selection and IP detection helpers

### Phase 2: Fault Injection Framework

3. **Create fault injection framework** in [test/e2e/helpers](test/e2e/helpers)
   - 🔄 Define `PeerFenceProvider` interface with `FenceIP()`, `UnfenceIP()`, `VerifyConnectivity()` methods
   - 🔄 Implement `NetworkFenceFaultProvider` (existing NetworkFence CRD backend)
   - 🔄 Implement `IptablesFaultProvider` (new iptables backend)
   - 🔄 Add factory function with `E2E_FAULT_INJECTOR` environment variable support (networkfence|iptables|none)

### Phase 3: iptables Backend Implementation

4. **Implement iptables backend** with privileged DaemonSet
   - 🔄 Create DaemonSet manifest template with `hostNetwork: true` + `NET_ADMIN` capabilities
   - 🔄 Implement node targeting strategy (storage nodes vs. all nodes vs. configurable labels)
   - 🔄 Add iptables rule execution via Kubernetes exec API
   - 🔄 Implement IP blocking/unblocking with `iptables -I OUTPUT -d <CIDR> -j REJECT/DROP`

5. **Add lifecycle management** in BeforeSuite/AfterSuite
   - 🔄 Dynamic DaemonSet deployment during BeforeSuite
   - 🔄 Resource tracking using existing `Framework.TrackResource()` patterns
   - 🔄 Emergency cleanup handlers for forced termination
   - 🔄 Per-test cleanup verification and iptables rule removal

### Phase 4: Integration and Testing

6. **Integrate IP blocking operations** 
   - 🔄 CIDR management and node IP detection
   - 🔄 Connectivity verification matching existing NetworkFence patterns
   - 🔄 Error handling and retry logic for iptables operations
   - 🔄 Logging and debugging support for fault injection

7. **Update existing peer-unreachable tests** in [test/e2e/replication](test/e2e/replication)
   - 🔄 Modify tests to use new fault injection framework
   - 🔄 Add capability detection with graceful test skipping
   - 🔄 Update test documentation and examples

### Phase 5: Configuration and Documentation

8. **Add configuration support**
   - 🔄 New environment variables: `E2E_FAULT_INJECTOR`, `E2E_IPTABLES_TARGET_NODES`
   - 🔄 Integration with existing test configuration YAML
   - 🔄 Security context requirements documentation

9. **Add comprehensive documentation**
   - 🔄 Usage examples across test suites
   - 🔄 Troubleshooting guide for privileged container issues
   - 🔄 Security implications and cluster requirements

## Technical Decisions Made

### Image Strategy
- **Decision**: Use minimal iptables-enabled container (e.g., `alpine:latest` with iptables package)
- **Rationale**: Smaller attack surface, faster deployment, dedicated purpose

### Node Targeting Approach  
- **Decision**: Target all nodes by default, with configurable node selector via `E2E_IPTABLES_TARGET_NODES`
- **Rationale**: Maximum flexibility while maintaining simplicity for common cases

### Rule Persistence Strategy
- **Decision**: Temporary in-memory rules with explicit cleanup validation
- **Rationale**: Safer for test environments, explicit cleanup reduces test interference

### Security Context Requirements
```yaml
securityContext:
  privileged: true
  capabilities:
    add: ["NET_ADMIN"]
hostNetwork: true
```

## Current Status
- ✅ Suite-level capability detection variables added
- ✅ `IsPrivilegedDaemonSetSupportAvailable()` function implemented
- ✅ `HasPrivilegedDaemonSetSupport()` function implemented and compiling
- ✅ Fault injection framework interface created
- ✅ Iptables backend provider implemented (simplified version)
- ✅ Factory function and environment variable support added
- ✅ NetworkFence provider stub created
- ✅ Integration tests created and compiling

## Environment Variables
```bash
# Fault injection backend selection
E2E_FAULT_INJECTOR=networkfence|iptables|none  # Default: networkfence (backward compatibility)

# iptables-specific configuration  
E2E_IPTABLES_TARGET_NODES=storage-nodes        # Node selector for DaemonSet deployment
E2E_IPTABLES_IMAGE=alpine:latest               # Container image for iptables operations
E2E_IPTABLES_CLEANUP_TIMEOUT=30s               # Timeout for rule cleanup operations
```

## Next Immediate Steps
1. Implement `HasPrivilegedDaemonSetSupport()` function to fix compile error
2. Create helper functions for DaemonSet operations
3. Implement the `PeerFenceProvider` interface and factory
4. Build and test the iptables backend implementation
