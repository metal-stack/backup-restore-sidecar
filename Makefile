GO111MODULE := on
CGO_ENABLED := 1
LINKMODE := -extldflags '-static -s -w'
DOCKER_TAG := $(or ${GITHUB_TAG_NAME},latest)
BACKUP_PROVIDER := $(or ${BACKUP_PROVIDER},local)

.PHONY: all
all:
	go mod tidy
	go build -ldflags "$(LINKMODE)" -tags 'osusergo netgo static_build' -o bin/backup-restore-sidecar github.com/metal-stack/backup-restore-sidecar/cmd
	strip bin/backup-restore-sidecar

.PHONY: proto
proto:
	docker pull metalstack/builder
	docker run -it --rm --user $$(id -u):$$(id -g) -v ${PWD}/api:/work/api metalstack/builder protoc -I api/ api/v1/*.proto --go_out=plugins=grpc:api

.PHONY: dockerimage
dockerimage:
	docker build -t ghcr.io/metal-stack/backup-restore-sidecar:${DOCKER_TAG} .

.PHONY: dockerpush
dockerpush:
	docker push ghcr.io/metal-stack/backup-restore-sidecar:${DOCKER_TAG}

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

.PHONY: start
start: kind-cluster-create
	kind --name backup-restore-sidecar load docker-image ghcr.io/metal-stack/backup-restore-sidecar:latest
ifneq ($(BACKUP_PROVIDER),local)
	# if you want to use other providers, please fill in your credentials and backup config!
	# for this, you need to edit deploy/provider-secret-$(BACKUP_PROVIDER)
	# take care not to push your provider secrets to origin
	kubectl --kubeconfig $(KUBECONFIG) apply -f deploy/provider-secret-$(BACKUP_PROVIDER).yaml
endif
	kubectl --kubeconfig $(KUBECONFIG) delete -f "deploy/$(DB)-$(BACKUP_PROVIDER).yaml" || true # for idempotence
	kubectl --kubeconfig $(KUBECONFIG) apply -f "deploy/$(DB)-$(BACKUP_PROVIDER).yaml"
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
