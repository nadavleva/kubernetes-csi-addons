#!/bin/bash
# PROPER FIX for stuck namespaces with stale CDI API discovery
#
# The root issue: API server has stale CDI CRD in its discovery cache
# Even after deleting the CRD, the cache isn't cleared
#
# Solution: Delete CRD → Restart API server → Verify health → Delete namespaces

set -euo pipefail

echo "🔧 FIXING STUCK NAMESPACES (Stale CDI API Discovery)"
echo "======================================================"

# Step 1: Check for stuck namespaces
echo ""
echo "[1/5] Checking for stuck namespaces..."
STUCK=$(kubectl get ns -o jsonpath='{.items[?(@.status.phase=="Terminating")].metadata.name}' 2>/dev/null || echo "")

if [[ -z "$STUCK" ]]; then
	echo "✓ No stuck namespaces found"
	exit 0
fi

COUNT=$(echo "$STUCK" | wc -w)
echo "✓ Found $COUNT stuck namespace(s)"
echo ""

# Step 2: Remove stale CDI CRD
echo "[2/5] Removing stale CDI CRD (upload.cdi.kubevirt.io)..."
if kubectl get crd upload.cdi.kubevirt.io &>/dev/null 2>&1; then
	echo "  Found stale CDI CRD, deleting..."
	kubectl delete crd upload.cdi.kubevirt.io --ignore-not-found 2>/dev/null || true
	echo "  ✓ Stale CDI CRD deleted"
else
	echo "  ✓ No stale CDI CRD found"
fi
echo ""

# Step 3: Restart API server to clear discovery cache
echo "[3/5] Restarting kube-apiserver to clear discovery cache..."
API_PODS=$(kubectl get pods -n kube-system -l component=kube-apiserver -o jsonpath='{.items[*].metadata.name}' 2>/dev/null | wc -w || echo 0)

if [[ $API_PODS -gt 0 ]]; then
	echo "  Found $API_PODS kube-apiserver pod(s)"
	echo "  Deleting kube-apiserver pods (kubelet will restart them)..."
	kubectl delete pods -n kube-system -l component=kube-apiserver --grace-period=30 2>/dev/null || true
	echo "  ✓ API server restart initiated"
else
	echo "  ⚠️  No kube-apiserver pods found (might be static pods)"
fi
echo ""

# Step 4: Wait for API server to come back healthy
echo "[4/5] Waiting for API server to come back healthy..."
MAX_WAIT=60
WAIT_TIME=0
while [[ $WAIT_TIME -lt $MAX_WAIT ]]; do
	if kubectl cluster-info &>/dev/null && kubectl api-resources &>/dev/null; then
		echo "  ✓ API server is back and responding"
		break
	fi
	echo "  Waiting... ($WAIT_TIME/$MAX_WAIT seconds)"
	sleep 2
	WAIT_TIME=$((WAIT_TIME + 2))
done

if [[ $WAIT_TIME -ge $MAX_WAIT ]]; then
	echo "  ⚠️  WARNING: API server took longer than expected"
	echo "     Waiting additional time for cluster to stabilize..."
	sleep 10
fi
echo ""

# Step 5: Verify CDI CRD is gone and API discovery is working
echo "[5/5] Verifying API discovery is working..."
if kubectl get crd upload.cdi.kubevirt.io &>/dev/null 2>&1; then
	echo "  ⚠️  WARNING: CDI CRD still exists, trying to delete again..."
	kubectl delete crd upload.cdi.kubevirt.io --ignore-not-found 2>/dev/null || true
fi

if kubectl api-resources 2>&1 | grep -i "upload\|cdi" >/dev/null 2>&1; then
	echo "  ⚠️  WARNING: CDI still showing in api-resources"
	echo "     This may resolve after additional API server restart"
fi

echo "  ✓ API discovery verification complete"
echo ""

# Step 6: Now try to delete namespaces
echo "=========================================="
echo "Now attempting to delete stuck namespaces..."
echo "=========================================="
echo ""

DELETED=0
FAILED=0

for ns in $STUCK; do
	phase=$(kubectl get ns "$ns" -o jsonpath='{.status.phase}' 2>/dev/null || echo "DELETED")

	if [[ "$phase" != "Terminating" ]]; then
		echo "  ✓ $ns - Already $phase"
		DELETED=$((DELETED + 1))
		continue
	fi

	echo "  Deleting $ns..."

	# Check if namespace still has the discovery failure condition
	discovery_failure=$(kubectl describe ns "$ns" 2>/dev/null | grep -i "NamespaceDeletionDiscoveryFailure.*True" || echo "")

	if [[ -n "$discovery_failure" ]]; then
		echo "    ⚠️  Still has discovery failure, trying to remove finalizers..."
		# Remove finalizers one more time now that API server is restarted
		kubectl patch namespace "$ns" -p '{"metadata":{"finalizers":null}}' --type=merge 2>/dev/null || true
		sleep 1
	fi

	# Try to delete with timeout
	if timeout 10 kubectl delete namespace "$ns" --grace-period=0 --force 2>/dev/null; then
		echo "    ✓ $ns deleted"
		DELETED=$((DELETED + 1))
	else
		echo "    ⚠️  $ns still not deleted, may require manual intervention"
		FAILED=$((FAILED + 1))
	fi
done

echo ""
echo "=========================================="
echo "RESULTS:"
echo "=========================================="
echo "  Deleted: $DELETED/$COUNT"
echo "  Failed: $FAILED/$COUNT"
echo ""

if [[ $FAILED -eq 0 ]]; then
	echo "✅ All stuck namespaces have been cleared!"
	exit 0
else
	echo "⚠️  Some namespaces still stuck. Checking status..."
	echo ""
	for ns in $STUCK; do
		phase=$(kubectl get ns "$ns" -o jsonpath='{.status.phase}' 2>/dev/null || echo "DELETED")
		if [[ "$phase" == "Terminating" ]]; then
			echo "  Still stuck: $ns"
			echo "    Getting discovery status..."
			kubectl describe ns "$ns" 2>/dev/null | grep -A 2 "NamespaceDeletionDiscoveryFailure"
		fi
	done
fi
