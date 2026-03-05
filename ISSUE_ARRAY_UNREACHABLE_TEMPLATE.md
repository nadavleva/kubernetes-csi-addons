# Test Infrastructure: Array/Storage Unreachability Simulation Support Required

## Issue Title
**Test Infrastructure Gap: Support for Array/Storage Unreachability Simulation in E2E Tests**

## Description

Four E2E test scenarios require the ability to simulate storage array/backend unavailability on the local cluster, but this capability is not yet implemented in the test infrastructure.

## Current Gap

The existing test infrastructure can simulate:
- ✅ **Network Fencing**: Using NetworkFence CRDs to block network connectivity between peer clusters
- ❌ **Storage Unreachability**: NO mechanism to simulate local storage array becoming unreachable or offline

### Key Distinction
- **NetworkFence**: Blocks network access between clusters but storage remains accessible locally
- **Array Unreachable**: Local storage backend becomes unavailable (e.g., Ceph pool offline, storage array power failure)

These are fundamentally different failure modes with different expected behaviors.

## Affected Tests

### Promote Operations (2 tests)
1. **L1-PROM-005**: Promote secondary to primary with array unreachable (force=false)
   - Location: [promote_volumereplication_test.go#L678-L694](https://github.com/nadavleva/kubernetes-csi-addons/blob/csicerttests/test/e2e/replication/promote_volumereplication_test.go#L678-L694)
   - Test behavior: secondary volume should fail promotion if local storage is unavailable

2. **L1-PROM-006**: Promote secondary to primary with array unreachable (force=true)
   - Location: [promote_volumereplication_test.go#L698-L715](https://github.com/nadavleva/kubernetes-csi-addons/blob/csicerttests/test/e2e/replication/promote_volumereplication_test.go#L698-L715)
   - Test behavior: force=true should NOT override storage layer unavailability

### Demote Operations (2 tests)
3. **L1-DEM-005**: Demote primary to secondary with array unreachable (force=false)
   - Location: [demote_volumereplication_test.go#L672-L689](https://github.com/nadavleva/kubernetes-csi-addons/blob/csicerttests/test/e2e/replication/demote_volumereplication_test.go#L672-L689)
   - Test behavior: primary volume should fail demotion if local storage is unavailable

4. **L1-DEM-006**: Demote primary to secondary with array unreachable (force=true)
   - Location: [demote_volumereplication_test.go#L690-L708](https://github.com/nadavleva/kubernetes-csi-addons/blob/csicerttests/test/e2e/replication/demote_volumereplication_test.go#L690-L708)
   - Test behavior: force=true should NOT override storage layer unavailability

## Expected Test Behavior

### Promote with Array Unreachable
**Current State**: Secondary volume exists on DR2
**Storage Condition**: Local array/pool offline or unreachable

**Expected Result** (force=false):
- CSI driver reports volume unavailable or I/O error
- Controller cannot execute PromoteVolume RPC (storage access required)
- VR Status: `Degraded=True`, `FailedToPromote` reason
- Operation fails and waits for storage recovery

**Expected Result** (force=true):
- Same as force=false
- Reason: force parameter affects peer coordination, NOT storage layer access
- Storage unavailability cannot be overridden by any force flag

### Demote with Array Unreachable
**Current State**: Primary volume exists on DR1
**Storage Condition**: Local array/pool offline or unreachable

**Expected Result** (force=false):
- CSI driver reports volume unavailable or I/O error
- DemoteVolume RPC requires storage access to coordinate state
- VR Status: `Degraded=True`, `FailedToDemote` reason
- Operation fails until storage recovers

**Expected Result** (force=true):
- Same as force=false
- Force parameter cannot overcome storage layer limitations

## Implementation Requirements

### Option 1: Driver-Specific Storage Simulation
Implement storage unreachability for the specific driver (e.g., Ceph RBD):
- Add mechanism to take Ceph pool offline
- Use `rbd pool` commands or direct API calls
- Requires storage-specific knowledge

### Option 2: Mock CSI Driver Hooks
Create injectable CSI driver hooks that simulate storage errors:
- Modify test CSI driver to optionally return `UNAVAILABLE` errors
- Can be toggled on/off for specific test phases
- Requires CSI driver modifications

### Option 3: Test Infrastructure Extension
Extend test harness with storage error injection:
- Create abstract storage fault injection interface
- Implement for different storage backends
- Allow tests to trigger storage unavailability state

### Option 4: Container-Level Simulation
Use container manipulation to simulate storage unavailability:
- Docker/container commands to block device access
- Network-level blocking of storage endpoints
- Kubernetes node drain or pod eviction

## Relationship to Other Issues

- Related to: #6 (Phase 5: Add Integrated DR Workflow E2E Tests) - parent test suite epic
- Different from: #7 (RBD mirror degraded force promote) - focuses on peer coordination issue
- NetworkFence support (already implemented): Handles peer unreachability, not storage unreachability

## Success Criteria

✅ Implement storage unreachability simulation mechanism  
✅ Unskip L1-PROM-005, L1-PROM-006, L1-DEM-005, L1-DEM-006  
✅ Tests verify CSI driver behavior when storage is unavailable  
✅ Confirm force parameter cannot override storage layer failures  
✅ Document the storage simulation approach for future test development  

## Priority
Medium - Completes full DR failure scenario coverage, but force promote issue (#7) is higher priority

## Labels
- area: test-infrastructure
- area: replication-e2e
- type: enhancement
- status: blocked-on-test-infrastructure
