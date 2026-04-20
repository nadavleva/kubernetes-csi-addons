#!/bin/bash
# ONE-LINER EMERGENCY CLEANUP FOR STUCK NAMESPACES
# Run this immediately to clean up your 17 stuck namespaces
# This is faster than waiting for the patched scripts

set -euo pipefail

echo "🚨 Emergency Cleanup for Stuck Namespaces"
echo "========================================"

# Get stuck namespaces
STUCK=$(kubectl get ns -o jsonpath='{.items[?(@.status.phase=="Terminating")].metadata.name}' 2>/dev/null || echo "")

if [[ -z "$STUCK" ]]; then
	echo "✓ No stuck namespaces found"
	exit 0
fi

COUNT=$(echo "$STUCK" | wc -w)
echo "Found $COUNT stuck namespace(s)"
echo ""

# Remove stale CDI CRD
echo "[1/2] Removing stale CDI CRD..."
kubectl delete crd upload.cdi.kubevirt.io --ignore-not-found 2>/dev/null
echo "✓ Stale CDI CRD removed"

# Force-delete all stuck namespaces in parallel
echo "[2/2] Force-deleting $COUNT stuck namespaces in parallel..."
echo "$STUCK" | tr ' ' '\n' | while read -r ns; do
	(
		echo "  Cleaning $ns..."
		# Remove all finalizers
		kubectl patch namespace "$ns" -p '{"metadata":{"finalizers":null}}' --type=merge 2>/dev/null || true
		# Force delete with timeout
		timeout 5 kubectl delete namespace "$ns" --grace-period=0 --force 2>/dev/null || true
	) &
done

# Wait for all background jobs
wait

echo ""
echo "✓ Cleanup initiated (may complete in background)"
echo ""
echo "Checking status..."
sleep 2
REMAINING=$(kubectl get ns -o jsonpath='{.items[?(@.status.phase=="Terminating")].metadata.name}' 2>/dev/null | wc -w || echo 0)
echo "Remaining stuck namespaces: $REMAINING"
echo ""

if [[ $REMAINING -eq 0 ]]; then
	echo "✅ All namespaces cleaned!"
else
	echo "⚠️  Some namespaces still terminating (will complete in background)"
fi
