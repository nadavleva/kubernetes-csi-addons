#!/bin/bash
# WORKAROUND: Delete namespaces without waiting (bypass discovery check)
#
# The issue: kubectl delete waits for the namespace to be fully deleted,
# which triggers the API discovery check. If we don't wait, the deletion
# request is sent but doesn't block on the discovery cache issue.

set -euo pipefail

echo "🔧 NAMESPACE DELETION WORKAROUND (Non-blocking Delete)"
echo "======================================================"

STUCK=$(kubectl get ns -o jsonpath='{.items[?(@.status.phase=="Terminating")].metadata.name}' 2>/dev/null || echo "")

if [[ -z "$STUCK" ]]; then
	echo "✓ No stuck namespaces found"
	exit 0
fi

COUNT=$(echo "$STUCK" | wc -w)
echo "Found $COUNT stuck namespace(s)"
echo ""

echo "Sending DELETE requests (non-blocking)..."
echo ""

for ns in $STUCK; do
	echo "Deleting $ns (non-blocking)..."

	# First, try to remove finalizers completely using kubectl patch
	# Use triple backslash to ensure null is passed
	kubectl patch namespace "$ns" --type=json -p='[{"op":"remove","path":"/metadata/finalizers"}]' 2>/dev/null ||
		kubectl patch namespace "$ns" -p='{"metadata":{"finalizers":null}}' --type=merge 2>/dev/null ||
		true

	sleep 0.5

	# Send delete request without waiting
	# This sends the deletion signal to the API server but doesn't block
	kubectl delete namespace "$ns" --grace-period=0 --force --wait=false 2>/dev/null || true

	echo "  → Deletion signal sent"
done

echo ""
echo "======================================================"
echo "Deletion requests sent (may take 30-60 seconds to complete)"
echo ""
echo "Monitoring progress..."
echo ""

# Monitor progress for 2 minutes
for _ in {1..12}; do
	sleep 10
	REMAINING=$(kubectl get ns -o jsonpath='{.items[?(@.status.phase=="Terminating")].metadata.name}' 2>/dev/null | wc -w || echo 0)

	if [[ $REMAINING -eq 0 ]]; then
		echo "✅ All namespaces deleted!"
		exit 0
	fi

	echo "Still remaining: $REMAINING namespace(s)"
done

echo ""
echo "⚠️  Some namespaces still terminating after 2 minutes"
echo ""
echo "Final status:"
kubectl get ns | grep -E "NAME|e2e-replication"
