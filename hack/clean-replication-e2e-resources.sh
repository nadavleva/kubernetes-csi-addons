#!/bin/bash
# Clean up leftover resources from replication E2E test runs.
#
# Deletes in order: VolumeReplications (after removing finalizers), PVCs (after
# removing finalizers), VolumeSnapshots (if CRD exists), e2e-replication-* namespaces,
# and test-created VolumeReplicationClasses. Run this before a fresh E2E run if
# previous runs left resources stuck (e.g. Terminating).
#
# Usage: ./hack/clean-replication-e2e-resources.sh [--dry-run]
#
# Environment variables:
#   KUBECONFIG - Same as kubectl (default: $HOME/.kube/config)
#
# Examples:
#   ./hack/clean-replication-e2e-resources.sh
#   ./hack/clean-replication-e2e-resources.sh --dry-run

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
DRY_RUN=false

for arg in "$@"; do
	case "$arg" in
		--dry-run) DRY_RUN=true ;;
		*) echo "Unknown option: $arg"; echo "Usage: $0 [--dry-run]"; exit 1 ;;
	esac
done

if [[ -z "${KUBECONFIG:-}" ]] && [[ -f "${HOME}/.kube/config" ]]; then
	export KUBECONFIG="${HOME}/.kube/config"
fi

if ! kubectl cluster-info &>/dev/null; then
	echo "ERROR: Cannot access cluster. Set KUBECONFIG and ensure kubectl works."
	exit 1
fi


# Remove finalizer from a VR so it can be deleted
remove_vr_finalizer() {
	local ns="$1"
	local name="$2"
	if [[ "$DRY_RUN" == "true" ]]; then
		echo "  [dry-run] would patch VR $ns/$name to remove finalizer"
		return 0
	fi
	kubectl patch vr -n "$ns" "$name" --type=json -p='[{"op": "replace", "path": "/metadata/finalizers", "value": []}]' 2>/dev/null || true
}

# Remove finalizer from a PVC so it can be deleted
remove_pvc_finalizer() {
	local ns="$1"
	local name="$2"
	if [[ "$DRY_RUN" == "true" ]]; then
		echo "  [dry-run] would patch PVC $ns/$name to remove finalizer"
		return 0
	fi
	kubectl patch pvc -n "$ns" "$name" -p '{"metadata":{"finalizers":[]}}' --type=merge 2>/dev/null || true
}

echo "=========================================="
echo "Clean replication E2E resources"
echo "=========================================="
[[ "$DRY_RUN" == "true" ]] && echo "(dry-run: no changes will be made)"
echo ""

