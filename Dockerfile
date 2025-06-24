FROM ghcr.io/metal-stack/rethinkdb-backup-tools-build:v2.4.4-bookworm-slim AS rethinkdb-backup-tools

FROM alpine:3.22
COPY bin/backup-restore-sidecar /backup-restore-sidecar
COPY --from=rethinkdb-backup-tools /rethinkdb-dump /rethinkdb-restore /rethinkdb/
CMD ["/backup-restore-sidecar"]
