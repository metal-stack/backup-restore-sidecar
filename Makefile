GO111MODULE := on
CGO_ENABLED := 1
LINKMODE := -extldflags '-static -s -w'
DOCKER_TAG := $(or ${GITHUB_TAG_NAME}, latest)

.PHONY: all
all:
	go mod tidy
	go build -ldflags "$(LINKMODE)" -tags 'osusergo netgo static_build' -o bin/backup-restore-sidecar github.com/metal-stack/backup-restore-sidecar/cmd
	strip bin/backup-restore-sidecar

.PHONY: proto
proto:
	docker run -it --rm -v ${PWD}/api:/work/api metalstack/builder protoc -I api/ api/v1/*.proto --go_out=plugins=grpc:api

.PHONY: dockerimage
dockerimage:
	docker build -t metalstack/backup-restore-sidecar:${DOCKER_TAG} .

.PHONY: dockerpush
dockerpush:
	docker push metalstack/backup-restore-sidecar:${DOCKER_TAG}

# # #
# the following tasks can be used to set up a development environment
# # #

KUBECONFIG := $(shell pwd)/.kubeconfig

.PHONY: start-postgres
start-postgres:
	$(MAKE)	start	DB=postgres

.PHONY: start-rethinkdb
start-rethinkdb:
	$(MAKE)	start	DB=rethinkdb

.PHONY: start-etcd
start-etcd:
	$(MAKE)	start	DB=etcd

.PHONY: start
start: kind-cluster-create
	kind --name backup-restore-sidecar load docker-image metalstack/backup-restore-sidecar:latest
	# kubectl --kubeconfig $(KUBECONFIG) apply -f deploy/provider-secret.yaml # make sure to fill in your credentials and backup config!
	kubectl --kubeconfig $(KUBECONFIG) delete -f "deploy/$(DB).yaml" || true # for idempotence
	kubectl --kubeconfig $(KUBECONFIG) apply -f "deploy/$(DB).yaml"
	# tailing
	stern --kubeconfig $(KUBECONFIG) '.*'

.PHONY: kind-cluster-create
kind-cluster-create: dockerimage
	@if ! which kind > /dev/null; then echo "kind needs to be installed"; exit 1; fi
	@if ! kind get clusters | grep backup-restore-sidecar > /dev/null; then \
		kind create cluster \
		--name backup-restore-sidecar \
		--kubeconfig $(KUBECONFIG); fi

.PHONY: cleanup
cleanup:
	kind delete cluster --name backup-restore-sidecar
	rm -f $(KUBECONFIG)

.PHONY: dev-env
dev-env:
	@echo "export KUBECONFIG=$(KUBECONFIG)"
