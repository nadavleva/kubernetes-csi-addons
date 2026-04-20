#!/bin/bash
# Diagnostic and remediation script for stale Kubernetes API discovery issues.
#
# Detects and fixes stale API cache problems that can cause:
# - NamespaceDeletionDiscoveryFailure with stale CDI API discovery
# - Test failures due to API resource discovery timeouts
# - KubeVirt CDI API version conflicts (e.g., upload.cdi.kubevirt.io/v1beta1)
#
# Usage: ./hack/diagnose-api-discovery.sh [--fix] [--contexts <context1,context2,...>]
#
# Options:
#   --fix                  Apply fixes (removes stale API CRDs and resets discovery cache)
#   --contexts <contexts>  Comma-separated list of contexts to check (default: auto-detect DR1/DR2 or current)
#   --dry-run              Show what would be fixed without applying changes
#
# Examples:
#   ./hack/diagnose-api-discovery.sh                # Diagnose only
#   ./hack/diagnose-api-discovery.sh --fix          # Diagnose and fix
#   ./hack/diagnose-api-discovery.sh --dry-run      # Show fixes without applying
#   ./hack/diagnose-api-discovery.sh --fix --contexts DR1,DR2

set -euo pipefail

FIX=false
DRY_RUN=false
CONTEXTS=()

for arg in "$@"; do
	case "$arg" in
	--fix) FIX=true ;;
	--dry-run) DRY_RUN=true ;;
	--contexts)
		shift
		IFS=',' read -ra CONTEXTS <<<"$1"
		;;
	*)
		echo "Unknown option: $arg"
		echo "Usage: $0 [--fix] [--dry-run] [--contexts <context1,context2,...>]"
		exit 1
		;;
	esac
done

# Helper to run kubectl in a specific context
kubectl_ctx() {
	local ctx="$1"
	shift
	kubectl --context="$ctx" "$@"
}

