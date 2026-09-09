# Valkey Master-Replica Backup and Restore

This document describes how the backup-restore-sidecar handles backup and restore operations in a Valkey master-replica deployment.

## Problem

In a standalone Valkey deployment backup and restore is straightforward: there is only one instance that owns the data. In a master-replica topology with multiple pods two problems arise:

1. **Backup**: Only the master should create backups. Replicas have the same data via replication but backing up from a replica could produce inconsistent snapshots.
2. **Restore**: If all pods restore from backup simultaneously on startup replicas will load the backup data and then also receive replicated data from the master leading to an uncontrolled database state.

## Topology

This is **not** a Valkey Cluster (which requires `cluster-enabled yes` and has built-in sharding/failover). Instead it is a simple master-replica setup using a Kubernetes StatefulSet:

- **Pod-0** is always the Valkey master (determined by an init script based on the pod ordinal).
- **Pod-1, Pod-2, ...** are read-only replicas that connect to pod-0 via `--replicaof`.
- Replicas cannot promote themselves to master. If the master pod is deleted Kubernetes recreates pod-0 and it starts as master again.
- A **headless Service** (ClusterIP: None) provides stable DNS names for each pod which replicas use to find the master:
  ```
  <statefulset-name>-0.<service-name>.<namespace>.svc.cluster.local
  ```

### Why not Valkey Cluster mode?

Valkey Cluster (`cluster-enabled yes`) was evaluated. It would provide automatic failover (and thus zero-downtime for writes during rolling updates), but several issues make it impractical for this use case:

- **Cluster formation**: A fresh deploy requires active cluster formation (`CLUSTER MEET`, `CLUSTER ADDSLOTS` for 16384 hash slots). This is not in the sidecar's scope and would need a separate mechanism (operator, init job). Master-replica only needs `--replicaof` in an init script.
- **`nodes.conf` and pod IPs**: Valkey Cluster stores its topology with IP addresses in `nodes.conf`. Kubernetes pod IPs change on every restart. This requires `cluster-announce-hostname` and backing up/restoring `nodes.conf` alongside `dump.rdb`. Master-replica has no such state file.
- **Restore complexity**: A full restore requires all nodes to restore their shard AND the cluster to reform afterwards. With master-replica only pod-0 restores and replicas sync via replication.
- **Sharding provides no benefit here**: The dataset is small. Sharding across multiple masters solves no problem we have. The only advantage of cluster mode would be automatic failover, which could also be achieved with Sentinel as a lighter-weight alternative.

## How It Works

Each pod in the StatefulSet runs two containers:

1. **`valkey` container**: Runs `backup-restore-sidecar wait` which blocks until the sidecar has finished its initialization (restore if needed). Then it starts the actual Valkey server via `post-exec-cmds`.
2. **`backup-restore-sidecar` container**: Runs the sidecar lifecycle: check if restore is needed, restore from backup if the data directory is empty, then start periodic backups.

The containers coordinate via a gRPC server (port 8000) that the sidecar exposes. The `wait` command polls this server every 3 seconds until it reports `DONE` ensuring Valkey never starts before a restore completes.

## Backup

```
Cron trigger (e.g. every minute)
    |
    v
ShouldPerformBackup()
    |
    +-- Not master-replica mode? --> always proceed
    |
    +-- Master-replica mode:
        |
        +-- Query Valkey: INFO replication
        |
        +-- role != master? --> skip
        |
        +-- role == master --> proceed with backup
```

Only the Valkey master (pod-0) takes backups. The sidecar determines this by querying the local Valkey instance via `INFO replication` and checking for `role:master`.

The backup pipeline is: dump (`SAVE`) -> compress (tar.gz) -> encrypt (AES) -> upload to backup provider.

The dump (`dump.rdb`) remains in the data directory after the upload. Together with Valkey's automatic snapshotting it serves as the local recovery point: a pod that restarts with an intact volume resumes from this local data, and a restore from backup is only needed when the volume is actually lost.

Backups can also be triggered manually via the gRPC API (`backup-restore-sidecar create-backup`). The role check is enforced in both paths — a manual backup request on a replica fails with an explicit error instead of silently uploading a replica's state.

## Restore

```
Pod startup (data directory empty)
    |
    v
Recover()
    |
    +-- Not master-replica mode? --> restore immediately
    |
    +-- Master-replica mode:
        |
        +-- Extract ordinal from POD_NAME
        |
        +-- ordinal != 0? --> skip restore (will sync from master via replication)
        |
        +-- ordinal == 0 --> restore from backup
```

At restore time Valkey is not running yet, so the sidecar cannot query the database role. Instead it uses the pod ordinal (extracted from `POD_NAME`) to determine whether to restore:

1. Pod-0 (ordinal 0) is the future master and restores from backup.
2. All other pods skip restore and start with an empty data directory. Once Valkey starts as a replica it syncs all data from the master via Valkey's built-in replication.

The ordinal is deterministic- It is derived from the StatefulSet pod name which Kubernetes guarantees. No distributed coordination or Kubernetes API access is needed.

## Configuration

### Sidecar Config (config.yaml)

