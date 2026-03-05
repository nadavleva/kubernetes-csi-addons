#!/bin/bash
# Setup NetworkFenceClass and NetworkFence for CephFS (L1-E-003 test)
# Handles immutable parameters by deleting and recreating the NetworkFenceClass
#
# Usage: ./Temp/setup-networkfence-cephfs.sh

set -e

NFC_NAME="rook-cephfs-fence-class"
NF_NAME="test-fence-cephfs"
PROVISIONER="rook-ceph.cephfs.csi.ceph.com"
SECRET_NAME="rook-csi-cephfs-provisioner"
SECRET_NS="rook-ceph"
CLUSTER_ID="rook-ceph"
CIDR="192.168.122.164/32"  # dr2 node

echo "=== Deleting existing NetworkFence (if any) ==="
kubectl delete networkfence "$NF_NAME" --ignore-not-found --timeout=30s 2>/dev/null || true

echo "=== Deleting existing NetworkFenceClass (if any) ==="
if kubectl get networkfenceclass "$NFC_NAME" &>/dev/null; then
  kubectl delete networkfenceclass "$NFC_NAME" --timeout=30s 2>/dev/null || true
  # Remove finalizer if stuck in Terminating
  if kubectl get networkfenceclass "$NFC_NAME" &>/dev/null; then
    echo "  Removing finalizer from stuck NetworkFenceClass..."
    kubectl patch networkfenceclass "$NFC_NAME" -p '{"metadata":{"finalizers":[]}}' --type=merge
  fi
fi

echo "=== Creating NetworkFenceClass with clusterID ==="
kubectl apply -f - <<EOF
apiVersion: csiaddons.openshift.io/v1alpha1
kind: NetworkFenceClass
metadata:
  name: $NFC_NAME
spec:
  provisioner: $PROVISIONER
  parameters:
    clusterID: $CLUSTER_ID
    csiaddons.openshift.io/networkfence-secret-name: $SECRET_NAME
    csiaddons.openshift.io/networkfence-secret-namespace: $SECRET_NS
EOF

echo "=== Creating NetworkFence ==="
kubectl apply -f - <<EOF
apiVersion: csiaddons.openshift.io/v1alpha1
kind: NetworkFence
metadata:
  name: $NF_NAME
spec:
  networkFenceClassName: $NFC_NAME
  cidrs:
    - $CIDR
  fenceState: Fenced
EOF

echo "=== Waiting for NetworkFence status ==="
for i in $(seq 1 30); do
  RESULT=$(kubectl get networkfence "$NF_NAME" -o jsonpath='{.status.result}' 2>/dev/null || echo "")
  if [ -n "$RESULT" ]; then
    MESSAGE=$(kubectl get networkfence "$NF_NAME" -o jsonpath='{.status.message}' 2>/dev/null || echo "")
    echo "Result: $RESULT"
    echo "Message: $MESSAGE"
    [ "$RESULT" = "Succeeded" ] && exit 0
    [ "$RESULT" = "Failed" ] && exit 1
  fi
  sleep 2
done

echo "Timeout waiting for status. Current state:"
kubectl get networkfence "$NF_NAME" -o yaml
exit 1
