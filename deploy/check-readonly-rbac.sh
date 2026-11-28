#!/bin/bash
# Checks on a real cluster that deploy/readonly-clusterrole.yaml gives Hody
# exactly the read access it needs and nothing more. Run it against a
# throwaway cluster (CI uses kind):
#
#   kind create cluster && bash deploy/check-readonly-rbac.sh
set -euo pipefail
cd "$(dirname "$0")"

kubectl create namespace shop --dry-run=client -o yaml | kubectl apply -f - >/dev/null
kubectl apply -f readonly-clusterrole.yaml >/dev/null

failed=0
expect() { # expect yes|no <can-i arguments...>
  want=$1; shift
  got=$(kubectl auth can-i "$@" --as student 2>/dev/null || true)
  if [ "$got" = "$want" ]; then
    echo "ok    can-i $* -> $got"
  else
    echo "FAIL  can-i $* -> $got (want $want)"; failed=1
  fi
}

# What Hody reads, in the namespace it was given.
expect yes get pods -n shop
expect yes list pods -n shop
expect yes get pods --subresource=log -n shop
expect yes list events -n shop
expect yes get configmaps -n shop
expect yes list services -n shop
expect yes list resourcequotas -n shop
expect yes get deployments.apps -n shop
expect yes list replicasets.apps -n shop
expect yes list endpointslices.discovery.k8s.io -n shop

# What it must never do.
expect no get secrets -n shop
expect no list secrets -n shop
expect no watch pods -n shop
expect no create pods -n shop
expect no delete pods -n shop
expect no create pods --subresource=exec -n shop
expect no patch deployments.apps -n shop
expect no update configmaps -n shop
expect no delete deployments.apps -n shop

# Nothing outside the namespace it was bound in.
expect no list pods -n default
expect no list pods -n kube-system

exit $failed
