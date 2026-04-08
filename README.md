# K8s Backup Restore Sidecar for Databases

This project provides automated backup and recovery for K8s databases.

**Core Design Principle**
This sidecar solution actively controls the database startup sequence. Instead of acting as a passive sidecar, it intercepts the main database container until the latest backup is completely downloaded and restored.
As soon as the restore process is finished, the sidecar signals the database container to start.
After a successful startup, the sidecar continuously runs in the background, performing regular backups according to your defined schedule. [See the sequence diagram in the How it works section](#how-it-works)

The idea is taken from the [etcd-backup-restore](https://github.com/gardener/etcd-backup-restore) project.

Probably, it does not make sense to use this project with large databases. However, if it is certain that a database will never grow large, the auto-recovery mechanism can come in very handy.

## Supported Databases

| Database  | Image        | Status | Upgrade Support |
|-----------|--------------|:------:|:---------------:|
| postgres  | >= 12-alpine |  beta  |        ✅        |
| rethinkdb | >= 2.4.0     |  beta  |        ❌        |
| ETCD      | >= 3.5       | alpha  |        ❌        |
| redis     | >= 6.0       | alpha  |        ❌        |
| keydb     | >= 6.0       | alpha  |        ❌        |
| valkey    | >= 8.1       | alpha  |        ❌        |
| localfs   |              | alpha  |        ❌        |

Postgres also supports updates when using the TimescaleDB extension. Please consider the integration test for supported upgrade paths.

> [!IMPORTANT]
> Upgrade from 12-alpine to 13-alpine is not possible because of library differences in icu-lib.
> The solution is to upgrade to a older 14.10-alpine which has the same icu-lib version as 12-alpine
> and then update to 14.18-alpine or newer which does not require to run pg_upgrade.
> It is also recommended to pin the original database to postgres:12.22-alpine to ensure the latest minor.

## Database Upgrades

### Postgres

Postgres requires special treatment if a major version upgrade is planned. `pg_upgrade` needs to be called with the old and new binaries, also the old data directory and a already initialized data directory which was initialized with the new binary, e.g. `initdb <new directory>`.

To make this process as smooth as possible, backup-restore-sidecar will detect if the version of the database files and the version of the postgres binary. If the binary is newer than the database files it will start the upgrade process. Strict validation to ensure all prerequisites are met is done before actually starting the upgrade process.

To achieve this, `backup-restore-sidecar` saves the postgres binaries in the database directory in the form of `pg-bin-v12` for postgres 12. If later the database version is upgraded, the previous postgres binaries are present for doing the actual upgrade.

## Supported Compression Methods

With `--compression-method` you can define how generated backups are compressed before stored at the storage provider. Available compression methods are:

| compression-method | suffix   | comments                                                                                     |
|--------------------|----------|----------------------------------------------------------------------------------------------|
| tar                | .tar     | no compression, best suited for already compressed content                                   |
| targz              | .tar.gz  | tar and gzip, most commonly used, best compression ratio, average performance                |
| tarlz4             | .tar.lz4 | tar and lz4, very fast compression/decompression speed compared to gz, slightly bigger files |

## Supported Storage Providers

- GCS Buckets
- S3 Buckets (tested against Ceph RADOS gateway)
- Local

## Encryption

For all three storage providers AES encryption is supported and can be enabled with `--encryption-key=<YOUR_KEY>`.
The key must be 32 bytes (AES-256) long.
The backups are stored at the storage provider with the `.aes` suffix. If the file does not have this suffix, decryption is skipped.

## How it works

![Sequence Diagram](docs/sequence.drawio.svg)

## Limitations

- The database is deployed unclustered / standalone
- The database is deployed as a statefulset and the data is backed by a PVC
- No "Point in Time Recovery" (PITR)

## Using Multiple Backup-Restore-Sidecars On a Single Bucket

It is possible to let multiple backup-restore-sidecars (for different databases) use the same backup bucket at an external provider. However, it has to be noted that these sidecars must all configure a dedicated object prefix in which they store the backups. Otherwise they would overwrite each other's data.

Be aware that, if you change the object prefix under which the backups are stored, the old lifecycle policies matching this prefix are not automatically cleaned up and have to be removed manually.

## Deployment
The [deploy](deploy/) directory contains manifests as a reference for deploying the `backup-restore-sidecar`. Because configuration (like environment variables, `ConfigMap` parameters, and `post-exec-cmds`) differs heavily between database engines and storage providers, these generated manifests serve as the best reference for your specific setup. 

To see the backup-restore-sidecar in the wild, you can take a look at our [metal-roles](https://github.com/metal-stack/metal-roles/blob/master/control-plane/roles/postgres-backup-restore/templates/postgres.yaml), which deploys the backup-restore-sidecar for a postgres database in production.

### Startup- and readiness probes
The [manifests](deploy) have a startup probe and a readiness probe configured for the database to avoid restarts of the database during the restore process and to only serve traffic when the database is ready.

* **Startup probe:** Configured with a long timeout threshold (e.g. 1h) to give the initializer enough time to restore a backup before the probe fails and the pod is restarted. This can be necessary, especially for large databases, as the restore process can take a while.
* **Readiness probe:** Configured with a short timeout (e.g. 5s) to quickly detect when the database is ready to serve traffic (after the initial startup probe has succeeded).
* **Liveness probe:** Not recommended for the database container. A liveness probe could incorrectly kill the database during heavy-load operations. It will not interfere with the restore process, as Kubernetes disables liveness probes until the startup probe successfully succeeds.

### Monitoring and Alerting
The sidecar exposes Prometheus metrics on port `2112` at the `/metrics` endpoint. 

Operators should set up Prometheus alerts based on the provided metrics:

* `backup_success` (Gauge): Is `0` when the last backup failed, and `1` when it was successful. This is the primary metric for alerting.
* `backup_errors` (CounterVec): Total number of errors during backups, labeled by operation.
* `backup_total_backups` (Counter): Total number of successful backups.
* `backup_size` (Gauge): Size of the last backup in bytes.

There is no health endpoint for the sidecar, as a failed backup should not affect the availability of the database.
The backup-restore-sidecar is designed to be resilient to backup failures, and it will continue to operate and attempt future backups even if one fails.

## Local Development / Demo Setup

Requires:

- [docker](https://www.docker.com/)
- [kind](https://github.com/kubernetes-sigs/kind)
- [kubectl](https://kubernetes.io/docs/tasks/tools/install-kubectl/)
- [stern](https://github.com/wercker/stern)

To start a demo / devel setup, run: `make start-postgres` or `make start-rethinkdb`.

By default, the backup-restore-sidecar will start with the `local` backup provider, which is probably not very useful for most use-cases. If you want to test storing the data at a real backup provider, then:

1. Configure the backup provider secret in `deploy/provider-secret-<backup-provider>.yaml`.
2. Run `BACKUP_PROVIDER=<backup-provider> make start-postgres` instead.

## Manual restoration

Follow the documentation [here](docs/manual_restore.md) in order to manually restore a specific version of your database.
