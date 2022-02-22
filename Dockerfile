FROM golang:1.17 as builder
WORKDIR /work
COPY api api
COPY cmd cmd
COPY go.mod .
COPY go.sum .
COPY Makefile .
RUN make

FROM krallin/ubuntu-tini as ubuntu-tini

# rethinkdb backup/restore requires the python client library
# let's make small binaries of these commands in order not to blow up the image size
FROM rethinkdb:2.4.0 as rethinkdb-python-client-builder
WORKDIR /work
RUN apt update && apt install -y python3-pip
RUN pip3 install pyinstaller==4.3.0 rethinkdb
COPY build/rethinkdb-dump.spec rethinkdb-dump.spec
COPY build/rethinkdb-restore.spec rethinkdb-restore.spec
RUN pyinstaller rethinkdb-dump.spec \
    && pyinstaller rethinkdb-restore.spec

FROM alpine:3.15
RUN apk add --no-cache tini ca-certificates
COPY --from=builder /work/bin/backup-restore-sidecar /backup-restore-sidecar
COPY --from=ubuntu-tini /usr/local/bin/tini /ubuntu/tini
COPY --from=rethinkdb-python-client-builder /work/dist/rethinkdb-dump /work/dist/rethinkdb-restore /rethinkdb/
CMD ["/backup-restore-sidecar"]
