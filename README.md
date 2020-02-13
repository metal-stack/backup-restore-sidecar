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
- Local

## How it works

![Sequence Diagram](docs/sequence.png)

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

1. Configure your backup provider configuration in `deploy/provider-secret.yaml`
2. Enable deployment of the provider secret by commenting in the `kubectl` command in the Makefile's `start` target
3. Run `make start-postgres` or `start-rethinkdb`

## Manual restoration

Follow the documentation [here](docs/manual_restore.md) in order to manually restore a specific version of your database.
