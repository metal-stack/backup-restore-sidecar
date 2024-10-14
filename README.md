# K8s Backup Restore Sidecar for Databases

This project adds automatic backup and recovery to databases managed by K8s via sidecar.

The idea is taken from the [etcd-backup-restore](https://github.com/gardener/etcd-backup-restore) project.

Probably, it does not make sense to use this project with large databases. However, if it is certain that a database will never grow large, the auto-recovery mechanism can come in very handy.

## Supported Databases

| Database    | Image        | Status | Upgrade Support |
| ----------- | ------------ | :----: | :-------------: |
| postgres    | >= 12-alpine |  beta  |       ✅        |
| rethinkdb   | >= 2.4.0     |  beta  |       ❌        |
| ETCD        | >= 3.5       | alpha  |       ❌        |
| meilisearch | >= 1.2.0     | alpha  |       ✅        |
| redis       | >= 6.0       | alpha  |       ❌        |
| keydb       | >= 6.0       | alpha  |       ❌        |
| localfs     |              | alpha  |       ❌        |

Postgres also supports updates when using the TimescaleDB extension. Please consider the integration test for supported upgrade paths.

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

## Try it out

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
