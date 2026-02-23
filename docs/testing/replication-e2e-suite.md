# Replication E2E Test Suite

The replication E2E suite runs cluster-facing tests that create VolumeReplication and VolumeReplicationClass resources and assert on their status. It implements scenarios from the [Layer-1 CSI Replication Add-on Test Matrix](https://github.com/nadavleva/csi_replication_certs/blob/main/docs/layer-1-vr-tests.md).

## Location

- **Package**: `test/e2e/replication/`
- **Scenarios**: EnableVolumeReplication (L1-E-001, L1-E-002, L1-E-005), GetVolumeReplicationInfo (L1-INFO-001, L1-INFO-008)

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

**VRC and timeouts:** Tests create their own VolumeReplicationClasses (e.g. `vrc-snapshot-<ns>`, `vrc-journal-<ns>`) with **scheduling interval 1m** for snapshot mode so the first replication cycle can complete within the wait window. They do not use existing cluster VRCs (e.g. `vrc-5m`). If Replicating=True is not set within the timeout (e.g. journal mode or a slow cluster), increase **`REPLICATION_POLL_TIMEOUT`** (seconds). Test output includes step-by-step progress and per-poll VR status (`state`, `conditions`) when waiting.

**Replication secret:** The Ceph RBD CSI driver expects the secret referenced by the VolumeReplicationClass to contain **`userID`** and **`userKey`** in its `data`. If you do **not** set `REPLICATION_SECRET_NAME` and `REPLICATION_SECRET_NAMESPACE`, the suite creates a per-test secret with placeholder `userID`/`userKey` so the driver does not return “missing ID field 'userID' in secrets”. For a real Ceph cluster (e.g. Rook), use the existing RBD CSI secret by setting both variables. Typical Rook secrets in `rook-ceph`:

- **`rook-csi-rbd-provisioner`** – used by the RBD CSI controller (recommended for replication).
- `rook-csi-rbd-node` – used by the RBD node plugin.

Example:

```bash
REPLICATION_SECRET_NAME=rook-csi-rbd-provisioner REPLICATION_SECRET_NAMESPACE=rook-ceph make test-replication-e2e
```

## Test IDs (current)

| ID         | API / scenario                          | Description                                      |
|------------|-----------------------------------------|--------------------------------------------------|
| L1-E-001   | EnableVolumeReplication                 | Enable snapshot mode; VR gets success condition  |
| L1-E-002   | EnableVolumeReplication                 | Enable journal mode; VR gets success condition   |
| L1-E-005   | EnableVolumeReplication                 | Idempotent enable on already enabled volume      |
| L1-INFO-001| GetVolumeReplicationInfo                | Query healthy replication status                 |
| L1-INFO-008| GetVolumeReplicationInfo                | Non-existent volume returns error in VR status   |

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
