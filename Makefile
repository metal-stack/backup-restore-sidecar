GO111MODULE := on
CGO_ENABLED := 1
LINKMODE := -linkmode external -extldflags '-static -s -w'
DOCKER_TAG := $(or ${GITHUB_TAG_NAME}, latest)

.PHONY: all
all:
	go mod tidy
	go build -tags netgo -ldflags "$(LINKMODE)" -o bin/backup-restore-sidecar github.com/metal-stack/backup-restore-sidecar/cmd
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

.PHONY: start-postgres
start-postgres:
	$(MAKE)	start	DB=postgres

.PHONY: start-rethinkdb
start-rethinkdb:
	$(MAKE)	start	DB=rethinkdb

.PHONY: start
start: kind-cluster-create
	# kubectl apply -f deploy/provider-secret.yaml # make sure to fill in your credentials and backup config!
	kubectl delete -f "deploy/$(DB).yaml" || true # for idempotence
	kubectl apply -f "deploy/$(DB).yaml"
	# tailing
	stern '.*'

.PHONY: kind-cluster-create
kind-cluster-create: dockerimage
	kind create cluster || true
	kind load docker-image metalstack/backup-restore-sidecar:latest
