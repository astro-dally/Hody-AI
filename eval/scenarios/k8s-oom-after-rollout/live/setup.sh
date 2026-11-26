#!/bin/sh
# Live setup for a kind cluster. Run by a person before `hody eval --live`; Hody never runs this.
# Applies revision 1, waits, then revision 2, so `what changed` has two ReplicaSets to compare.
set -e
d="$(dirname "$0")"
kubectl apply -f "$d/manifest-1.yaml"
kubectl -n shop rollout status deployment/search --timeout=180s
kubectl apply -f "$d/manifest.yaml"
