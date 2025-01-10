FROM ghcr.io/metal-stack/rethinkdb-backup-tools-build:v2.4.1 as rethinkdb-backup-tools

FROM alpine:3.20
COPY bin/backup-restore-sidecar /backup-restore-sidecar
COPY --from=rethinkdb-backup-tools /rethinkdb-dump /rethinkdb-restore /rethinkdb/
CMD ["/backup-restore-sidecar"]
