# K8s Backup Restore Sidecar for Databases

![Go version](https://img.shields.io/github/go-mod/go-version/metal-stack/backup-restore-sidecar)
[![Go Report Card](https://goreportcard.com/badge/github.com/metal-stack/backup-restore-sidecar)](https://goreportcard.com/report/github.com/metal-stack/backup-restore-sidecar)
[![go.dev reference](https://img.shields.io/badge/go.dev-reference-007d9c?logo=go&logoColor=white&style=flat-square)](https://pkg.go.dev/github.com/metal-stack/backup-restore-sidecar)
[![Build](https://github.com/metal-stack/backup-restore-sidecar/actions/workflows/docker.yaml/badge.svg?branch=master)](https://github.com/metal-stack/backup-restore-sidecar/actions)
[![Slack](https://img.shields.io/badge/slack-metal--stack-brightgreen.svg?logo=slack)](https://metal-stack.slack.com/)

This project adds automatic backup and recovery to databases managed by K8s via sidecar.

The idea is taken from the [etcd-backup-restore](https://github.com/gardener/etcd-backup-restore) project.

Probably, it does not make sense to use this project with large databases. However, if it is certain that a database will never grow large, the auto-recovery mechanism can come in very handy.

## Supported Databases

| Database  | Image        | Status | Upgrade Support |
| --------- | ------------ | :----: | :-------------: |
| postgres  | >= 12-alpine |  beta  |       ✅        |
| rethinkdb | >= 2.4.0     |  beta  |       ❌        |
| ETCD      | >= 3.5       | alpha  |       ❌        |
| redis     | >= 6.0       | alpha  |       ❌        |
| keydb     | >= 6.0       | alpha  |       ❌        |
| valkey    | >= 8.1       | alpha  |       ❌        |
| localfs   |              | alpha  |       ❌        |

Postgres also supports updates when using the TimescaleDB extension. Please consider the integration test for supported upgrade paths.

> [!IMPORTANT]
> Upgrade from 12-alpine to 13-alpine is not possible because of library differences in icu-lib.
> The solution is to upgrade to a older 14.10-alpine which has the same icu-lib version as 12-alpine
> and then update to 14.18-alpine or newer which does not require to run pg_upgrade.
> It is also recommended to pin the original database to postgres:12.22-alpine to ensure the latest minor.
> Upgrade from 14.18-alpine to 15-alpine is not possible because of version differences in ICU.
> The solution is to upgrade to 15.13-alpine, followed by 15.18-alpine before upgrading to 17.10-alpine.

## Database Upgrades

### Postgres

Postgres requires special treatment if a major version upgrade is planned. `pg_upgrade` needs to be called with the old and new binaries, also the old data directory and a already initialized data directory which was initialized with the new binary, e.g. `initdb <new directory>`.

To make this process as smooth as possible, backup-restore-sidecar will detect if the version of the database files and the version of the postgres binary. If the binary is newer than the database files it will start the upgrade process. Strict validation to ensure all prerequisites are met is done before actually starting the upgrade process.

To achieve this, `backup-restore-sidecar` saves the postgres binaries in the database directory in the form of `pg-bin-v12` for postgres 12. If later the database version is upgraded, the previous postgres binaries are present for doing the actual upgrade.

## Supported Compression Methods

With `--compression-method` you can define how generated backups are compressed before stored at the storage provider. Available compression methods are:

| compression-method | suffix   | comments                                                                                     |
| ------------------ | -------- | -------------------------------------------------------------------------------------------- |
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

In a recovery scenario, control plane state can be restored from regular backups taken by the `backup-restore-sidecar` component to S3-compatible object storage. On startup, the affected database automatically restores from the referenced backup without manual intervention. The process is illustrated in the following diagram:

![Sequence Diagram](docs/sequence.drawio.svg)

## Limitations

- The database is deployed unclustered / standalone
- The database is deployed as a statefulset and the data is backed by a PVC
- No "Point in Time Recovery" (PITR)

## Using Multiple Backup-Restore-Sidecars On a Single Bucket

It is possible to let multiple backup-restore-sidecars (for different databases) use the same backup bucket at an external provider. However, it has to be noted that these sidecars must all configure a dedicated object prefix in which they store the backups. Otherwise they would overwrite each other's data.

Be aware that, if you change the object prefix under which the backups are stored, the old lifecycle policies matching this prefix are not automatically cleaned up and have to be removed manually.

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

The advantage of the `backup-restore-sidecar` is that it automatically restores the latest backup automatically in case your data is lost. There can be situations though where you need to restore a specific backup from the past manually. In order to manually restore a specific backup version with the `backup-restore-sidecar`, use the following steps:

Take a copy of your existing stateful set by running:

```bash
kubectl get sts -o yaml <your-database-sts>
```

Now, get into a clean state, i.e. delete the existing stateful set and the pvc of your database
Deploy the exact stateful set you had but only with the backup-restore-sidecar container and tail some file such that container does not die. This is your "helper" stateful set, which you can use for manual administration.

- For postgres check the example [here](https://github.com/metal-stack/backup-restore-sidecar/blob/master/deploy/postgres_manual_restore.yaml)
- For rethinkdb check the example [here](https://github.com/metal-stack/backup-restore-sidecar/blob/master/deploy/rethinkdb_manual_restore.yaml)

Enter the container in your "helper" pod by running:

```bash
kubectl exec -it <your-database-helper-pod>-0 -c backup-restore-sidecar -- bash
```

Inside the container, you can view the existing backup versions using

```bash
backup-restore-sidecar restore ls
```

Choose the version to restore by running

```bash
backup-restore-sidecar restore <version>
```

The backup was now restored, you can exit the container and remove the "helper" stateful set but keep the pvc!

```bash
kubectl delete sts <helper-sts-name>
```

Now, deploy the regular backup-restore-sidecar stateful set again. It will find out that all the data is in place and the database will start normally
