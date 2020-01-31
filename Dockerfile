FROM golang:1.13 as builder
WORKDIR /work
COPY api api
COPY cmd cmd
COPY go.mod .
COPY go.sum .
COPY Makefile .
RUN make

FROM alpine:3.11
COPY --from=builder /work/bin/backup-restore-sidecar /backup-restore-sidecar
RUN apk add --no-cache tini
USER nobody
CMD ["/backup-restore-sidecar"]