# Auto-detect DR1/DR2 contexts if none specified
if [[ ${#CONTEXTS[@]} -eq 0 ]]; then
	mapfile -t AVAILABLE_CONTEXTS < <(kubectl config get-contexts -o name 2>/dev/null || echo "")
	for ctx in "${AVAILABLE_CONTEXTS[@]}"; do
		if [[ "$ctx" == *"DR1"* ]] || [[ "$ctx" == *"DR2"* ]] || [[ "$ctx" == *"dr1"* ]] || [[ "$ctx" == *"dr2"* ]]; then
			CONTEXTS+=("$ctx")
		fi
	done
	# If no DR contexts found, use current context
	if [[ ${#CONTEXTS[@]} -eq 0 ]]; then
		CURRENT_CONTEXT=$(kubectl config current-context 2>/dev/null || echo "")
		if [[ -n "$CURRENT_CONTEXT" ]]; then
			CONTEXTS=("$CURRENT_CONTEXT")
		fi
	fi
fi

if [[ ${#CONTEXTS[@]} -eq 0 ]]; then
	echo "ERROR: No Kubernetes contexts found. Set KUBECONFIG or use --contexts."
	exit 1
fi

# Check if API server can discover all resource types
check_api_discovery() {
	local ctx="$1"
	echo ""
	echo "Checking API discovery for context: $ctx"
	echo "=========================================="

	# Try to get all APIs
	if ! kubectl_ctx "$ctx" api-resources &>/dev/null; then
		echo "⚠️  API discovery failed for $ctx (kubectl api-resources error)"
		return 1
	fi

	# Check for known problematic APIs
	local has_issues=0

	# 1. Check for stale CDI APIs
	echo "  Checking KubeVirt CDI APIs..."
	local cdi_apis
	cdi_apis=$(kubectl_ctx "$ctx" api-resources 2>&1 | grep -i cdi || true)
	if [[ -z "$cdi_apis" ]]; then
		echo "    ✓ No CDI resources found (CDI not installed or clean)"
	else
		echo "    Found CDI resources:"
		printf '%s\n' "$cdi_apis" | sed 's/^/      /'
		# Check for conflicts
		if kubectl_ctx "$ctx" get crd upload.cdi.kubevirt.io &>/dev/null 2>&1; then
			echo "    ⚠️  Found upload.cdi.kubevirt.io CRD"
			has_issues=1
		fi
	fi

	# 2. Check for stale CRDs without matching API groups in API server
	echo ""
	echo "  Checking for orphaned CRDs (not discoverable via API server)..."
	local orphaned_count=0
	mapfile -t CRDS < <(kubectl_ctx "$ctx" get crd -o jsonpath='{.items[*].metadata.name}' 2>/dev/null)
	for crd in "${CRDS[@]}"; do
		local group
		group="${crd##*.}"
		# Try to get the API group version
		if ! kubectl_ctx "$ctx" api-resources --api-group="$group" &>/dev/null 2>&1; then
			# Only warn if it's not a known internal CRD
			if ! echo "$crd" | grep -qE "(kubernetes.io|k8s.io)$"; then
				echo "    ⚠️  Orphaned CRD: $crd (API group $group not discoverable)"
				((orphaned_count++))
				has_issues=1
			fi
		fi
	done
	if [[ $orphaned_count -eq 0 ]]; then
		echo "    ✓ No orphaned CRDs found"
	fi

	# 3. Check kube-apiserver logs for discovery errors
	echo ""
	echo "  Checking API server for discovery-related errors..."
	if kubectl_ctx "$ctx" get pods -n kube-system -l component=kube-apiserver &>/dev/null 2>&1; then
		local api_pods
		mapfile -t api_pods < <(kubectl_ctx "$ctx" get pods -n kube-system -l component=kube-apiserver -o jsonpath='{.items[*].metadata.name}' 2>/dev/null || echo "")
		if [[ ${#api_pods[@]} -gt 0 ]]; then
			for pod in "${api_pods[@]}"; do
				local error_count
				error_count=$(kubectl_ctx "$ctx" logs -n kube-system "$pod" --tail=100 2>/dev/null | grep -ic "discovery\|stale\|cdi" || echo 0)
				if [[ $error_count -gt 0 ]]; then
					echo "    ⚠️  Found $error_count discovery-related errors in $pod logs"
					has_issues=1
				else
					echo "    ✓ No discovery errors in $pod logs"
				fi
			done
		else
			echo "    (no kube-apiserver pods found, skipping log check)"
		fi
	else
		echo "    (cannot access kube-system, skipping log check)"
	fi

	# 4. Check for namespaces stuck in Terminating state
	echo ""
	echo "  Checking for stuck namespaces (Terminating state)..."
	local stuck_ns
	stuck_ns=$(kubectl_ctx "$ctx" get ns -o jsonpath='{.items[?(@.status.phase=="Terminating")].metadata.name}' 2>/dev/null || echo "")
	if [[ -n "$stuck_ns" ]]; then
		echo "    Found stuck namespaces:"
		echo "$stuck_ns" | tr ' ' '\n' | sed 's/^/      /'
		has_issues=1
	else
		echo "    ✓ No stuck namespaces"
	fi

	# 5. Check e2e-replication-* namespaces for deletion conditions
	echo ""
	echo "  Checking e2e-replication-* namespaces for deletion failures..."
	mapfile -t e2e_ns < <(kubectl_ctx "$ctx" get ns -o jsonpath='{.items[?(@.metadata.name | test("^e2e-replication-"))].metadata.name}' 2>/dev/null || echo "")
	if [[ ${#e2e_ns[@]} -gt 0 ]]; then
		echo "    Found ${#e2e_ns[@]} e2e-replication-* namespace(s)"
		for ns in "${e2e_ns[@]}"; do
			local discovery_failure
			discovery_failure=$(kubectl_ctx "$ctx" describe ns "$ns" 2>/dev/null | grep -i "NamespaceDeletionDiscoveryFailure\|DiscoveryFailed" || echo "")
			if [[ -n "$discovery_failure" ]]; then
				echo "      ⚠️  $ns: API discovery failure detected"
				has_issues=1
			else
				local phase
				phase=$(kubectl_ctx "$ctx" get ns "$ns" -o jsonpath='{.status.phase}')
				echo "      $ns: phase=$phase"
			fi
		done
	else
		echo "    ✓ No e2e-replication-* namespaces found"
	fi

	if [[ $has_issues -ne 0 ]]; then
		return 1
	fi
	return 0
}

# Apply fixes for discovered issues
apply_fixes() {
	local ctx="$1"
	echo ""
	echo "Applying fixes for context: $ctx"
	echo "=========================================="

	# 1. Remove stale CDI CRDs if they exist
	if kubectl_ctx "$ctx" get crd upload.cdi.kubevirt.io &>/dev/null 2>&1; then
		echo "  Removing stale CDI CRD: upload.cdi.kubevirt.io"
		if [[ "$DRY_RUN" != "true" ]]; then
			kubectl_ctx "$ctx" delete crd upload.cdi.kubevirt.io --ignore-not-found 2>/dev/null || true
		fi
	fi

	# 2. Force delete stuck namespaces by removing finalizers
	echo "  Checking for stuck namespaces to force-delete..."
	local stuck_ns
	stuck_ns=$(kubectl_ctx "$ctx" get ns -o jsonpath='{.items[?(@.status.phase=="Terminating")].metadata.name}' 2>/dev/null || echo "")
	if [[ -n "$stuck_ns" ]]; then
		echo "    Removing finalizers from $(echo "$stuck_ns" | wc -w) stuck namespace(s) (non-blocking)..."
		if [[ "$DRY_RUN" != "true" ]]; then
			# Remove finalizers from all stuck namespaces in parallel (non-blocking)
			for ns in $stuck_ns; do
				(
					kubectl_ctx "$ctx" patch namespace "$ns" -p '{"metadata":{"finalizers":[]}}' --type=merge 2>/dev/null || true
					# Try to delete with timeout (don't wait indefinitely)
					timeout 5 kubectl_ctx "$ctx" delete namespace "$ns" --ignore-not-found --grace-period=0 --force 2>/dev/null || true
				) &
			done
			# Wait for all background operations to complete (max 30s)
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
			echo "    Finalizers removed from all stuck namespaces"
		fi
	fi

	# 3. Clear API discovery cache by restarting kube-apiserver (if possible)
	if kubectl_ctx "$ctx" get pods -n kube-system -l component=kube-apiserver &>/dev/null 2>&1; then
		echo "  Clearing API discovery cache (restarting kube-apiserver pods)..."
		if [[ "$DRY_RUN" != "true" ]]; then
			if kubectl_ctx "$ctx" delete pods -n kube-system -l component=kube-apiserver --grace-period=30 2>/dev/null; then
				echo "    Waiting 10s for kube-apiserver to restart..."
				sleep 10
				# Verify API server is back
				if ! kubectl_ctx "$ctx" cluster-info &>/dev/null; then
					echo "    ⚠️  WARNING: API server did not come back quickly. Cluster may be recovering. Try again in 30s."
				fi
			fi
		fi
	fi

	# 4. Clear orphaned CRDs
	echo "  Checking for orphaned CRDs to clean..."
	mapfile -t CRDS < <(kubectl_ctx "$ctx" get crd -o jsonpath='{.items[*].metadata.name}' 2>/dev/null || echo "")
	for crd in "${CRDS[@]}"; do
		if echo "$crd" | grep -qE "cdi\.kubevirt|upload\." 2>/dev/null; then
			echo "    Removing potentially stale CRD: $crd"
			if [[ "$DRY_RUN" != "true" ]]; then
				kubectl_ctx "$ctx" delete crd "$crd" --ignore-not-found 2>/dev/null || true
			fi
		fi
	done

	echo "  Fixes applied (or would be applied in dry-run mode)"
}

# Main execution
echo "API Discovery Diagnostic & Remediation Tool"
echo "==========================================="
[[ "$DRY_RUN" == "true" ]] && echo "DRY-RUN MODE (no changes will be applied)"
[[ "$FIX" == "true" ]] && echo "FIX mode enabled"
echo ""

OVERALL_STATUS=0
for CURRENT_CTX in "${CONTEXTS[@]}"; do
	if ! check_api_discovery "$CURRENT_CTX"; then
		OVERALL_STATUS=1
		if [[ "$FIX" == "true" ]]; then
			apply_fixes "$CURRENT_CTX"
		fi
	fi
done

echo ""
echo "=========================================="
if [[ $OVERALL_STATUS -eq 0 ]]; then
	echo "✓ All contexts have healthy API discovery"
	exit 0
else
	echo "⚠️  Issues detected in one or more contexts"
	if [[ "$FIX" != "true" ]]; then
		echo ""
		echo "To fix these issues, run:"
		echo "  $0 --fix"
	fi
	exit 1
fi
