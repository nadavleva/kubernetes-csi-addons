#!/bin/bash
# Commit and push the replication E2E test suite to origin only (not upstream).
# Run from repo root. Creates one commit and pushes to origin. Then open PR from GitHub/gh.
set -e
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

BRANCH="${1:-$(git branch --show-current)}"
if [[ -z "$BRANCH" ]]; then
  echo "Could not determine branch. Create one first: git checkout -b feat/replication-e2e-suite"
  exit 1
fi

echo "=== Adding replication test suite files ==="
git add .gitignore Makefile README.md
git add docs/overview.md docs/testing/
git add hack/clean-replication-e2e-resources.sh hack/diagnose-replication-vr.sh hack/run-ceph-replication-addon.sh hack/run-replication-e2e.sh hack/commit-and-push-replication-e2e.sh
git add test/

echo "=== Status ==="
git status --short

echo "=== Creating commit ==="
git commit -m "Add replication E2E test suite

- test/e2e/replication: Ginkgo suite and Layer-1 VR tests (enable, get info, full DR)
- hack/run-replication-e2e.sh: run suite against existing cluster with optional GINKGO_FOCUS
- hack/clean-replication-e2e-resources.sh: clean e2e-replication-* namespaces, VRs, PVCs, VRCs
- hack/run-ceph-replication-addon.sh: run controller + client tests with Ceph
- hack/diagnose-replication-vr.sh: diagnose VolumeReplication state
- docs: replication-e2e-suite.md, testing gap analysis, overview
- Makefile: test-replication-e2e, clean-replication-e2e targets
- README: document replication E2E and test commands"

echo "=== Pushing to origin only (not upstream) ==="
git push origin "$BRANCH"

echo "=== Done. Create PR from branch $BRANCH to upstream main (e.g. via GitHub or: gh pr create --base main --head $BRANCH --repo csi-addons/kubernetes-csi-addons)" 
