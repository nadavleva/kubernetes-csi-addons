# CSI-Addons Replication Testing Gap Analysis and Implementation Guide

This document provides a detailed mapping of existing replication tests, identifies gaps against the Layer-1 VR/VRG test matrices, and provides recommendations for implementing comprehensive test coverage.

## 1. Current Replication Tests Location and Mapping

### 1.1 Volume Replication Tests

#### Core API Client Tests
**Location**: [`internal/client/volume-replication_test.go`](../../internal/client/volume-replication_test.go)

| Test Function | API Operation | Current Coverage | Lines |
|---------------|---------------|------------------|-------|
| `TestEnableVolumeReplication` | EnableVolumeReplication | Success case, Error handling | 29-51 |
| `TestDisableVolumeReplication` | DisableVolumeReplication | Success case, Error handling | 53-76 |
| `TestPromoteVolume` | PromoteVolume | Success case, Force parameter, Error handling | 77-99 |
| `TestDemoteVolume` | DemoteVolume | Success case, Error handling | 101-123 |
| `TestResyncVolume` | ResyncVolume | Success case, Error handling | 125-147 |
| `TestGetVolumeReplicationInfo` | GetVolumeReplicationInfo | Non-existent volume error handling | ✅ Implemented |

#### Controller Integration Tests
**Location**: [`internal/controller/replication.storage/volumereplication_test.go`](../../internal/controller/replication.storage/volumereplication_test.go)

| Test Function | Component | Coverage | Lines |
|---------------|-----------|----------|-------|
| `TestGetScheduledTime` | Schedule Parser | Valid intervals, Default handling, Invalid parameters | 25-45 |

#### Controller Reconciliation Tests
**Location**: [`internal/controller/replication.storage/volumereplication_controller.go`](../../internal/controller/replication.storage/volumereplication_controller.go)

| Function | Replication State | Coverage Area |
|----------|------------------|---------------|
| `markVolumeAsPrimary` | Primary | Promotion logic, Force promotion | 
| `markVolumeAsSecondary` | Secondary | Demotion logic |
| `resyncVolume` | Resync | Resync operations |
| `enableReplication` | All | Initial enable |
| `disableVolumeReplication` | Cleanup | Disable on deletion |

#### PVC and Resource Management Tests
**Location**: [`internal/controller/replication.storage/pvc_test.go`](../../internal/controller/replication.storage/pvc_test.go)

| Test Function | Component | Coverage | Lines |
|---------------|-----------|----------|-------|
| `TestGetVolumeHandle` | Volume Handle Resolution | PVC/PV relationship validation | 108-150 |
| `TestVolumeReplicationReconciler_annotatePVCWithOwner` | PVC Annotation | Owner annotation management | 180-220 |

#### Volume Replication Class Tests
**Location**: [`internal/controller/replication.storage/volumereplicationclass_test.go`](../../internal/controller/replication.storage/volumereplicationclass_test.go)

| Test Function | Component | Coverage | Lines |
|---------------|-----------|----------|-------|
| `TestGetVolumeReplicaClass` | VRC Management | Class retrieval, Not found scenarios | 41-70 |

### 1.2 Volume Group Replication Tests

#### Volume Group Replication Tests
**Location**: [`internal/controller/replication.storage/volumegroupreplication_test.go`](../../internal/controller/replication.storage/volumegroupreplication_test.go)

| Test Function | Component | Coverage | Lines |
|---------------|-----------|----------|-------|
| `TestVolumeGroupReplication` | VGR Controller | Group workflows, PVC matching | 107-200 |

#### Volume Group Replication Class Tests  
**Location**: [`internal/controller/replication.storage/volumegroupreplicationclass_test.go`](../../internal/controller/replication.storage/volumegroupreplicationclass_test.go)

| Test Function | Component | Coverage | Lines |
|---------------|-----------|----------|-------|
| `TestGetVolumeGroupReplicationClass` | VGRC Management | Class retrieval validation | 39-70 |

#### Volume Group Client Tests
**Location**: [`internal/client/volumegroup-client_test.go`](../../internal/client/volumegroup-client_test.go)

