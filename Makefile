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
	docker build -t metalpod/backup-restore-sidecar:${DOCKER_TAG} .

.PHONY: dockerpush
dockerpush:
	docker push metalpod/backup-restore-sidecar:${DOCKER_TAG}
