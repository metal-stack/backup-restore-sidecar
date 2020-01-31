# K8s Backup Restore Sidecar for Databases

This project adds automatic backup and recovery to databases managed by K8s via sidecar.

The idea is taken from the [etcd-backup-restore](https://github.com/gardener/etcd-backup-restore) project.

Probably, it does not make sense to use this project with large databases. However, if it is certain that a database will never grow large, the auto-recovery mechanism can come in very handy.

## Supported Databases

| Database  | Image     | Status |
| --------- | --------- | ------ |
| postgres  | 12-alpine | alpha  |
| rethinkdb | 2.4.0     | alpha  |

## Supported Storage Providers

- GCS Buckets

## Deployment