| Test Function | API Operation | Coverage | Lines |
|---------------|---------------|----------|-------|
| `TestCreateVolumeGroup` | CreateVolumeGroup | Success, Error scenarios | 30-50 |
| `TestDeleteVolumeGroup` | DeleteVolumeGroup | Success, Error scenarios | 52-72 |
| `TestModifyVolumeGroupMembership` | ModifyVolumeGroupMembership | Success, Error scenarios | 74-94 |
| `TestControllerGetVolumeGroup` | ControllerGetVolumeGroup | Success, Error scenarios | 96-116 |

### 1.3 Sidecar Service Tests

#### Replication Source Configuration Tests
**Location**: [`internal/sidecar/service/volumereplication_test.go`](../../internal/sidecar/service/volumereplication_test.go)

| Test Function | Component | Coverage | Lines |
|---------------|-----------|----------|-------|
| `Test_setReplicationSource` | Replication Source Config | Volume/VolumeGroup source setup, Nil handling | 25-108 |

### 1.4 Supporting Infrastructure Tests

#### Error Handling Tests
**Location**: [`internal/util/error_test.go`](../../internal/util/error_test.go)

| Test Function | Component | Coverage | Lines |
|---------------|-----------|----------|-------|
| `TestGetErrorMessage` | Error Processing | Error message extraction | 26-35 |
| `TestIsUmplementedError` | Error Classification | Unimplemented error detection | 37-45 |

#### Replication Logic Tests
**Location**: [`internal/controller/replication.storage/replication/replication_test.go`](../../internal/controller/replication.storage/replication/replication_test.go)

| Test Function | Component | Coverage | Lines |
|---------------|-----------|----------|-------|
| `TestGetMessageFromError` | Error Message Processing | gRPC error message extraction | 25-40 |

## 2. Gap Analysis Against Layer-1 Test Matrices

### 2.1 Volume Replication API Coverage Gaps

