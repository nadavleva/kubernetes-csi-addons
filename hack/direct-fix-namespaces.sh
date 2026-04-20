#!/bin/bash
# FINAL SOLUTION: Edit namespaces directly via etcd or kubectl JSON manipulation
#
# When API server is a static pod and discovery cache won't clear,
# we need to edit the namespace object directly, bypassing the discovery layer

set -euo pipefail

echo "🔨 DIRECT NAMESPACE FIX (Bypass API Discovery Cache)"
echo "===================================================="

# Check for stuck namespaces
STUCK=$(kubectl get ns -o jsonpath='{.items[?(@.status.phase=="Terminating")].metadata.name}' 2>/dev/null || echo "")

if [[ -z "$STUCK" ]]; then
	echo "✓ No stuck namespaces found"
	exit 0
fi

COUNT=$(echo "$STUCK" | wc -w)
echo "Found $COUNT stuck namespace(s)"
echo ""

# Try Method 1: Direct JSON manipulation (get JSON, remove finalizers, apply)
echo "[Method 1] Direct JSON manipulation..."
echo ""

DELETED=0
FAILED=0

for ns in $STUCK; do
	echo "Fixing $ns..."

	# Get namespace as JSON, remove finalizers field entirely, and replace
	if kubectl get namespace "$ns" -o json 2>/dev/null |
		jq 'del(.metadata.finalizers)' |
		kubectl replace -f - 2>/dev/null; then

		echo "  ✓ Finalizers removed via JSON, attempting delete..."

		# Now try to delete
		if timeout 5 kubectl delete namespace "$ns" --grace-period=0 --force 2>/dev/null; then
			echo "  ✓ $ns DELETED"
			DELETED=$((DELETED + 1))
		else
			echo "  ⚠️  Delete timed out, namespace may still be terminating"
			# Check if it's actually gone
			if ! kubectl get namespace "$ns" &>/dev/null 2>&1; then
				echo "  ✓ $ns is actually gone (just slow to show)"
				DELETED=$((DELETED + 1))
			else
				echo "  ⚠️  $ns still exists"
				FAILED=$((FAILED + 1))
			fi
		fi
	else
		echo "  ⚠️  JSON replacement failed, trying alternative method..."
		FAILED=$((FAILED + 1))
	fi

	sleep 1
done

echo ""
echo "===================================================="
echo "Results after JSON manipulation:"
echo "  Deleted: $DELETED/$COUNT"
echo "  Issues: $FAILED/$COUNT"
echo ""

# If some are still stuck, try Method 2: Using kubectl patch with JSON operations
if [[ $FAILED -gt 0 ]]; then
	echo "[Method 2] Strategic JSON patch removal..."
	echo ""

	for ns in $STUCK; do
		# Check if still stuck
		if kubectl get namespace "$ns" &>/dev/null 2>&1; then
			echo "Still stuck: $ns - trying JSON operations patch..."

			# Try removing finalizers with JSON patch operation
			if kubectl patch namespace "$ns" --type json -p='[{"op": "remove", "path": "/metadata/finalizers"}]' 2>/dev/null; then
				echo "  ✓ JSON patch succeeded"
				sleep 1

				# Try delete again
				if timeout 5 kubectl delete namespace "$ns" --grace-period=0 --force 2>/dev/null ||
					! kubectl get namespace "$ns" &>/dev/null 2>&1; then
					echo "  ✓ $ns cleaned"
					DELETED=$((DELETED + 1))
					FAILED=$((FAILED - 1))
				fi
			fi
		fi
	done

	echo ""
	echo "===================================================="
	echo "Final results:"
	echo "  Deleted: $DELETED/$COUNT"
	echo "  Still stuck: $FAILED/$COUNT"
fi

echo ""

if [[ $FAILED -eq 0 ]]; then
	echo "✅ All namespaces fixed!"
	exit 0
else
	echo "⚠️  Some namespaces still stuck. Last resort: etcd access needed."
	echo ""
	echo "Remaining stuck namespaces:"
	for ns in $STUCK; do
		if kubectl get namespace "$ns" &>/dev/null 2>&1; then
			echo "  - $ns"
			# Show what's blocking
			kubectl get namespace "$ns" -o jsonpath='{.metadata.finalizers}' && echo ""
		fi
	done
	echo ""
	echo "If etcd is accessible on the nodes, you can directly remove:"
	echo ""
	for ns in $STUCK; do
		if kubectl get namespace "$ns" &>/dev/null 2>&1; then
			cat <<EOM
# For $ns (if you have etcd access on control plane):
kubectl debug node/$(kubectl get nodes -o jsonpath='{.items[0].metadata.name}') -it --image=ubuntu
  # Inside the debug pod:
  crictl exec -it <etcd-container-id> etcdctl del /registry/namespaces/$ns

# Or restart kubelet to force cleanup:
ssh root@<node-ip>
  systemctl restart kubelet

EOM
		fi
	done
	exit 1
fi
