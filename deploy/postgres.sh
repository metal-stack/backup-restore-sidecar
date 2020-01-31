#!/usr/bin/env bash
set -eo pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" > /dev/null 2>&1 && pwd )"
cd $SCRIPT_DIR

# build image
make --directory $SCRIPT_DIR/.. dockerimage

# prepare dev env
kind create cluster || true
kind load docker-image metalpod/backup-restore-sidecar:latest

# on first execution: 
# kubectl apply -f provider-secret.yaml # make sure to fill in your credentials and backup config!

# deploy sidecar
kubectl delete -f postgres.yaml || true # for idempotence
kubectl apply -f postgres.yaml

# tailing the output
stern '.*'