Based on the [Layer-1 VR Test Matrix](https://github.com/nadavleva/csi_replication_certs/blob/main/docs/layer-1-vr-tests.md):

#### EnableVolumeReplication API Gaps

| Test ID | Scenario | Current Status | Implementation Gap |
|---------|----------|----------------|-------------------|
| **L1-VR-EN-001** | Enable replication with snapshot mode | **Missing** | Parameter validation needed |
| **L1-VR-EN-002** | Enable replication with journal mode | **Missing** | Parameter validation needed |
| **L1-VR-EN-003** | Enable with peer cluster unreachable | **Missing** | Network failure scenarios |
| **L1-VR-EN-004** | Invalid schedulingInterval parameter | **Partial** | Limited validation in `TestGetScheduledTime` |
| **L1-VR-EN-005** | Enable already enabled volume (idempotent) | **Missing** | Idempotency testing |
| **L1-VR-EN-006** | Missing/invalid secret reference | **Missing** | Authentication failure scenarios |
| **L1-VR-EN-007** | Invalid mirroringMode parameter | **Missing** | Parameter validation |
| **L1-VR-EN-008** | Future schedulingStartTime | **Partial** | Basic parsing in schedule tests |
| **L1-VR-EN-009** | Invalid schedulingStartTime format | **Partial** | Basic validation exists |

**Coverage**: 2/9 scenarios (22%) - **78% gap**

#### DisableVolumeReplication API Gaps

| Test ID | Scenario | Current Status | Implementation Gap |
|---------|----------|----------------|-------------------|
| **L1-VR-DIS-001** | Disable active primary, peer healthy | **Basic** | Success case covered |
| **L1-VR-DIS-002** | Disable secondary, peer healthy | **Missing** | Role-specific testing |
| **L1-VR-DIS-003** | Disable already disabled (idempotent) | **Missing** | Idempotency validation |
| **L1-VR-DIS-004** | Disable with peer unreachable | **Missing** | Network failure scenarios |
| **L1-VR-DIS-005** | Force disable scenarios | **Missing** | Force parameter testing |
| **L1-VR-DIS-006-016** | Additional disable scenarios | **Missing** | Extended test coverage |

**Coverage**: 1/16 scenarios (6%) - **94% gap**

#### PromoteVolume API Gaps

| Test ID | Scenario | Current Status | Implementation Gap |
|---------|----------|----------------|-------------------|
| **L1-VR-PROM-001** | Promote secondary→primary, healthy | **Basic** | Success case covered |
| **L1-VR-PROM-002** | Promote already primary (idempotent) | **Missing** | Idempotency testing |
| **L1-VR-PROM-003** | Promote with peer unreachable, force=false | **Missing** | Network failure scenarios |
| **L1-VR-PROM-004** | Force promote with peer unreachable | **Partial** | Force param exists, scenarios missing |
| **L1-VR-PROM-005-008** | Workload and advanced scenarios | **Missing** | Advanced promotion scenarios |

**Coverage**: 1.5/8 scenarios (19%) - **81% gap**

#### DemoteVolume API Gaps

| Test ID | Scenario | Current Status | Implementation Gap |
|---------|----------|----------------|-------------------|
| **L1-VR-DEM-001** | Demote primary→secondary, healthy | **Basic** | Success case covered |
| **L1-VR-DEM-002** | Demote already secondary (idempotent) | **Missing** | Idempotency testing |
| **L1-VR-DEM-003** | Demote with peer unreachable | **Missing** | Network failure scenarios |
| **L1-VR-DEM-004-008** | Force demote and workload scenarios | **Missing** | Advanced demotion scenarios |

**Coverage**: 1/8 scenarios (12.5%) - **87.5% gap**

#### ResyncVolume API Gaps

| Test ID | Scenario | Current Status | Implementation Gap |
|---------|----------|----------------|-------------------|
| **L1-VR-RSYNC-001** | Resync after split-brain scenario | **Missing** | Conflict resolution testing |
| **L1-VR-RSYNC-002** | Resync with network issues | **Missing** | Network failure scenarios |
| **L1-VR-RSYNC-003** | Force resync scenarios | **Missing** | Force parameter testing |

**Coverage**: 0/3+ scenarios (0%) - **100% gap**

#### GetVolumeReplicationInfo API Gaps

| Test ID | Scenario | Current Status | Implementation |
|---------|----------|----------------|-----------------|
| **L1-INFO-001-014** | Info query scenarios | ✅ **Implemented** | Integrated with EnableVolumeReplication E2E tests |
| **L1-INFO-008** | Non-existent volume | ✅ **Implemented** | Standalone E2E test in `test/e2e/replication/get_volumereplication_info_test.go` |

**Coverage**: 15/14+ scenarios - ✅ **Complete API tested** (E2E suite + standalone tests)

### 2.2 Volume Group Replication Coverage Gaps

Based on the [Layer-1 VRG Test Matrix](https://github.com/nadavleva/csi_replication_certs/blob/main/docs/layer-1-vrg-tests.md):

#### VolumeGroup API Coverage Gaps

| Test Category | Current Implementation | Missing Scenarios | Gap |
|---------------|----------------------|-------------------|-----|
| **CreateVolumeGroup** | Basic success/error (TestCreateVolumeGroup) | Parameter validation, capacity limits, member validation | **75%** |
| **DeleteVolumeGroup** | Basic success/error (TestDeleteVolumeGroup) | Force delete, dependency checking, cleanup validation | **80%** |
| **ModifyVolumeGroupMembership** | Basic success/error (TestModifyVolumeGroupMembership) | Concurrent modifications, capacity validation | **70%** |
| **ControllerGetVolumeGroup** | Basic success/error (TestControllerGetVolumeGroup) | Status reporting, member enumeration | **60%** |

#### VolumeGroup Replication API Gaps

| API Operation | Current Status | Missing Test Coverage | Gap |
|---------------|----------------|----------------------|-----|
| **EnableVolumeGroupReplication** | **Missing entirely** | All scenarios from VRG test matrix | **100%** |
| **DisableVolumeGroupReplication** | **Missing entirely** | All scenarios from VRG test matrix | **100%** |
| **PromoteVolumeGroup** | **Missing entirely** | All scenarios from VRG test matrix | **100%** |
| **DemoteVolumeGroup** | **Missing entirely** | All scenarios from VRG test matrix | **100%** |
| **ResyncVolumeGroup** | **Missing entirely** | All scenarios from VRG test matrix | **100%** |
| **GetVolumeGroupReplicationInfo** | **Missing entirely** | All scenarios from VRG test matrix | **100%** |

### 2.3 Overall Test Coverage Summary

| Component | Available Tests | Required Tests (Layer-1) | Coverage | Gap |
|-----------|----------------|-------------------------|----------|-----|
| **Volume Replication APIs** | 10 basic tests | 57+ comprehensive scenarios | **18%** | **82%** |
| **Volume Group APIs** | 4 basic tests | 25+ comprehensive scenarios | **16%** | **84%** |
| **Volume Group Replication APIs** | 0 tests | 45+ comprehensive scenarios | **0%** | **100%** |
| **Integration/E2E** | Controller tests only | Full gRPC + storage integration | **30%** | **70%** |
| **Overall Coverage** | **14 test functions** | **127+ test scenarios** | **11%** | **89%** |

## 3. Recommendations for Porting PR Tests

### 3.1 Analysis of PR Test Structure

Based on the [PR test implementations](https://github.com/nadavleva/kubernetes_csiaddontests/pull/1), the tests follow this pattern:

```go
// Example test structure from PR
func TestEnableVolumeReplication_SnapshotMode(t *testing.T) {
    // Setup test environment
    // Create VolumeReplication CR with specific parameters
    // Verify controller behavior
    // Validate expected outcomes
}
```

### 3.2 Integration Strategy

#### Phase 1: Port Existing PR Tests (Immediate)

**Target Location**: [`internal/client/volume-replication_test.go`](../../internal/client/volume-replication_test.go)

1. **Extend existing test functions** to cover multiple scenarios:
```go
func TestEnableVolumeReplication(t *testing.T) {
    testCases := []struct {
        name string
        scenario string
        parameters map[string]string
        expectError bool
        errorType codes.Code
    }{
        // Port scenarios from PR tests
        {"EnableSnapshotMode", "L1-VR-EN-001", snapShotParams, false, codes.OK},
        {"EnableJournalMode", "L1-VR-EN-002", journalParams, false, codes.OK},
        {"PeerUnreachable", "L1-VR-EN-003", baseParams, true, codes.Unavailable},
        // ... more scenarios
    }
}
```

2. **Add missing API test functions**:
```go
func TestGetVolumeReplicationInfo(t *testing.T) {
    // Port all L1-VR-INFO scenarios from PR
}
```

#### Phase 2: Enhance Controller Integration Tests

**Target Location**: [`internal/controller/replication.storage/volumereplication_controller_test.go`](../../internal/controller/replication.storage/volumereplication_controller_test.go) (new file)

```go
func TestVolumeReplicationController_EnableReplication(t *testing.T) {
    // Port controller-level scenarios from PR
    // Test actual reconciliation loops with various parameters
}

func TestVolumeReplicationController_StateTransitions(t *testing.T) {
    // Test Primary ↔ Secondary ↔ Resync state transitions
    // Port idempotency scenarios from PR
}
```

#### Phase 3: Add Volume Group Replication Tests

**Target Location**: [`internal/client/volumegroup-replication_test.go`](../../internal/client/volumegroup-replication_test.go) (new file)

```go
func TestEnableVolumeGroupReplication(t *testing.T) {
    // Port all VRG enable scenarios from Layer-1 matrix
}

func TestVolumeGroupReplicationStateTransitions(t *testing.T) {
    // Port VRG state management scenarios
}
```

### 3.3 Test Porting Guidelines

#### 3.3.1 Mock Client Enhancement

**Location**: [`internal/client/fake/replication.go`](../../internal/client/fake/replication.go)

Add support for scenario-based responses:
```go
type ReplicationClient struct {
    // Enhanced mock functions with scenario support
    EnableVolumeReplicationMock func(scenario string, volumeID, replicationID string, secretName, secretNamespace string, parameters map[string]string) (*proto.EnableVolumeReplicationResponse, error)
    
    // Add missing API mocks
    GetVolumeReplicationInfoMock func(volumeID, replicationID string, secretName, secretNamespace string) (*proto.GetVolumeReplicationInfoResponse, error)
}
```

#### 3.3.2 Parameter Validation Tests

Create dedicated parameter validation test suite:
**Location**: [`internal/controller/replication.storage/parameter_validation_test.go`](../../internal/controller/replication.storage/parameter_validation_test.go) (new file)

```go
func TestValidateReplicationParameters(t *testing.T) {
    // Port parameter validation scenarios from PR
    invalidParams := []struct {
        name string
        params map[string]string
        expectedError string
    }{
        {"InvalidInterval", map[string]string{"schedulingInterval": "invalid"}, "invalid duration"},
        {"MissingSecret", map[string]string{}, "secret not specified"},
        // ... port all parameter validation scenarios
    }
}
```

#### 3.3.3 Network Failure Simulation

Add network failure testing support:
**Location**: [`internal/client/fake/network_simulator.go`](../../internal/client/fake/network_simulator.go) (new file)

```go
type NetworkSimulator struct {
    // Simulate network conditions from PR tests
    SimulateUnreachablePeer bool
    SimulateTimeout         bool
    SimulatePartialFailure  bool
}
```

## 4. Layer-1 Test Implementation Strategy

### 4.1 New Test Suite Structure

Create comprehensive test suites aligned with Layer-1 certification requirements:

```
internal/
├── client/
│   ├── layer1/                              # New Layer-1 test directory
│   │   ├── volume_replication_l1_test.go    # L1 VR API tests
│   │   ├── volume_group_l1_test.go          # L1 VG API tests  
│   │   ├── volume_group_replication_l1_test.go # L1 VGR API tests
│   │   └── test_matrix.go                   # Test scenario definitions
│   └── fake/
│       ├── layer1_client.go                 # Enhanced fake client for L1
│       └── network_simulator.go             # Network condition simulation
├── controller/
│   └── replication.storage/
│       ├── layer1/                          # New L1 controller tests
│       │   ├── integration_l1_test.go       # L1 integration scenarios
│       │   ├── state_transition_l1_test.go  # L1 state management tests
│       │   └── idempotency_l1_test.go       # L1 idempotency validation
└── e2e/                                     # New E2E test directory
    ├── layer1_certification_test.go         # Full L1 certification suite
    └── real_driver_integration_test.go      # Real CSI driver tests
```

### 4.2 Test Matrix Implementation

#### 4.2.1 Scenario-Driven Test Framework

**Location**: [`internal/client/layer1/test_matrix.go`](../../internal/client/layer1/test_matrix.go)

```go
type TestScenario struct {
    ID          string                    // L1-VR-EN-001
    Name        string                   // "Enable replication with snapshot mode"
    API         string                   // "EnableVolumeReplication"
    Parameters  map[string]string        // Test-specific parameters
    Preconditions []string               // Setup requirements
    ExpectedResult ExpectedOutcome       // Success/Error expectations
    NetworkCondition NetworkState        // Healthy/Unreachable/Timeout
    ValidationSteps []ValidationStep     // Post-test validation
}

var VolumeReplicationL1Matrix = []TestScenario{
    // Port all scenarios from layer-1-vr-tests.md
    {
        ID: "L1-VR-EN-001",
        Name: "Enable replication with snapshot mode",
        API: "EnableVolumeReplication", 
        Parameters: map[string]string{"mirroringMode": "snapshot"},
        // ... complete scenario definition
    },
    // ... all 57+ scenarios
}
```

#### 4.2.2 Automated Test Generation

Create test generators that automatically create test functions from the matrix:

**Location**: [`internal/client/layer1/generator.go`](../../internal/client/layer1/generator.go)

```go
//go:generate go run generator.go

func GenerateL1Tests() {
    // Automatically generate test functions from TestScenario matrix
    // Creates comprehensive test coverage from scenario definitions
}
```

### 4.3 Enhanced Mock Infrastructure

#### 4.3.1 Scenario-Aware Mock Client

**Location**: [`internal/client/fake/layer1_client.go`](../../internal/client/fake/layer1_client.go)

```go
type Layer1ReplicationClient struct {
    *ReplicationClient
    
    // Scenario-based response configuration
    ScenarioResponses map[string]ScenarioResponse
    NetworkSimulator  *NetworkSimulator
    StateTracker     *ReplicationStateTracker
}

type ScenarioResponse struct {
    Response interface{}
    Error    error
    Delay    time.Duration
    NetworkState NetworkCondition
}

func (c *Layer1ReplicationClient) EnableVolumeReplication(scenarioID string, params map[string]string) (*proto.EnableVolumeReplicationResponse, error) {
    // Route to appropriate scenario response
    scenario := c.ScenarioResponses[scenarioID]
    
    // Simulate network conditions
    if err := c.NetworkSimulator.SimulateCondition(scenario.NetworkState); err != nil {
        return nil, err
    }
    
    // Return scenario-specific response
    return scenario.Response.(*proto.EnableVolumeReplicationResponse), scenario.Error
}
```

#### 4.3.2 State Tracking for Idempotency Tests

```go
type ReplicationStateTracker struct {
    VolumeStates map[string]ReplicationState
    GroupStates  map[string]ReplicationState
}

func (t *ReplicationStateTracker) ValidateIdempotency(operation string, volumeID string) error {
    // Track state changes and validate idempotent behavior
}
```

### 4.4 Integration Test Implementation

#### 4.4.1 Controller Integration with Layer-1 Scenarios

**Location**: [`internal/controller/replication.storage/layer1/integration_l1_test.go`](../../internal/controller/replication.storage/layer1/integration_l1_test.go)

```go
func TestVolumeReplicationController_L1Scenarios(t *testing.T) {
    for _, scenario := range VolumeReplicationL1Matrix {
        t.Run(scenario.ID, func(t *testing.T) {
            // Setup controller with scenario-specific mock client
            controller := setupControllerWithL1Mock(scenario)
            
            // Create VolumeReplication CR with scenario parameters  
            vr := createVRWithScenario(scenario)
            
            // Execute reconciliation
            result, err := controller.Reconcile(ctx, reconcile.Request{NamespacedName: vr.NamespacedName()})
            
            // Validate results against scenario expectations
            validateScenarioOutcome(t, scenario, result, err)
        })
    }
}
```

#### 4.4.2 Real Driver Integration Tests

**Location**: [`internal/e2e/real_driver_integration_test.go`](../../internal/e2e/real_driver_integration_test.go)

```go
func TestL1CertificationWithRealDriver(t *testing.T) {
    if !useExistingCluster() {
        t.Skip("Real driver tests require USE_EXISTING_CLUSTER=true")
    }
    
    // Discover available CSI drivers with replication capability
    drivers := discoverReplicationCapableDrivers(t)
    
    for _, driver := range drivers {
        t.Run(driver.Name, func(t *testing.T) {
            // Run Layer-1 certification suite against real driver
            runL1CertificationSuite(t, driver)
        })
    }
}
```

### 4.5 Test Execution and Validation

#### 4.5.1 Layer-1 Test Runner

**Location**: [`cmd/l1-test-runner/main.go`](../../cmd/l1-test-runner/main.go)

```go
func main() {
    // Command-line tool for running Layer-1 certification tests
    // Supports both mock and real driver modes
    // Generates certification reports
}
```

#### 4.5.2 Certification Report Generation

```go
type CertificationReport struct {
    TestSuite    string
    TotalTests   int
    PassedTests  int
    FailedTests  int
    Scenarios    []ScenarioResult
    Coverage     CoverageMetrics
}

func GenerateL1CertificationReport(results []TestResult) *CertificationReport {
    // Generate comprehensive certification report
    // Map results back to Layer-1 test matrix requirements
}
```

### 4.6 Implementation Phases

#### Phase 1: Foundation (Weeks 1-2)
1. Create new test directory structure
2. Implement test matrix definitions from Layer-1 specifications
3. Enhance mock client with scenario support
4. Port critical API tests from PR

#### Phase 2: Core API Coverage (Weeks 3-4)  
1. Implement all Volume Replication API scenarios
2. Add Volume Group Replication API tests
3. Create parameter validation test suite
4. Implement idempotency testing framework

#### Phase 3: Integration Testing (Weeks 5-6)
1. Controller integration with Layer-1 scenarios
2. State transition and error recovery tests
3. Network failure simulation testing
4. Real driver integration testing support

#### Phase 4: Certification and Validation (Weeks 7-8)
1. Full Layer-1 certification suite
2. Automated test report generation  
3. Performance and scalability testing
4. Documentation and certification guide

This implementation strategy provides a comprehensive roadmap for achieving full Layer-1 CSI Volume Replication certification coverage while building on existing test infrastructure and incorporating learnings from the PR test implementations.