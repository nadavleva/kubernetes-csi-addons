#!/bin/bash
# Quick health check for API discovery before running tests.
#
# Runs as part of the E2E test pipeline to detect and remediate API discovery issues
# that can cause namespaces to get stuck in Terminating state (e.g., stale CDI API discovery).
#
# Returns 0 if healthy, 1 if issues found (non-blocking by default; use --fatal to exit)
#
# Usage: ./hack/api-discovery-health-check.sh [--contexts <ctx1,ctx2,...>] [--fatal] [--fix]

set -euo pipefail

FATAL=false
FIX=false
CONTEXTS=()

for arg in "$@"; do
	case "$arg" in
	--fatal) FATAL=true ;;
	--fix) FIX=true ;;
	--contexts)
		shift
		IFS=',' read -ra CONTEXTS <<<"$1"
		;;
	esac
done

# Auto-detect DR contexts if not specified
if [[ ${#CONTEXTS[@]} -eq 0 ]]; then
	mapfile -t AVAILABLE_CONTEXTS < <(kubectl config get-contexts -o name 2>/dev/null || echo "")
	for ctx in "${AVAILABLE_CONTEXTS[@]}"; do
		if [[ "$ctx" == *"DR1"* ]] || [[ "$ctx" == *"DR2"* ]] || [[ "$ctx" == *"dr1"* ]] || [[ "$ctx" == *"dr2"* ]]; then
			CONTEXTS+=("$ctx")
		fi
	done
	if [[ ${#CONTEXTS[@]} -eq 0 ]]; then
		CURRENT_CONTEXT=$(kubectl config current-context 2>/dev/null || echo "")
		[[ -n "$CURRENT_CONTEXT" ]] && CONTEXTS=("$CURRENT_CONTEXT")
	fi
fi

if [[ ${#CONTEXTS[@]} -eq 0 ]]; then
	echo "⚠️  No contexts found for health check"
	exit 0
fi

kubectl_ctx() {
	local ctx="$1"
	shift
	kubectl --context="$ctx" "$@" 2>/dev/null || true
}

quick_check() {
	local ctx="$1"
	local has_issues=0

	# Check 1: API resources discoverable
	if ! kubectl_ctx "$ctx" api-resources &>/dev/null; then
		echo "❌ $ctx: API discovery failed"
		has_issues=1
	fi

	# Check 2: CDI API conflicts
	if kubectl_ctx "$ctx" get crd upload.cdi.kubevirt.io &>/dev/null 2>&1; then
		echo "⚠️  $ctx: Stale CDI CRD detected (upload.cdi.kubevirt.io)"
		has_issues=1
		if [[ "$FIX" == "true" ]]; then
			kubectl_ctx "$ctx" delete crd upload.cdi.kubevirt.io --ignore-not-found 2>/dev/null || true
			echo "   Fixed: Deleted stale CDI CRD"
		fi
	fi

	# Check 3: Stuck namespaces
	local stuck_ns
	stuck_ns=$(kubectl_ctx "$ctx" get ns -o jsonpath='{.items[?(@.status.phase=="Terminating")].metadata.name}' 2>/dev/null || echo "")
	if [[ -n "$stuck_ns" ]]; then
		echo "⚠️  $ctx: Stuck namespaces found: $stuck_ns"
		has_issues=1
		if [[ "$FIX" == "true" ]]; then
			echo "   Removing finalizers from stuck namespaces (non-blocking)..."
			# Remove finalizers from all stuck namespaces in parallel (non-blocking)
			for ns in $stuck_ns; do
				# Remove finalizers asynchronously
				(
					kubectl_ctx "$ctx" patch namespace "$ns" -p '{"metadata":{"finalizers":[]}}' --type=merge 2>/dev/null || true
					# Try to delete with timeout (don't wait indefinitely)
					timeout 5 kubectl_ctx "$ctx" delete namespace "$ns" --ignore-not-found --grace-period=0 --force 2>/dev/null || true
				) &
			done
			# Wait for all background deletions to complete (max 30s)
			local wait_count=0
			while [[ $(jobs -r -p | wc -l) -gt 0 ]] && [[ $wait_count -lt 60 ]]; do
				sleep 0.5
				wait_count=$((wait_count + 1))
			done
			# Kill any remaining jobs
			local pids
			pids=$(jobs -r -p 2>/dev/null)
			if [[ -n "$pids" ]]; then
				kill "$pids" 2>/dev/null || true
			fi
			echo "   Finalizers removed from stuck namespaces"
		fi
	fi

	return $has_issues
}

echo "API Discovery Health Check"
echo "=========================="

OVERALL_STATUS=0
for ctx in "${CONTEXTS[@]}"; do
	if ! quick_check "$ctx"; then
		OVERALL_STATUS=1
	fi
done

if [[ $OVERALL_STATUS -eq 0 ]]; then
	echo "✓ All contexts passed API discovery health check"
	exit 0
else
	echo ""
	echo "⚠️  API discovery issues detected"
	if [[ "$FIX" != "true" ]]; then
		echo "   To auto-fix, run with: --fix"
		echo "   For detailed diagnosis, run: ./hack/diagnose-api-discovery.sh --fix"
	fi
	if [[ "$FATAL" == "true" ]]; then
		exit 1
	else
		echo "   Proceeding anyway (use --fatal to exit on errors)"
		exit 0
	fi
fi
