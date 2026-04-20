#!/bin/bash
# AGGRESSIVE NAMESPACE CLEANUP - For stuck namespaces that won't delete
# This script uses multiple methods to force-delete stuck namespaces
# Works when normal deletion, finalizer patching, etc. have failed

set -euo pipefail

echo "🔨 AGGRESSIVE NAMESPACE CLEANUP"
echo "=============================="

# Get all stuck namespaces
STUCK=$(kubectl get ns -o jsonpath='{.items[?(@.status.phase=="Terminating")].metadata.name}' 2>/dev/null || echo "")

if [[ -z "$STUCK" ]]; then
	echo "✓ No stuck namespaces found"
	exit 0
fi

COUNT=$(echo "$STUCK" | wc -w)
echo "Found $COUNT stuck namespace(s)"
echo ""

# Helper to check if namespace still exists
ns_exists() {
	kubectl get namespace "$1" &>/dev/null 2>&1
}

# Helper to show namespace phase
get_ns_phase() {
	kubectl get ns "$1" -o jsonpath='{.status.phase}' 2>/dev/null || echo "DELETED"
}

# Helper to show finalizers
show_finalizers() {
	kubectl get ns "$1" -o jsonpath='{.metadata.finalizers}' 2>/dev/null || echo "none"
}

echo "=== METHOD 1: Remove finalizers using JSON patch ==="
for ns in $STUCK; do
	if ! ns_exists "$ns"; then
		echo "  ✓ $ns already deleted"
		continue
	fi

	echo "  Patching $ns..."

	# Method 1A: Try removing with empty array
	kubectl patch namespace "$ns" -p '{"metadata":{"finalizers":[]}}' --type=merge 2>/dev/null || true

	# Method 1B: Try removing with null (more aggressive)
	kubectl patch namespace "$ns" -p '{"metadata":{"finalizers":null}}' --type=merge 2>/dev/null || true

	sleep 1
done

echo ""
echo "=== METHOD 2: Remove finalizers using JSON edit ==="
for ns in $STUCK; do
	if ! ns_exists "$ns"; then
		echo "  ✓ $ns already deleted"
		continue
	fi

	echo "  Editing $ns (method 2)..."

	# Get the namespace as JSON, remove finalizers, and apply
	kubectl get ns "$ns" -o json 2>/dev/null |
		jq '.metadata.finalizers = []' |
		kubectl replace -f - 2>/dev/null || true

	sleep 1
done

echo ""
echo "=== METHOD 3: Force delete with grace period 0 ==="
for ns in $STUCK; do
	if ! ns_exists "$ns"; then
		echo "  ✓ $ns already deleted"
		continue
	fi

	echo "  Force-deleting $ns..."

	# Try with grace-period 0
	timeout 5 kubectl delete namespace "$ns" --grace-period=0 --force 2>/dev/null || true

	sleep 1
done

echo ""
echo "=== METHOD 4: Final aggressive patch (null finalizers) ==="
for ns in $STUCK; do
	if ! ns_exists "$ns"; then
		echo "  ✓ $ns already deleted"
		continue
	fi

	echo "  Final patch on $ns..."

	# Most aggressive: set finalizers to null using strategic merge
	kubectl patch namespace "$ns" --type='json' -p='[{"op": "remove", "path": "/metadata/finalizers"}]' 2>/dev/null || true

	sleep 1
done

echo ""
echo "=== VERIFICATION ==="
REMAINING_COUNT=0
for ns in $STUCK; do
	if ns_exists "$ns"; then
		phase=$(get_ns_phase "$ns")
		finalizers=$(show_finalizers "$ns")
		echo "  ⚠️  $ns - Phase: $phase, Finalizers: $finalizers"
		REMAINING_COUNT=$((REMAINING_COUNT + 1))
	else
		echo "  ✓ $ns - DELETED"
	fi
done

echo ""
echo "=============================="
if [[ $REMAINING_COUNT -eq 0 ]]; then
	echo "✅ All $COUNT namespaces cleaned!"
else
	echo "⚠️  $REMAINING_COUNT namespace(s) still present out of $COUNT"
	echo ""
	echo "Remaining stuck namespaces:"
	for ns in $STUCK; do
		if ns_exists "$ns"; then
			echo "  - $ns (phase: $(get_ns_phase "$ns"))"
		fi
	done
	echo ""
	echo "Try manual deletion (requires admin access):"
	echo ""
	for ns in $STUCK; do
		if ns_exists "$ns"; then
			cat <<EOM
# For $ns:
kubectl get namespace $ns -o json | jq 'del(.metadata.finalizers)' | kubectl replace -f -

# OR use etcd directly (if you have access):
# etcdctl del /registry/namespaces/$ns
EOM
		fi
	done
fi