```yaml
---
db: valkey
valkey-master-replica-mode: true

bind-addr: 0.0.0.0
db-data-directory: /data/
backup-provider: local
backup-cron-schedule: "*/1 * * * *"
redis-addr: localhost:6379
encryption-key: "01234567891234560123456789123456"
post-exec-cmds:
  - /usr/local/bin/init.sh
```

| Key | Description |
|-----|-------------|
| `db` | Database type, must be `valkey` |
| `valkey-master-replica-mode` | Enables master-replica coordination (ordinal-based restore + role checks for backup) |
| `db-data-directory` | Where Valkey stores its data (`dump.rdb`) |
| `backup-provider` | Backup storage backend: `local`, `gcp`, or `s3` |
| `backup-cron-schedule` | How often to take backups (cron expression) |
| `redis-addr` | Address of the local Valkey instance (always `localhost:6379` since the sidecar runs in the same pod). Named `redis-addr` because the underlying Go client uses the Redis protocol. |
| `encryption-key` | 32-byte AES key for encrypting backups |
| `post-exec-cmds` | Command to start Valkey after the sidecar finishes initialization |

### Init Script

The init script is mounted as a ConfigMap and determines whether the pod starts as master or replica:

```sh
#!/bin/sh
set -e

ORDINAL="${POD_NAME##*-}"
STATEFULSET_NAME="${POD_NAME%-*}"

if [ "$ORDINAL" -eq 0 ]; then
  echo "Starting as master (pod-0)"
  exec valkey-server --port 6379 --bind 0.0.0.0 --dir /data
else
  MASTER_ADDR="${STATEFULSET_NAME}-0.${STATEFULSET_NAME}.${POD_NAMESPACE}.svc.cluster.local"
  echo "Starting as replica (pod-$ORDINAL), master: $MASTER_ADDR"
  exec valkey-server --port 6379 --bind 0.0.0.0 --dir /data --replicaof "$MASTER_ADDR" 6379
fi
```

The script uses the pod's ordinal (extracted from `POD_NAME`) to decide the role. Pod-0 starts as master, all others connect to pod-0 via the headless service DNS name.

Valkey's automatic RDB snapshotting (default save points) stays enabled on purpose. It complements the sidecar's backups rather than competing with them:

- **Automatic snapshots** persist the dataset to the pod's volume. A pod restart with an intact volume resumes from this local data, and on a graceful shutdown (SIGTERM) Valkey writes a final snapshot, which makes rolling restarts lossless.
- **Sidecar backups** protect against losing the volume itself.

Both write `dump.rdb` atomically (temporary file + rename), so they cannot corrupt each other.

### Required Environment Variables

These must be set on both containers via the Kubernetes downward API:

```yaml
- name: POD_NAMESPACE
  valueFrom:
    fieldRef:
      fieldPath: metadata.namespace
- name: POD_NAME
  valueFrom:
    fieldRef:
      fieldPath: metadata.name
```

## Deployment

See [deploy/valkey-master-replica-local.yaml](../deploy/valkey-master-replica-local.yaml) for a complete example including the StatefulSet, headless Service, sidecar config, and init script.

### Required Kubernetes Resources

| Resource | Name | Purpose |
|----------|------|---------|
| StatefulSet | `valkey-master-replica` | 3 replicas with ordered startup |
| Headless Service | `valkey-master-replica` | Stable DNS for pod-to-pod communication |
| ConfigMap | `backup-restore-sidecar-config-valkey-master-replica` | Sidecar configuration |
| ConfigMap | `valkey-init-script-master-replica` | Master/replica init script |

## Failure Behavior

| Scenario | What happens |
|----------|-------------|
| **Pod-0 (master) is deleted** | Kubernetes recreates pod-0. If the data PVC survives, Valkey resumes from the local `dump.rdb` (on graceful shutdown Valkey writes a final snapshot, so no data is lost) and replicas reconnect. If the PVC is lost, the sidecar detects an empty data directory and restores from the latest backup. |
| **A replica pod is deleted** | Kubernetes recreates it. The sidecar skips restore (ordinal != 0) and Valkey syncs all data from the master via replication. |
| **Master pod is temporarily unreachable** | Replicas log connection errors and retry. There is no automatic promotion — writes are unavailable until pod-0 returns. Reads from replicas continue working. |
| **All pods are deleted (e.g. StatefulSet delete + recreate)** | Pod-0 starts first (ordered startup). With an intact volume it resumes from local data, otherwise it restores from the latest backup. Replicas start after pod-0 is ready and sync via replication. |

### Recovery Point

How much data can be lost depends on the failure:

- **Graceful restart, volume intact**: no data loss. Valkey writes a final snapshot on shutdown.
- **Hard crash, volume intact**: writes since the last snapshot are lost. A snapshot is written at every sidecar backup (`SAVE`) and at Valkey's own save points, so the loss is bounded by the `backup-cron-schedule` interval.
- **Volume lost**: the master restores the latest backup, so writes since that backup are lost. Replicas always resync from the master, data that only lived in replication after that backup is overwritten by the restored state.
