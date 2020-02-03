#!/usr/bin/env bash
set -eo pipefail

# build image
make --directory .. dockerimage

# prepare dev env
kind create cluster || true
kind load docker-image metalpod/backup-restore-sidecar:latest

# on first execution: 
# kubectl apply -f provider-secret.yaml # make sure to fill in your credentials and backup config!

# deploy sidecar
kubectl delete -f "${1}.yaml" || true # for idempotence
kubectl apply -f "${1}.yaml"

# tailing the output
stern '.*'
