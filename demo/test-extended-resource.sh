#!/usr/bin/env bash

# Copyright The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# This script demonstrates the DRA extended-resource feature (KEP-5004) by
# deploying the demo and verifying that the pods get GPUs allocated through
# the classic `resources.limits` API.

set -e

NAMESPACE="extended-resource-request"

echo "=== DRA Extended Resource Requests Demo (KEP-5004) ==="
echo

if ! command -v kubectl &> /dev/null; then
    echo "❌ kubectl is not available. Please install kubectl and ensure cluster access."
    exit 1
fi

if ! kubectl cluster-info &> /dev/null; then
    echo "❌ Unable to access Kubernetes cluster. Please check your kubeconfig."
    exit 1
fi

echo "✅ Kubernetes cluster is accessible"

echo "📦 Applying extended-resource-request.yaml demo..."
kubectl apply -f demo/extended-resource-request.yaml

# pod0 uses the implicit extended-resource name and works against any default
# chart install. Treat it as a hard requirement.
echo "⏳ Waiting for pod0 (implicit name) to be Ready..."
kubectl wait --for=condition=Ready pod/pod0 -n "${NAMESPACE}" --timeout=120s

# pod1 uses an explicit extended-resource name that requires the chart to be
# installed with `--set deviceClass.extendedResourceName=example.com/gpu`.
# If the chart wasn't configured that way, pod1 will stay Pending. Don't fail
# the script — just print a helpful hint and continue.
echo "⏳ Waiting for pod1 (explicit name) to be Ready..."
POD1_READY=true
if ! kubectl wait --for=condition=Ready pod/pod1 -n "${NAMESPACE}" --timeout=30s 2>/dev/null; then
    POD1_READY=false
    echo
    echo "ℹ️  pod1 did not become Ready in 30s. Most recent scheduler events:"
    kubectl get events -n "${NAMESPACE}" \
        --field-selector "involvedObject.name=pod1" \
        --sort-by='.lastTimestamp' \
        -o custom-columns=TIME:.lastTimestamp,REASON:.reason,MESSAGE:.message \
        2>/dev/null | tail -n 5 || true
    echo
    echo "    A common cause is that the chart was installed without an explicit"
    echo "    extended-resource mapping for 'example.com/gpu'. To exercise the"
    echo "    explicit form, reinstall the chart with:"
    echo
    echo "      helm upgrade -i \\"
    echo "        --create-namespace \\"
    echo "        --namespace dra-example-driver \\"
    echo "        --set deviceClass.extendedResourceName=example.com/gpu \\"
    echo "        dra-example-driver \\"
    echo "        deployments/helm/dra-example-driver"
    echo
    echo "    If the chart is already configured that way, run"
    echo "    'kubectl describe pod pod1 -n ${NAMESPACE}' for more detail."
    echo
fi

echo "=== Pod Status ==="
kubectl get pods -n "${NAMESPACE}"

echo
echo "=== ResourceClaims (created by the scheduler for extended-resource pods) ==="
# These ResourceClaims are created by the kube-scheduler with annotation
# `resource.kubernetes.io/extended-resource-claim: <pod-name>` and owned by
# the pod. They are GC'd when the pod is deleted.
kubectl get resourceclaims -n "${NAMESPACE}" -o wide

verify_pod() {
    local pod=$1

    echo
    echo "=== ${pod} logs (looking for GPU_DEVICE_* env vars) ==="
    local logs=""
    local gpu=""
    # `kubectl logs` can briefly return empty output even after the pod is
    # Ready, so retry for a short window.
    for _ in 1 2 3 4 5; do
        logs=$(kubectl logs -n "${NAMESPACE}" "${pod}" -c ctr0 2>/dev/null || true)
        gpu=$(echo "${logs}" | sed -nE 's/^declare -x GPU_DEVICE_[0-9]+="(.+)"$/\1/p' | head -n1)
        if [[ -n "${gpu}" ]]; then
            break
        fi
        sleep 2
    done

    echo "${logs}" | grep -E "GPU_DEVICE_[0-9]+" || true

    if [[ -n "${gpu}" ]]; then
        echo "✅ ${pod} got GPU: ${gpu}"
    else
        echo "❌ ${pod}: no GPU_DEVICE_* env var found in container logs"
        return 1
    fi

    echo
    echo "=== ${pod}.status.extendedResourceClaimStatus ==="
    # Demonstrates the new pod.status field added by KEP-5004. It maps each
    # <container, extended-resource-name> pair to the request name in the
    # scheduler-created ResourceClaim, which is how the kubelet routes
    # devices to the right container.
    if command -v jq &>/dev/null; then
        kubectl get pod -n "${NAMESPACE}" "${pod}" -o json \
            | jq '.status.extendedResourceClaimStatus'
    else
        kubectl get pod -n "${NAMESPACE}" "${pod}" -o yaml \
            | sed -n '/^  extendedResourceClaimStatus:/,/^  [a-zA-Z]/p' \
            | sed '$d'
    fi
}

verify_pod pod0
if [[ "${POD1_READY}" == "true" ]]; then
    verify_pod pod1
fi

echo
echo "=== Demo Complete ==="
echo "To clean up, run: kubectl delete namespace ${NAMESPACE}"
