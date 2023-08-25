# TODO: remove tini in a future release, not required anymore since pre- and post-exec-cmd flags
FROM krallin/ubuntu-tini as ubuntu-tini

FROM ghcr.io/metal-stack/rethinkdb-backup-tools-build:v2.4.1 as rethinkdb-backup-tools

FROM alpine:3.18
# TODO: remove tini in a future release, not required anymore since pre- and post-exec-cmd flags
RUN apk add --no-cache tini ca-certificates
COPY bin/backup-restore-sidecar /backup-restore-sidecar
COPY --from=ubuntu-tini /usr/local/bin/tini-static /ubuntu/tini
COPY --from=rethinkdb-backup-tools /rethinkdb-dump /rethinkdb-restore /rethinkdb/
CMD ["/backup-restore-sidecar"]
