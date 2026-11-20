#!/bin/sh
# Live setup for a kind cluster. Run by a person before `hody eval --live`; Hody never runs this.
set -e
kubectl apply -f "$(dirname "$0")/manifest.yaml"