# 1) Namespaces matching e2e-replication-*
NAMESPACES=($(kubectl get namespaces -o name 2>/dev/null | sed 's|namespace/||' | grep -E '^e2e-replication-[a-fA-F0-9]+$' || true))
if [[ ${#NAMESPACES[@]} -eq 0 ]]; then
	echo "No e2e-replication-* namespaces found."
else
	echo "Found ${#NAMESPACES[@]} e2e-replication namespace(s): ${NAMESPACES[*]}"
	for ns in "${NAMESPACES[@]}"; do
		echo "  Cleaning namespace: $ns"
		# VolumeReplications: remove finalizers then delete (skip if CRD not present)
		if kubectl get crd volumereplications.replication.storage.openshift.io &>/dev/null; then
			for n in $(kubectl get vr -n "$ns" -o jsonpath='{.items[*].metadata.name}' 2>/dev/null); do
				remove_vr_finalizer "$ns" "$n"
				[[ "$DRY_RUN" != "true" ]] && kubectl delete vr -n "$ns" "$n" --ignore-not-found --timeout=15s 2>/dev/null || true
			done
		fi

		# PVCs: remove finalizers then delete
		for pvc in $(kubectl get pvc -n "$ns" -o jsonpath='{.items[*].metadata.name}' 2>/dev/null); do
			remove_pvc_finalizer "$ns" "$pvc"
			[[ "$DRY_RUN" != "true" ]] && kubectl delete pvc -n "$ns" "$pvc" --ignore-not-found --timeout=15s 2>/dev/null || true
		done

		# VolumeSnapshots (if CRD exists)
		if kubectl get crd volumesnapshots.snapshot.storage.k8s.io &>/dev/null; then
			for vs in $(kubectl get volumesnapshot -n "$ns" -o jsonpath='{.items[*].metadata.name}' 2>/dev/null); do
				if [[ "$DRY_RUN" == "true" ]]; then
					echo "  [dry-run] would delete VolumeSnapshot $ns/$vs"
				else
					kubectl delete volumesnapshot -n "$ns" "$vs" --ignore-not-found --timeout=10s 2>/dev/null || true
				fi
			done
		fi

		# Delete namespace
		if [[ "$DRY_RUN" == "true" ]]; then
			echo "  [dry-run] would delete namespace $ns"
		else
			kubectl delete namespace "$ns" --ignore-not-found --timeout=60s 2>/dev/null || true
		fi
	done
fi

# 2) VolumeReplicationClasses created by tests (name prefix matches)
if kubectl get crd volumereplicationclasses.replication.storage.openshift.io &>/dev/null; then
	echo ""
	echo "Cleaning test VolumeReplicationClasses..."
	for vrc in $(kubectl get volumereplicationclass -o jsonpath='{.items[*].metadata.name}' 2>/dev/null); do
		if [[ "$vrc" == vrc-snapshot-* ]] || [[ "$vrc" == vrc-journal-* ]] || [[ "$vrc" == vrc-idem-* ]] || [[ "$vrc" == vrc-info-* ]] || [[ "$vrc" == vrc-nonexist-* ]] || [[ "$vrc" == vrc-fence-* ]]; then
			if [[ "$DRY_RUN" == "true" ]]; then
				echo "  [dry-run] would delete VRC $vrc"
			else
				kubectl delete volumereplicationclass "$vrc" --ignore-not-found --timeout=10s 2>/dev/null || true
				echo "  Deleted VRC $vrc"
			fi
		fi
	done
fi

# 3) NetworkFence and NetworkFenceClass created by L1-E-003 test
# Remove finalizers if stuck (e.g. controller cannot reach driver after fence)
remove_networkfence_finalizer() {
	local name="$1"
	if [[ "$DRY_RUN" == "true" ]]; then
		echo "  [dry-run] would remove finalizer from NetworkFence $name"
		return 0
	fi
	kubectl patch networkfence "$name" -p '{"metadata":{"finalizers":[]}}' --type=merge 2>/dev/null || true
}

remove_networkfenceclass_finalizer() {
	local name="$1"
	if [[ "$DRY_RUN" == "true" ]]; then
		echo "  [dry-run] would remove finalizer from NetworkFenceClass $name"
		return 0
	fi
	kubectl patch networkfenceclass "$name" -p '{"metadata":{"finalizers":[]}}' --type=merge 2>/dev/null || true
}

if kubectl get crd networkfences.csiaddons.openshift.io &>/dev/null; then
	echo ""
	echo "Cleaning test NetworkFences..."
	for nf in $(kubectl get networkfence -o jsonpath='{.items[*].metadata.name}' 2>/dev/null); do
		if [[ "$nf" == nf-fence-* ]]; then
			if [[ "$DRY_RUN" == "true" ]]; then
				echo "  [dry-run] would delete NetworkFence $nf"
			else
				kubectl delete networkfence "$nf" --ignore-not-found --timeout=30s 2>/dev/null || true
				# Remove finalizer if still present (e.g. stuck in Terminating)
				if kubectl get networkfence "$nf" &>/dev/null; then
					echo "  NetworkFence $nf stuck, removing finalizer..."
					remove_networkfence_finalizer "$nf"
					kubectl delete networkfence "$nf" --ignore-not-found --timeout=15s 2>/dev/null || true
				fi
				echo "  Deleted NetworkFence $nf"
			fi
		fi
	done
fi
if kubectl get crd networkfenceclasses.csiaddons.openshift.io &>/dev/null; then
	echo "Cleaning test NetworkFenceClasses..."
	for nfc in $(kubectl get networkfenceclass -o jsonpath='{.items[*].metadata.name}' 2>/dev/null); do
		if [[ "$nfc" == nfc-fence-* ]]; then
			if [[ "$DRY_RUN" == "true" ]]; then
				echo "  [dry-run] would delete NetworkFenceClass $nfc"
			else
				kubectl delete networkfenceclass "$nfc" --ignore-not-found --timeout=30s 2>/dev/null || true
				# Remove finalizer if still present (e.g. stuck in Terminating)
				if kubectl get networkfenceclass "$nfc" &>/dev/null; then
					echo "  NetworkFenceClass $nfc stuck, removing finalizer..."
					remove_networkfenceclass_finalizer "$nfc"
					kubectl delete networkfenceclass "$nfc" --ignore-not-found --timeout=15s 2>/dev/null || true
				fi
				echo "  Deleted NetworkFenceClass $nfc"
			fi
		fi
	done
fi

echo ""
echo "Done."
exit 0
