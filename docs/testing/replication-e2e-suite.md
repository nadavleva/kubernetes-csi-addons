# Replication E2E Test Suite

The replication E2E suite runs cluster-facing tests that create VolumeReplication and VolumeReplicationClass resources and assert on their status. It implements scenarios from the Layer-1 CSI Replication Add-on Test Matrix.

**This document is the source of truth** for the replication E2E test plan. The following external documents were used as references:

- [Layer-1 VolumeReplication Test Matrix (layer-1-vr-tests.md)](https://github.com/nadavleva/csi_replication_certs/blob/main/docs/layer-1-vr-tests.md)
- [Layer-1 VRG Test Matrix (layer-1-vrg-tests.md)](https://github.com/nadavleva/csi_replication_certs/blob/main/docs/layer-1-vrg-tests.md)

## Location

- **Package**: `test/e2e/replication/`
- **Scenarios**: EnableVolumeReplication (L1-E-001 through L1-E-009), DisableVolumeReplication (L1-DIS-001, L1-DIS-002), GetVolumeReplicationInfo (L1-INFO-001, L1-INFO-005, L1-INFO-008, L1-INFO-011, L1-INFO-012, L1-INFO-013, L1-INFO-014), and Full DR (two clusters). L1-E-003 uses NetworkFence to block the node so EnableVolumeReplication fails, then unfences and asserts success. L1-DIS-002 requires DR1_CONTEXT and DR2_CONTEXT; tests that need two clusters skip with a log when not configured.

## Cleanup

**On every run:** The run script registers an **EXIT trap** that runs `clean-replication-e2e-resources.sh` when the script exits (success, failure, or panic/timeout). So PVCs, VRs, test VRCs, and e2e-replication-* namespaces are cleaned even if tests panic or hit the test timeout.

**Before a run:** If a previous run left resources stuck (e.g. Terminating PVCs or VRs), you can clean them manually first:

```bash
make clean-replication-e2e
# or
./hack/clean-replication-e2e-resources.sh
```

To preview what would be deleted without making changes:

```bash
./hack/clean-replication-e2e-resources.sh --dry-run
```

The script removes finalizers from VolumeReplications and PVCs in `e2e-replication-*` namespaces, deletes those resources plus any VolumeSnapshots, deletes the namespaces, and deletes test-created VolumeReplicationClasses (names starting with `vrc-snapshot-`, `vrc-journal-`, etc.).

**Planned enhancement:** Create a failure report (PVC, VolumeReplication, VolumeReplicationGroup, events, pod logs from test namespaces and `REPLICATION_SECRET_NAMESPACE`) **before** cleanup on test failure, and store it under `Logs/<run-folder>/`. See [.github/ISSUE_TEMPLATE/replication-e2e-failure-report.md](../../.github/ISSUE_TEMPLATE/replication-e2e-failure-report.md) for the full issue description.

## Prerequisites

- **Cluster**: KUBECONFIG pointing at a running Kubernetes cluster
- **CRDs**: VolumeReplication and VolumeReplicationClass CRDs installed (e.g. `kubectl apply -f deploy/controller/crds.yaml`)
- **Controller**: CSI-Addons controller running in the cluster
- **CSI driver**: A CSI driver with replication support (e.g. Ceph RBD with mirroring) and a StorageClass that provisions volumes from it

## Running the suite

### Run all replication E2E tests

```bash
make test-replication-e2e
```

Or directly:

```bash
./hack/run-replication-e2e.sh
```

Output is written to `Logs/replication-e2e_<timestamp>.log` and to stdout. The run script uses `stdbuf -oL` (when available) so output is line-buffered and appears as tests run instead of only at the end. Each test logs steps (e.g. "Starting L1-E-001", "Creating namespace", "Creating PVC...", "[PVC] ns/name phase=...", "[VR] ...") so you can see progress during long waits.

### Run specific tests (focus)

Use the `GINKGO_FOCUS` environment variable to run only tests whose descriptions match the given expression:

```bash
# Run a single test by Layer-1 ID
GINKGO_FOCUS="L1-E-001" ./hack/run-replication-e2e.sh

# Run all EnableVolumeReplication tests
GINKGO_FOCUS="EnableVolumeReplication" ./hack/run-replication-e2e.sh

# Run all GetVolumeReplicationInfo tests
GINKGO_FOCUS="GetVolumeReplicationInfo" ./hack/run-replication-e2e.sh
```

With make:

```bash
make test-replication-e2e GINKGO_FOCUS="L1-E-001"
```

### Optional environment variables

| Variable                       | Description                                                                 | Default        |
|--------------------------------|-----------------------------------------------------------------------------|----------------|
| `GINKGO_FOCUS`                 | Ginkgo focus expression to run only matching specs                         | (run all)      |
| `INSTALL_CRDS`                 | Set to `true` to install CRDs from `deploy/controller/crds.yaml` if missing | `false`        |
| `STORAGE_CLASS`                | StorageClass name used for test PVCs                                       | auto-detect    |
| `CSI_PROVISIONER`              | Provisioner name for VolumeReplicationClass                                | auto-detect    |
| `REPLICATION_SECRET_NAME`      | Name of existing secret for replication (use with `REPLICATION_SECRET_NAMESPACE`) | (create per-namespace secret) |
| `REPLICATION_SECRET_NAMESPACE` | Namespace of existing replication secret                                   | (create per-namespace secret) |
| `REPLICATION_POLL_TIMEOUT`     | Timeout in seconds for waiting on Replicating=True (and error conditions)  | `300` |
| `REPLICATION_TEST_TIMEOUT`     | Go test timeout for the entire suite (e.g. `30m`, `60m`). Prevents "test timed out after 10m0s". | `30m` |
| `DR1_CONTEXT`                  | Kubeconfig context name for primary cluster (DR1). Set with `DR2_CONTEXT` for full-DR mode. | (unset) |
| `DR2_CONTEXT`                  | Kubeconfig context name for secondary cluster (DR2). Set with `DR1_CONTEXT` for full-DR mode. | (unset) |
| `FENCE_CIDRS`                  | Comma-separated CIDRs for L1-E-003 (peer unreachable) NetworkFence test. If unset, CIDRs are read from CSIAddonsNode `status.networkFenceClientStatus`, or fallback to node InternalIPs. | (CSIAddonsNode or node IPs) |
| `FENCE_CLUSTER_ID`             | ClusterID for Ceph CSI NetworkFenceClass (required for Ceph/Rook). If unset, inferred from `REPLICATION_SECRET_NAMESPACE` when provisioner contains "ceph". | (inferred from secret namespace) |

**L1-E-003 NetworkFence (Ceph CSI):** The Ceph CSI driver requires `clusterID` in NetworkFenceClass parameters for network fencing. For Rook, use the cluster namespace (e.g. `rook-ceph`). The e2e suite adds this automatically when the provisioner contains "ceph":

```yaml
parameters:
  clusterID: rook-ceph
  csiaddons.openshift.io/networkfence-secret-name: rook-csi-rbd-provisioner
  csiaddons.openshift.io/networkfence-secret-namespace: rook-ceph
```

Set `FENCE_CLUSTER_ID` to override the inferred value.

**VRC and timeouts:** Tests create their own VolumeReplicationClasses (e.g. `vrc-snapshot-<ns>`, `vrc-journal-<ns>`) with **scheduling interval 1m** for snapshot mode so the first replication cycle can complete within the wait window. They do not use existing cluster VRCs (e.g. `vrc-5m`). If Replicating=True is not set within the timeout (e.g. journal mode or a slow cluster), increase **`REPLICATION_POLL_TIMEOUT`** (seconds). Test output includes step-by-step progress and per-poll VR status (`state`, `conditions`) when waiting.

**Replication secret:** The Ceph RBD CSI driver expects the secret referenced by the VolumeReplicationClass to contain **`userID`** and **`userKey`** in its `data`. If you do **not** set `REPLICATION_SECRET_NAME` and `REPLICATION_SECRET_NAMESPACE`, the suite creates a per-test secret with placeholder `userID`/`userKey` so the driver does not return “missing ID field 'userID' in secrets”. For a real Ceph cluster (e.g. Rook), use the existing RBD CSI secret by setting both variables. Typical Rook secrets in `rook-ceph`:

- **`rook-csi-rbd-provisioner`** – used by the RBD CSI controller (recommended for replication).
- `rook-csi-rbd-node` – used by the RBD node plugin.

Example:

```bash
REPLICATION_SECRET_NAME=rook-csi-rbd-provisioner REPLICATION_SECRET_NAMESPACE=rook-ceph make test-replication-e2e
```

## Test IDs (current)

The suite has **13 specs** covering **24 test cases**. Enable and GetVolumeReplicationInfo are combined: each Enable spec asserts the corresponding GetInfo outcome (success: L1-INFO-001; error: L1-INFO-005, L1-INFO-011, L1-INFO-012, L1-INFO-013, L1-INFO-014). The GetVolumeReplicationInfo validations are executed as part of each Enable spec (see log STEP lines: "Assertions: GetVolumeReplicationInfo (L1-INFO-xxx)").

| Spec                         | Test cases covered        | Description                                                                 |
|------------------------------|---------------------------|-----------------------------------------------------------------------------|
| L1-E-001 + L1-INFO-001       | 2 (Enable snapshot, GetInfo) | Enable snapshot mode; assert VR state and replication info (conditions)   |
| L1-E-002 + L1-INFO-001       | 2 (Enable journal, GetInfo)  | Enable journal mode; assert VR state and replication info (conditions)   |
| L1-E-003 + L1-INFO-005 + L1-INFO-001 | 4 (Peer unreachable) | NetworkFence blocks node → EnableVolumeReplication fails, GetVolumeReplicationInfo (L1-INFO-005) shows error; set fenceState Unfenced → EnableVolumeReplication succeeds, GetVolumeReplicationInfo (L1-INFO-001) shows healthy |
| L1-E-004 + L1-INFO-012       | 2 (Invalid interval, GetInfo error) | Invalid schedulingInterval returns error; GetVolumeReplicationInfo returns error state |
| L1-E-005 + L1-INFO-001       | 2 (Idempotent enable, GetInfo) | Idempotent enable on first VR; assert first VR state and conditions; second VR idempotent |
| L1-E-006 + L1-INFO-013       | 2 (Invalid secret, GetInfo error) | Secret missing/invalid returns error; GetVolumeReplicationInfo returns error state |
| L1-E-007 + L1-INFO-011       | 2 (Invalid mirroringMode, GetInfo error) | Invalid mirroringMode returns error; GetVolumeReplicationInfo returns error state |
| L1-E-008 + L1-INFO-001       | 2 (Future startTime, GetInfo) | Future schedulingStartTime enables replication; GetVolumeReplicationInfo returns replication info |
| L1-E-009 + L1-INFO-014       | 2 (Invalid time format, GetInfo error) | Invalid schedulingStartTime format returns error; GetVolumeReplicationInfo returns error state |
| L1-INFO-008                  | 1                         | Non-existent volume returns error in VR status                              |
| L1-DIS-001                   | 1                         | Disable active replication on primary; replication removed, volume writeable |
| L1-DIS-002                   | 1                         | Disable active replication on secondary (requires DR1_CONTEXT and DR2_CONTEXT); secondary PVC restored from primary per RBD mirror backup/restore; replication stopped, secondary remains RO |
| Full DR (two clusters)       | 1                         | Creates namespace on both DR1 and DR2, PVC and VR on DR1 only              |

**Total: 13 specs, 24 test cases**. Tests that require two clusters (L1-DIS-002, Full DR) call `SkipIfNotFullDR` and are skipped with a log message when `DR1_CONTEXT` and `DR2_CONTEXT` are not both set. **L1-E-003 (peer unreachable):** Uses NetworkFenceClass and NetworkFence to block the storage node so EnableVolumeReplication fails; then sets `spec.fenceState: Unfenced` to unfence (deletion no longer triggers UnfenceClusterNetwork). Recovery may take ~2 minutes after unfence (RBD mirror reconnect, controller retry). **Before running**, the test checks that the **NetworkFence and NetworkFenceClass CRDs** are installed; if not, it skips. CIDRs come from env `FENCE_CIDRS`, CSIAddonsNode, or node InternalIPs. For **Ceph CSI (Rook)**, the test adds `clusterID` to NetworkFenceClass. **L1-E-003 cleanup:** `DeleteNetworkFenceWithCleanup` unfences first (sets fenceState Unfenced), then deletes the NetworkFence, so CIDRs are unblocked before VR/PVC cleanup even on failure or interrupt.

## Cleanup and finalizers

The controller adds finalizers to VolumeReplication (`replication.storage.openshift.io`) and to PVCs (`replication.storage.openshift.io/pvc-protection`). On delete, the controller removes them after disabling replication. The e2e suite cleanup:

1. Deletes VR first, then VRC, then PVC, then namespace.
2. Waits up to 45 seconds for each resource to be gone after delete.
3. If a VR or PVC is still present (e.g. controller cannot reach the driver), the test removes the replication finalizer so the resource can be deleted and the namespace can terminate.

So leftover Terminating PVCs or VRs from failed runs should be cleared by the next run’s cleanup, or you can remove the finalizers manually if needed.

## VolumeReplication status.State = Unknown

The controller sets `status.state` to **Primary** only after **both** of these succeed:

1. **EnableVolumeReplication** RPC (enable replication on the volume).
2. **PromoteVolume** RPC (mark volume as primary), invoked from `markVolumeAsPrimary`.

If either call fails (e.g. sidecar unreachable, driver error, or no CSIAddonsNode for the provisioner), the controller calls `updateReplicationStatus(..., GetCurrentReplicationState(instance.Status.State), msg)`. Because `Status.State` is still empty at that point, `GetCurrentReplicationState("")` returns **UnknownState**, so the status stays **Unknown**.

**What to check when State stays Unknown:**

- **Controller and VRs in the same cluster**  
  The CSI-Addons controller only reconciles VRs in the cluster where it runs. If VRs are created in cluster A but the controller runs in cluster B, those VRs are never reconciled and State is never set.
- **VRC provisioner must match CSIAddonsNode driver name**  
  The controller looks up a CSIAddonsNode whose `spec.driver.name` **exactly** matches the VolumeReplicationClass `spec.provisioner`. If they differ (e.g. VRC uses `rook-ceph.rbd.csi.ceph.com` but CSIAddonsNode uses `rbd.csi.ceph.com`), the controller never finds a connection and State stays Unknown. The e2e tests default the VRC provisioner to `rook-ceph.rbd.csi.ceph.com`; set env **`CSI_PROVISIONER`** to match your CSIAddonsNode driver name when running the suite (and when creating VRCs manually).
- **CSIAddonsNode exists and supports VolumeReplication**  
  If there is no CSIAddonsNode for the provisioner, or it does not advertise VolumeReplication, the controller cannot call the driver.
- **Controller logs**  
  Look for errors like “failed to enable replication”, “failed to promote volume”, or “leading CSIAddonsNode … for driver … does not support VolumeReplication” in the controller pod logs.

**Diagnostic script:** To quickly see provisioner vs driver names and controller errors, run:

```bash
./hack/diagnose-replication-vr.sh
# Or for a specific VR: ./hack/diagnose-replication-vr.sh <namespace> <vr-name>
```

Example: `./hack/diagnose-replication-vr.sh e2e-replication-b8c5f92a vr-snapshot`

The tests accept either `Primary` or `Unknown` when the Replicating condition is True.

**Error conditions:** The controller signals failure by setting **ConditionDegraded** with **Status=True** (and Reason=Error, etc.), and **ConditionCompleted** with Status=False and a failure Reason (FailedToPromote, FailedToDemote, FailedToResync). It does not use ConditionFalse alone to mean "error". The e2e helpers (`hasVolumeReplicationErrorCondition`, `WaitForVolumeReplicationError`) are written to match this so that: (1) tests that expect an error (e.g. L1-INFO-008) detect it, and (2) L1-E-005’s "assert no error" on the idempotent second VR does not false-positive when the controller leaves the duplicate VR’s status untouched.

## Single cluster vs full DR (two clusters)

**Single cluster (default):** Omit `DR1_CONTEXT` and `DR2_CONTEXT`; the suite uses the current kubeconfig context. Use `kubectl config use-context <name>` then `make test-replication-e2e` to target a cluster.

The e2e suite creates all resources (namespaces, PVCs, VolumeReplications, VolumeReplicationClasses) in **the cluster that your kubeconfig is currently using**. It does not have a built-in notion of “DR1” vs “DR2”; it simply uses the default context (or the one set by `KUBECONFIG`).

**Full DR mode (two clusters):** Set both `DR1_CONTEXT` and `DR2_CONTEXT` to context names in your kubeconfig. The suite builds two clients, uses DR1 as primary, and runs "Full DR (two clusters)" tests. Example:

```bash
DR1_CONTEXT=dr1 DR2_CONTEXT=dr2 REPLICATION_SECRET_NAME=rook-csi-rbd-provisioner REPLICATION_SECRET_NAMESPACE=rook-ceph make test-replication-e2e
```

Use `GetK8sClientForCluster(ClusterDR1)` and `GetK8sClientForCluster(ClusterDR2)` to target either cluster in tests.

**Important:** The CSI-Addons controller must run in the cluster where you create the VRs (e.g. DR1 for primary VRs).

## Note

These tests require `USE_EXISTING_CLUSTER=true`. Do not run them with `make test` (which uses envtest and no real cluster). Use `make test-replication-e2e` or `./hack/run-replication-e2e.sh` instead.

---

## Full Test Plan

The full test plan below enumerates all endpoint, state, and workflow-driven scenarios for Layer-1 CSI Replication driver conformance. It is intended for use by certification tools, automation, and test writers.

**Columns:**
- Test ID
- API (gRPC/CRD)
- Scenario/Description
- Node Role / Cluster State / Peer State / S3 State
- Parameters (e.g., force)
- Test Type (functional, negative, behavioral, API, performance)
- Input/Setup Steps
- Expected Result/Pass Criteria
- Notes/Automation Link/Reference

### EnableVolumeReplication

| Test ID   | API                    | Scenario                              | Role       | Peer State | Params          | Test Type   | Setup/Input                                    | Expected Outcome                                            | Notes/Link             |
|-----------|------------------------|---------------------------------------|------------|------------|-----------------|-------------|-----------------------------------------------|-------------------------------------------------------------|------------------------|
| L1-E-001  | EnableVolumeReplication| Enable snapshot mode                  | Primary    | Up         | mode=snapshot   | functional  | Volume present, PVC bound, rep. disabled      | VR CR created, status.replicationHandle populated           |                        |
| L1-E-002  | EnableVolumeReplication| Enable journal mode                   | Primary    | Up         | mode=journal    | functional  | Volume present, PVC bound, rep. disabled      | VR CR created, continuous replication active                 |                        |
| L1-E-003  | EnableVolumeReplication| Peer cluster unreachable              | Primary    | Down       | mode=snapshot   | negative    | Peer/all unreachable network                   | Operation fails: timeout, appropriate error in status        | *Not Supported - unreachable storage not supported in current K8s CSI tests will be implemented in later stage |
| L1-E-004  | EnableVolumeReplication| Invalid interval parameter            | Primary    | Up         | interval=5x     | negative    | Bad parameter                                 | Returns gRPC InvalidArgument error                           |                        |
| L1-E-005  | EnableVolumeReplication| Already enabled volume                | Primary    | Up         | (none)          | functional  | VR CR exists, rep enabled already             | Idempotent, operation succeeds with no change                |                        |
| L1-E-006  | EnableVolumeReplication| Secret reference missing/invalid      | Primary    | Up         | secret=missing  | negative    | Bad rep. secret ref                            | gRPC FailedPrecondition error                                |                        |
| L1-E-007  | EnableVolumeReplication| Invalid mirroringMode parameter       | Primary    | Up         | mode=invalid    | negative    | Bad mirroringMode parameter                    | Returns gRPC InvalidArgument error                           |                        |
| L1-E-008  | EnableVolumeReplication| Future schedulingStartTime            | Primary    | Up         | mode=snapshot, startTime=+30s | functional | Valid future start time                        | VR CR created, scheduling starts at specified time          |                        |
| L1-E-009  | EnableVolumeReplication| Invalid schedulingStartTime format    | Primary    | Up         | mode=snapshot, startTime=invalid | negative | Bad time format parameter                      | Returns gRPC InvalidArgument error                           |                        |

**EnableVolumeReplication Test Count: 9 scenarios**

### DisableVolumeReplication (all key permutations)

| Test ID   | API                        | Scenario                             | Node Role  | Peer State | Array State | Params       | Test Type   | Setup/Input                                 | Expected Outcome                                     | Notes/Link             |
|-----------|----------------------------|--------------------------------------|------------|------------|-------------|--------------|-------------|----------------------------------------------|------------------------------------------------------|------------------------|
| L1-DIS-001| DisableVolumeReplication   | Disable, active, peer up             | Primary    | Up         | Up          | force=false  | functional  | Rep enabled, all healthy                    | Replication removed, volume writeable                |                        |
| L1-DIS-002| DisableVolumeReplication   | Disable, active, peer up             | Secondary  | Up         | Up          | force=false  | functional  | Rep enabled, all healthy                    | Replication stopped; secondary remains RO            |                        |
| L1-DIS-003| DisableVolumeReplication   | Previously disabled, peer up         | Primary    | Up         | Up          | force=false  | functional  | No replication relationship                 | Idempotent, no error                                |                        |
| L1-DIS-004| DisableVolumeReplication   | Previously disabled, peer up         | Secondary  | Up         | Up          | force=false  | functional  | No replication relationship                 | Idempotent, no error                                |                        |
| L1-DIS-005| DisableVolumeReplication   | Peer down, force=false               | Primary    | Down       | Up          | force=false  | negative    | Peer unreachable (simulate network failure) | Fails gracefully, logs/unavailable                   | *Not Supported - unreachable storage not supported in current K8s CSI tests will be implemented in later stage |
| L1-DIS-006| DisableVolumeReplication   | Peer down, force=true                | Primary    | Down       | Up          | force=true   | behavioral  | Peer unreachable, force=true                | Immediate disable, makes primary writeable (warn)    |                        |
| L1-DIS-007| DisableVolumeReplication   | Array unreachable, force=false       | Primary    | Up         | Down        | force=false  | negative    | Disconnect primary array                     | Fails, error code: array unreachable                 | *Not Supported - unreachable storage not supported in current K8s CSI tests will be implemented in later stage |
| L1-DIS-008| DisableVolumeReplication   | Array unreachable, force=true        | Secondary  | Up         | Down        | force=true   | negative    | Disconnect secondary array                   | Fails, error code: array unreachable                 | *Not Supported - unreachable storage not supported in current K8s CSI tests will be implemented in later stage |

#### DisableVolumeReplication with force=true (Complete Test Matrix)

| Test ID   | API                        | Scenario                                 | Node Role  | Peer State | Array State | Params       | Test Type   | Setup/Input                                 | Expected Outcome                                     | Notes/Link             |
|-----------|----------------------------|------------------------------------------|------------|------------|-------------|--------------|-------------|----------------------------------------------|------------------------------------------------------|------------------------|
| L1-DIS-009| DisableVolumeReplication   | Force disable, active, peer up           | Primary    | Up         | Up          | force=true   | behavioral  | Rep enabled, all healthy, force=true        | Immediate disable, volume writeable, warn logged     |                        |
| L1-DIS-010| DisableVolumeReplication   | Force disable, active, peer up           | Secondary  | Up         | Up          | force=true   | behavioral  | Rep enabled, all healthy, force=true        | Immediate disable, secondary disconnected, warn logged|                        |
| L1-DIS-011| DisableVolumeReplication   | Force disable, previously disabled       | Primary    | Up         | Up          | force=true   | functional  | No replication relationship, force=true     | Idempotent, no error                                |                        |
| L1-DIS-012| DisableVolumeReplication   | Force disable, previously disabled       | Secondary  | Up         | Up          | force=true   | functional  | No replication relationship, force=true     | Idempotent, no error                                |                        |
| L1-DIS-013| DisableVolumeReplication   | Force disable, peer down                  | Primary    | Down       | Up          | force=true   | behavioral  | Peer unreachable, force=true                | Immediate disable, split-brain warning logged       | *Not Supported - unreachable storage not supported in current K8s CSI tests will be implemented in later stage |
| L1-DIS-014| DisableVolumeReplication   | Force disable, peer down                  | Secondary  | Down       | Up          | force=true   | behavioral  | Peer unreachable, force=true                | Emergency disable, cleanup attempted                 | *Not Supported - unreachable storage not supported in current K8s CSI tests will be implemented in later stage |
| L1-DIS-015| DisableVolumeReplication   | Force disable, primary array down        | Primary    | Up         | Down        | force=true   | negative    | Primary array unreachable, force=true       | Still fails, cannot force without array access      | *Not Supported - unreachable storage not supported in current K8s CSI tests will be implemented in later stage |
| L1-DIS-016| DisableVolumeReplication   | Force disable, secondary array down      | Secondary  | Up         | Down        | force=true   | behavioral  | Secondary array unreachable, force=true     | Forced cleanup, metadata inconsistency warnings     | *Not Supported - unreachable storage not supported in current K8s CSI tests will be implemented in later stage |

**DisableVolumeReplication Test Count: 16 scenarios (8 force=false + 8 force=true)**

### PromoteVolume (Complete Test Matrix)

| Test ID   | API                    | Scenario                                  | Node Role  | Peer State | Array State | Params      | Test Type  | Setup/Input | Expected Outcome                              | Notes/Link |
|-----------|------------------------|-------------------------------------------|------------|------------|-------------|-------------|-----------|-------------|-----------------------------------------------|------------|
| L1-PROM-001| PromoteVolume         | Promote secondary → primary, healthy      | Secondary  | Up         | Up          | force=false | functional| All VRs in sync, healthy                      | VR status.state=Primary, volume RW                  |            |
| L1-PROM-002| PromoteVolume         | Promote already primary, healthy          | Primary    | Up         | Up          | force=false | functional| Volume already primary                        | Idempotent operation, no change                     |            |
| L1-PROM-003| PromoteVolume         | Promote secondary, peer down, force=false | Secondary  | Down       | Up          | force=false | negative  | Primary cluster unreachable, attempt promote   | Fails, split-brain prevention active               | *Not Supported - unreachable storage not supported in current K8s CSI tests will be implemented in later stage |
| L1-PROM-004| PromoteVolume         | Promote secondary, peer down, force=true  | Secondary  | Down       | Up          | force=true  | behavioral| Peer down, force emergency failover           | Promoted, warning about possible data loss          | *Not Supported - unreachable storage not supported in current K8s CSI tests will be implemented in later stage |
| L1-PROM-005| PromoteVolume         | Promote, array unreachable, force=false   | Secondary  | Up         | Down        | force=false | negative  | Secondary array disconnected                   | Fails, cannot access volume for promotion          | *Not Supported - unreachable storage not supported in current K8s CSI tests will be implemented in later stage |
| L1-PROM-006| PromoteVolume         | Promote, array unreachable, force=true    | Secondary  | Up         | Down        | force=true  | negative  | Secondary array disconnected, force attempted  | Still fails, cannot promote without array access   | *Not Supported - unreachable storage not supported in current K8s CSI tests will be implemented in later stage |
| L1-PROM-007| PromoteVolume         | Promote with active I/O workload          | Secondary  | Up         | Up          | force=false | behavioral| Active workload on primary                     | Graceful promotion, I/O redirected                 |            |
| L1-PROM-008| PromoteVolume         | Force promote with active I/O workload    | Secondary  | Up         | Up          | force=true  | behavioral| Active workload, force promotion              | Immediate promotion, potential I/O disruption warning|           |

**PromoteVolume Test Count: 8 scenarios**

### DemoteVolume (Complete Test Matrix)

| Test ID   | API                      | Scenario                                    | Node Role   | Peer State | Array State | Params      | Test Type  | Setup/Input  | Expected Outcome                             | Notes/Link |
|-----------|--------------------------|---------------------------------------------|-------------|------------|-------------|-------------|-----------|--------------|----------------------------------------------|------------|
| L1-DEM-001| DemoteVolume             | Demote primary to secondary, healthy        | Primary     | Up         | Up          | force=false | functional| Primary with healthy replication             | VR status.state=Secondary, volume RO         |            |
| L1-DEM-002| DemoteVolume             | Demote already secondary, healthy           | Secondary   | Up         | Up          | force=false | functional| Volume already secondary                     | Idempotent operation, no change              |            |
| L1-DEM-003| DemoteVolume             | Demote primary, peer down, force=false      | Primary     | Down       | Up          | force=false | negative  | Peer unreachable                             | Fails, cannot establish secondary relationship| *Not Supported - unreachable storage not supported in current K8s CSI tests will be implemented in later stage |
| L1-DEM-004| DemoteVolume             | Demote primary, peer down, force=true       | Primary     | Down       | Up          | force=true  | behavioral| Peer down, force demotion                    | Demoted locally, warning about peer state   | *Not Supported - unreachable storage not supported in current K8s CSI tests will be implemented in later stage |
| L1-DEM-005| DemoteVolume             | Demote, array unreachable, force=false      | Primary     | Up         | Down        | force=false | negative  | Primary array disconnected                   | Fails, cannot access volume for demotion    | *Not Supported - unreachable storage not supported in current K8s CSI tests will be implemented in later stage |
| L1-DEM-006| DemoteVolume             | Demote, array unreachable, force=true       | Primary     | Up         | Down        | force=true  | negative  | Primary array disconnected, force attempted  | Still fails, cannot demote without array access| *Not Supported - unreachable storage not supported in current K8s CSI tests will be implemented in later stage |
| L1-DEM-007| DemoteVolume             | Demote with active I/O workload, force=false| Primary     | Up         | Up          | force=false | behavioral| Active workload, graceful demotion           | Pending I/O completed, then demoted to RO   |            |
| L1-DEM-008| DemoteVolume             | Force demote with active I/O workload       | Primary     | Up         | Up          | force=true  | behavioral| Active workload, force=true                  | Immediate demotion, pending I/O may be dropped, warning issued |  |

**DemoteVolume Test Count: 8 scenarios**

### ResyncVolume

| Test ID   | API                    | Scenario                                      | Node Role | Peer State | Params      | Test Type  | Setup/Input  | Expected Outcome                                  | Notes/Link |
|-----------|------------------------|-----------------------------------------------|-----------|------------|-------------|-----------|--------------|---------------------------------------------------|------------|
| L1-RSYNC-001| ResyncVolume         | Resync secondary after split-brain            | Secondary | Up         | -           | functional| Split-brain resolved                            | Full resync completes, data consistent             |            |
| ...       | ...                    | ...                                           | ...       | ...        | ...         | ...       | ...          | ...                                              | ...        |

**ResyncVolume Test Count: 2+ scenarios (expandable)**

### GetVolumeReplicationInfo

| Test ID   | API                        | Scenario                                    | Node Role | Peer State | Array State | Params      | Test Type  | Setup/Input  | Expected Outcome                                  | Notes/Link |
|-----------|----------------------------|---------------------------------------------|-----------|------------|-------------|-------------|-----------|--------------|---------------------------------------------------|------------|
| L1-INFO-001| GetVolumeReplicationInfo  | Query for healthy replication                | Primary   | Up         | Up          | -           | functional| Volume in sync, replication active               | Returns lastSyncTime, status=healthy, replicationHandle | See L1-PROM-002 for related promote scenario |
| L1-INFO-002| GetVolumeReplicationInfo  | Query for healthy replication on secondary   | Secondary | Up         | Up          | -           | functional| Volume in sync, receiving replication            | Returns lastSyncTime, status=healthy, role=secondary    | See L1-DEM-002 for related demote scenario |
| L1-INFO-003| GetVolumeReplicationInfo  | Query during sync operation                  | Primary   | Up         | Up          | -           | functional| Sync in progress                                 | Returns status=syncing, progress percentage             |            |
| L1-INFO-004| GetVolumeReplicationInfo  | Query for degraded replication               | Primary   | Up         | Up          | -           | functional| Network issues, replication lagging              | Returns status=degraded, lastSyncTime old, error details| Related to L1-DIS-005, L1-PROM-003 peer down scenarios |
| L1-INFO-005| GetVolumeReplicationInfo  | Query with peer unreachable                 | Primary   | Down       | Up          | -           | behavioral| Peer cluster unreachable                         | Returns status=disconnected, connection error details   | *Not Supported - unreachable storage not supported in current K8s CSI tests will be implemented in later stage |
| L1-INFO-006| GetVolumeReplicationInfo  | Query for disabled replication               | Primary   | Up         | Up          | -           | functional| Replication disabled                             | Returns status=disabled, no replicationHandle           | See L1-DIS-001, L1-DIS-002 for disable scenarios |
| L1-INFO-007| GetVolumeReplicationInfo  | Query with array unreachable                | Primary   | Up         | Down        | -           | behavioral| Storage array disconnected                        | Returns status=error, array connectivity error details  | *Not Supported - unreachable storage not supported in current K8s CSI tests will be implemented in later stage |
| L1-INFO-008| GetVolumeReplicationInfo  | Query for non-existent volume               | N/A       | Up         | Up          | invalid-vol | negative  | Volume ID that doesn't exist                     | Returns gRPC NotFound error                              |            |
| L1-INFO-009| GetVolumeReplicationInfo  | Query during split-brain condition          | Primary   | Partial    | Up          | -           | behavioral| Split-brain scenario detected                     | Returns status=split-brain, conflict details            | Related to L1-PROM-004, L1-DIS-013 force scenarios |
| L1-INFO-010| GetVolumeReplicationInfo  | Query for never-enabled replication         | Primary   | Up         | Up          | -           | functional| Volume never had replication enabled             | Returns status=not-configured, no replication metadata  |            |
| L1-INFO-011| GetVolumeReplicationInfo  | Query after invalid mirroringMode error     | Primary   | Up         | Up          | -           | functional| Enable failed due to invalid mirroringMode      | Returns status=not-configured, no replication metadata  | See L1-E-007 for related enable error |
| L1-INFO-012| GetVolumeReplicationInfo  | Query after invalid interval parameter error| Primary   | Up         | Up          | -           | functional| Enable failed due to bad interval parameter     | Returns status=not-configured, no replication metadata  | See L1-E-004 for related enable error |
| L1-INFO-013| GetVolumeReplicationInfo  | Query after secret reference error          | Primary   | Up         | Up          | -           | functional| Enable failed due to missing/invalid secret     | Returns status=error, FailedPrecondition details       | See L1-E-006 for related enable error |
| L1-INFO-014| GetVolumeReplicationInfo  | Query after invalid time format error       | Primary   | Up         | Up          | -           | functional| Enable failed due to bad schedulingStartTime    | Returns status=not-configured, no replication metadata  | See L1-E-009 for related enable error |

**GetVolumeReplicationInfo Test Count: 14 scenarios**

**Total VolumeReplication API Test Count: 57+ scenarios**
- EnableVolumeReplication: 9 scenarios
- DisableVolumeReplication: 16 scenarios
- PromoteVolume: 8 scenarios
- DemoteVolume: 8 scenarios
- ResyncVolume: 2+ scenarios
- GetVolumeReplicationInfo: 14 scenarios

*Note: Tests marked with "Not Supported" involve unreachable storage/cluster scenarios that are not supported in the current Kubernetes CSI test framework and will be implemented in later stage. See [disruptive tests documentation](https://github.com/nadavleva/kubernetes_csiaddontests/blob/docs/storage-test-framework/test/e2e/storage/README.md#disruptive-tests) for details.*

### Volume Group Operations (using VolumeReplication gRPC APIs with replicationsource field)

*Volume group replication uses the same VolumeReplication gRPC APIs (EnableVolumeReplication, DisableVolumeReplication, PromoteVolume, DemoteVolume, etc.) with the **replicationsource** field to specify group membership.*

**Important**: VolumeReplicationGroup (VRG) Kubernetes CRD tests are **not in scope for Phase 1**. The following focuses on Volume Group operations using CSI gRPC APIs only.

| Test ID   | RPC API                    | Scenario                                    | Group State | Params          | Test Type   | Setup/Input                                    | Expected Outcome                                          | Notes/Link             |
|-----------|----------------------------|---------------------------------------------|-------------|-----------------|-------------|------------------------------------------------|-----------------------------------------------------------|------------------------|
| L1-GRP-001| EnableVolumeReplication    | Enable replication for volume group         | All disabled| replicationsource=group1 | functional  | 3 volumes in group, all healthy                | All volumes in group enabled for replication             |                        |
| L1-GRP-002| DisableVolumeReplication   | Disable replication for volume group        | All enabled | replicationsource=group1 | functional  | 3 volumes in group, all replicating            | All volumes in group disabled, group consistent          |                        |
| L1-GRP-003| PromoteVolume              | Promote volume group to primary             | Secondary   | replicationsource=group1 | functional  | Volume group in secondary state                | All volumes in group promoted to primary                 |                        |
| L1-GRP-004| DemoteVolume               | Demote volume group to secondary            | Primary     | replicationsource=group1 | functional  | Volume group in primary state                  | All volumes in group demoted to secondary                |                        |
| L1-GRP-005| EnableVolumeReplication    | Enable group with mixed volume states       | Mixed       | replicationsource=group1 | negative    | Some volumes enabled, some disabled            | Operation fails, group state inconsistent                |                        |
| L1-GRP-006| DisableVolumeReplication   | Force disable group, peer unreachable       | Enabled     | replicationsource=group1, force=true | behavioral | Group enabled, peer cluster down              | Group disabled with warnings, split-brain risk           |                        |

**Volume Group Operations Test Count: 6 scenarios (Phase 1 - using VolumeReplication gRPC APIs)**

### VolumeReplicationGroup (VRG) CRD Operations - Out of Scope for Phase 1

*The following VRG CRD-based operations are not included in Phase 1 testing scope:*

#### VRG Disable Operations - Core Scenarios

**Active Replication (Both Sides Alive) - force=false**

| Test ID        | API Operation | Scenario                                          | Primary State | Secondary State | Peer Conn | Array State | Params      | Test Type  | Setup/Input                                     | Expected Outcome                                           | Notes/Link |
|----------------|---------------|---------------------------------------------------|---------------|-----------------|-----------|-------------|-------------|------------|-------------------------------------------------|------------------------------------------------------------|------------|
| L1-VRG-DIS-001 | VRG Disable   | Disable on primary, active replication           | Active        | Active          | Up        | P:Up/S:Up   | force=false | functional | Active VRG with healthy replication             | VRG disabled, replication stopped, primary writable       |            |
| L1-VRG-DIS-002 | VRG Disable   | Disable on secondary, active replication         | Active        | Active          | Up        | P:Up/S:Up   | force=false | functional | Active VRG with healthy replication             | VRG disabled, replication stopped, secondary remains RO   |            |

**Previously Disabled Replication (Both Sides Alive) - force=false**

| Test ID        | API Operation | Scenario                                          | Primary State | Secondary State | Peer Conn | Array State | Params      | Test Type  | Setup/Input                                     | Expected Outcome                                           | Notes/Link |
|----------------|---------------|---------------------------------------------------|---------------|-----------------|-----------|-------------|-------------|------------|-------------------------------------------------|------------------------------------------------------------|------------|
| L1-VRG-DIS-003 | VRG Disable   | Disable on primary, previously disabled          | Disabled      | Disabled        | Up        | P:Up/S:Up   | force=false | functional | VRG exists but replication already disabled     | Idempotent operation, no error                             |            |
| L1-VRG-DIS-004 | VRG Disable   | Disable on secondary, previously disabled        | Disabled      | Disabled        | Up        | P:Up/S:Up   | force=false | functional | VRG exists but replication already disabled     | Idempotent operation, no error                             |            |

**Broken Replication (Peer Dead) - force=false**

| Test ID        | API Operation | Scenario                                          | Primary State | Secondary State | Peer Conn | Array State | Params      | Test Type  | Setup/Input                                     | Expected Outcome                                           | Notes/Link |
|----------------|---------------|---------------------------------------------------|---------------|-----------------|-----------|-------------|-------------|------------|-------------------------------------------------|------------------------------------------------------------|------------|
| L1-VRG-DIS-005 | VRG Disable   | Disable on primary, peer dead                    | Active        | Unknown         | Down      | P:Up/S:?    | force=false | negative   | Network partition, secondary cluster unreachable| Operation fails, appropriate timeout/error in status      |            |
| L1-VRG-DIS-006 | VRG Disable   | Disable on secondary, peer dead                  | Unknown       | Active          | Down      | P:?/S:Up    | force=false | negative   | Network partition, primary cluster unreachable | Operation fails, split-brain protection active            |            |

**Array Unreachable - force=false**

| Test ID        | API Operation | Scenario                                          | Primary State | Secondary State | Peer Conn | Array State | Params      | Test Type  | Setup/Input                                     | Expected Outcome                                           | Notes/Link |
|----------------|---------------|---------------------------------------------------|---------------|-----------------|-----------|-------------|-------------|------------|-------------------------------------------------|------------------------------------------------------------|------------|
| L1-VRG-DIS-007 | VRG Disable   | Disable on primary, primary array unreachable    | Unknown       | Active          | Up        | P:Down/S:Up | force=false | negative   | Primary storage array disconnected              | Operation fails, cannot access primary volume metadata    |            |
| L1-VRG-DIS-008 | VRG Disable   | Disable on secondary, secondary array unreachable| Active        | Unknown         | Up        | P:Up/S:Down | force=false | negative   | Secondary storage array disconnected            | Operation fails, cannot clean up secondary resources      |            |

**VRG Disable Operations - With force=true**

| Test ID        | API Operation | Scenario                                          | Primary State | Secondary State | Peer Conn | Array State | Params     | Test Type  | Setup/Input                                     | Expected Outcome                                           | Notes/Link |
|----------------|---------------|---------------------------------------------------|---------------|-----------------|-----------|-------------|------------|------------|-------------------------------------------------|------------------------------------------------------------|------------|
| L1-VRG-DIS-009 | VRG Disable   | Force disable on primary, active replication     | Active        | Active          | Up        | P:Up/S:Up   | force=true | behavioral | Active VRG, force immediate disable             | Immediate disable, potential data loss warning logged     |            |
| L1-VRG-DIS-010 | VRG Disable   | Force disable on secondary, active replication   | Active        | Active          | Up        | P:Up/S:Up   | force=true | behavioral | Active VRG, force immediate disable             | Immediate disable, secondary disconnected                  |            |
| L1-VRG-DIS-011 | VRG Disable   | Force disable on primary, previously disabled    | Disabled      | Disabled        | Up        | P:Up/S:Up   | force=true | functional | VRG exists but replication already disabled     | Idempotent operation, no error                             |            |
| L1-VRG-DIS-012 | VRG Disable   | Force disable on secondary, previously disabled | Disabled      | Disabled        | Up        | P:Up/S:Up   | force=true | functional | VRG exists but replication already disabled     | Idempotent operation, no error                             |            |
| L1-VRG-DIS-013 | VRG Disable   | Force disable on primary, peer dead              | Active        | Unknown         | Down      | P:Up/S:?    | force=true | behavioral | Network partition, force override               | Primary disabled immediately, split-brain warning           |            |
| L1-VRG-DIS-014 | VRG Disable   | Force disable on secondary, peer dead            | Unknown       | Active          | Down      | P:?/S:Up    | force=true | behavioral | Network partition, force override               | Secondary disabled, emergency cleanup                     |            |
| L1-VRG-DIS-015 | VRG Disable   | Force disable on primary, primary array down     | Unknown       | Active          | Up        | P:Down/S:Up | force=true | negative   | Primary array disconnected, force attempted     | Still fails, cannot force without array access             |            |
| L1-VRG-DIS-016 | VRG Disable   | Force disable on secondary, secondary array down | Active        | Unknown         | Up        | P:Up/S:Down | force=true | behavioral | Secondary array disconnected, force cleanup     | Forced cleanup, metadata inconsistency warnings           |            |

**VRG Disable Operations Test Count: 16 scenarios (8 force=false + 8 force=true) - Out of Scope for Phase 1**

#### VRG Creation and Lifecycle Operations

| Test ID        | API Operation | Scenario                                          | Cluster State | PVC State   | S3 State | Params         | Test Type  | Setup/Input                                     | Expected Outcome                                           | Notes/Link |
|----------------|---------------|---------------------------------------------------|---------------|-------------|----------|----------------|------------|-------------------------------------------------|------------------------------------------------------------|------------|
| L1-VRG-CRE-001 | VRG Create    | Create VRG for single PVC, healthy clusters      | Both Up       | Bound       | Up       | -              | functional | Valid PVC, both clusters healthy                | VRG created, VR resources provisioned                     |            |
| L1-VRG-CRE-002 | VRG Create    | Create VRG for multiple PVCs, healthy clusters   | Both Up       | Multiple    | Up       | -              | functional | Multiple matching PVCs                          | VRG created, multiple VR resources                         |            |
| L1-VRG-CRE-003 | VRG Create    | Create VRG, secondary cluster unreachable        | P:Up/S:Down   | Bound       | Up       | -              | negative   | Secondary cluster network failure               | VRG creation delayed/degraded state                        |            |
| L1-VRG-CRE-004 | VRG Create    | Create VRG, S3 unreachable                       | Both Up       | Bound       | Down     | -              | negative   | S3 metadata store unavailable                   | VRG creation fails, cannot store metadata                 |            |
| L1-VRG-CRE-005 | VRG Create    | Create VRG, invalid PVC selector                 | Both Up       | None        | Up       | bad-selector   | negative   | PVC selector matches no resources               | VRG created but no VR resources, appropriate status       |            |

**VRG Creation and Lifecycle Operations Test Count: 5 scenarios - Out of Scope for Phase 1**

#### VRG Failover and Failback Operations

| Test ID        | API Operation | Scenario                                          | Primary State | Secondary State | Peer Conn | S3 State | Params            | Test Type  | Setup/Input                                     | Expected Outcome                                           | Notes/Link |
|----------------|---------------|---------------------------------------------------|---------------|-----------------|-----------|----------|-------------------|------------|-------------------------------------------------|------------------------------------------------------------|------------|
| L1-VRG-FAIL-001| VRG Failover  | Emergency failover, primary cluster down         | Down          | Active          | Down      | Up       | action=Failover   | functional | Primary cluster failure simulation              | Secondary promoted to primary, data accessible            |            |
| L1-VRG-FAIL-002| VRG Failover  | Planned failover, both clusters healthy          | Active        | Active          | Up        | Up       | action=Failover   | functional | Graceful planned failover                       | Clean role switch, minimal downtime                       |            |
| L1-VRG-FAIL-003| VRG Failback  | Failback after primary recovery                  | Recovered     | Primary         | Up        | Up       | action=Failback   | functional | Primary cluster restored after failure          | Original primary restored, data consistent                 |            |
| L1-VRG-FAIL-004| VRG Failover  | Split-brain scenario, force failover             | Isolated      | Isolated        | Partial   | Up       | action=Failover   | behavioral| Network split causing isolation                 | Emergency promotion with split-brain warnings             |            |

#### VRG Status and Monitoring Operations

| Test ID        | API Operation | Scenario                                          | Replication State | Sync Status | Error State | Params | Test Type  | Setup/Input                                     | Expected Outcome                                           | Notes/Link |
|----------------|---------------|---------------------------------------------------|-------------------|-------------|-------------|--------|------------|-------------------------------------------------|------------------------------------------------------------|------------|
| L1-VRG-STAT-001| VRG Status    | Query status, healthy replication                | Active            | InSync      | None        | -      | functional | Normal replication operation                    | Status shows healthy, lastSyncTime current                |            |
| L1-VRG-STAT-002| VRG Status    | Query status, sync in progress                   | Active            | Syncing     | None        | -      | functional | Replication sync operation ongoing              | Status shows sync progress, estimated completion          |            |
| L1-VRG-STAT-003| VRG Status    | Query status, error condition                    | Degraded          | OutOfSync   | NetworkError| -      | functional | Network issues affecting replication            | Status shows error details, troubleshooting info          |            |

#### VRG Deletion and Cleanup Operations

| Test ID        | API Operation | Scenario                                          | VRG State     | VR State    | S3 State | Finalizers | Test Type  | Setup/Input                                     | Expected Outcome                                           | Notes/Link |
|----------------|---------------|---------------------------------------------------|---------------|-------------|----------|------------|------------|-------------------------------------------------|------------------------------------------------------------|------------|
| L1-VRG-DEL-001 | VRG Delete    | Delete VRG, all resources healthy                | Active        | Multiple    | Up       | Present    | functional | VRG with active VR resources                    | VRG deleted, all VRs cleaned up, finalizers cleared       |            |
| L1-VRG-DEL-002 | VRG Delete    | Delete VRG, S3 unavailable                       | Active        | Multiple    | Down     | Present    | negative   | S3 metadata store unreachable                   | Deletion blocked, finalizer remains, cleanup pending      |            |
| L1-VRG-DEL-003 | VRG Delete    | Delete VRG, peer cluster unreachable             | Active        | Multiple    | Up       | Present    | behavioral | Secondary cluster network failure               | Local cleanup, remote cleanup marked for retry            |            |
| L1-VRG-DEL-004 | VRG Delete    | Force delete VRG, resources stuck                | Terminating   | Stuck       | Up       | Stuck      | behavioral | VR resources cannot be cleaned normally         | Force deletion, resource leakage warnings                 |            |

#### VRG Cross-Namespace and Multi-Cluster Scenarios

| Test ID        | API Operation | Scenario                                          | Namespace     | PVC Location | Cluster Config | Test Type  | Setup/Input                                     | Expected Outcome                                           | Notes/Link |
|----------------|---------------|---------------------------------------------------|---------------|--------------|----------------|------------|-------------------------------------------------|------------------------------------------------------------|------------|
| L1-VRG-NS-001  | VRG Create    | VRG in different namespace than PVCs              | Different     | ns1/ns2      | Standard       | functional | VRG in ns-a, PVCs in ns-b                       | Cross-namespace selection works correctly                  |            |
| L1-VRG-NS-002  | VRG Create    | VRG with PVCs across multiple namespaces         | Multiple      | Multiple     | Standard       | functional | PVC selector spans namespaces                   | All matching PVCs selected regardless of namespace        |            |
| L1-VRG-MC-001  | VRG Create    | VRG on cluster with different storage classes    | Standard      | Mixed SC     | Heterogeneous  | behavioral | Primary/secondary use different storage         | VRG handles storage class differences gracefully          |            |

**Total VRG Test Count Summary:**

**Phase 1 - In Scope (Volume Group Operations using gRPC APIs):**
- Volume Group Operations: 6 scenarios

**Out of Scope for Phase 1 (VRG Kubernetes CRD Operations):**
- VRG Disable Operations: 16 scenarios
- VRG Creation/Lifecycle: 5 scenarios
- VRG Failover/Failback: 4+ scenarios
- VRG Status/Monitoring: 3+ scenarios
- VRG Deletion/Cleanup: 4+ scenarios
- VRG Cross-Namespace: 3+ scenarios
- **Total CRD Operations**: 35+ scenarios (future phases)
