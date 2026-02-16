# Valkey Master-Replica Backup and Restore

This document describes how the backup-restore-sidecar handles backup and restore operations in a Valkey master-replica deployment.

## Problem

In a standalone Valkey deployment backup and restore is straightforward: there is only one instance that owns the data. In a master-replica topology with multiple pods or two problems arise:

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

## How It Works

Each pod in the StatefulSet runs two containers:

1. **`valkey` container**: Runs `backup-restore-sidecar wait` which blocks until the sidecar has finished its initialization (restore if needed). Then it starts the actual Valkey server via `post-exec-cmds`.
2. **`backup-restore-sidecar` container**: Runs the sidecar lifecycle: check if restore is needed restore from backup if the data directory is empty then start periodic backups.

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

### Why not use leader election for backups?

Leader election is intentionally **not** used for backup coordination. The Kubernetes lease can be won by any pod (e.g. pod-1) and once a pod holds the lease it renews it indefinitely. If the elected leader is not the Valkey master and the Valkey master is not the elected leader, no pod would pass both checks and **backups would never happen**. Checking the database role directly avoids this problem entirely.

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
        +-- Wait for leader election (up to 90s)
        |
        +-- Timeout? --> skip restore, sync from master via replication
        |
        +-- Elected as leader:
            |
            +-- Pod ordinal != 0? --> skip restore (won't be master)
            |
            +-- Pod ordinal == 0 --> restore from backup
```

At restore time, Valkey is not running yet, so the sidecar cannot query the database role. Instead it uses Kubernetes leader election to coordinate which pod performs the restore:

1. All pods compete for a `Lease` resource.
2. The winner checks its pod ordinal. Only pod-0 (the future master) actually restores from backup.
3. Pods that lose the election (or time out after 90 seconds) skip restore and start with an empty data directory. Once Valkey starts as a replica it syncs all data from the master via Valkey's built-in replication.

### 90-Second Timeout

The timeout accounts for the worst-case leader election cycle:

| Component | Duration | Reason |
|-----------|----------|--------|
| LeaseDuration | 60s | Worst-case wait for a previous leader's lease to expire |
| RetryPeriod | 5s | Time for a new leader to acquire the expired lease |
| Buffer | 25s | Pod startup time, Kubernetes API latency, clock skew |
| **Total** | **90s** | |

### Leader Election Configuration

The sidecar uses [Kubernetes client-go leader election](https://pkg.go.dev/k8s.io/client-go/tools/leaderelection) with a `Lease` resource:

| Parameter | Value | Purpose |
|-----------|-------|---------|
| LeaseDuration | 60s | How long a lease is valid without renewal |
| RenewDeadline | 15s | How long the leader has to renew before losing the lease |
| RetryPeriod | 5s | How often pods attempt to acquire the lease |

## RBAC

The sidecar needs permissions to manage `Lease` resources for leader election. A `ServiceAccount`, `Role` and `RoleBinding` are required:

```yaml
rules:
- apiGroups: ["coordination.k8s.io"]
  resources: ["leases"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
```

## Configuration

### Sidecar Config (config.yaml)

```yaml
---
db: valkey
valkey-master-replica-mode: true
valkey-statefulset-name: valkey-master-replica

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
| `valkey-master-replica-mode` | Enables master-replica coordination (leader election + role checks) |
| `valkey-statefulset-name` | Used to derive the leader election lease name and headless service DNS |
| `db-data-directory` | Where Valkey stores its data (`dump.rdb`) |
| `backup-provider` | Backup storage backend: `local`, `gcp`, or `s3` |
| `backup-cron-schedule` | How often to take backups (cron expression) |
| `redis-addr` | Address of the local Valkey instance (always `localhost:6379` since the sidecar runs in the same pod) |
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

The script uses the pod's ordinal (extracted from `POD_NAME`) to decide the role. Pod-0 starts as master all others connect to pod-0 via the headless service DNS name.

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

See [deploy/valkey-master-replica-local.yaml](../deploy/valkey-master-replica-local.yaml) for a complete example including the StatefulSet, headless Service, RBAC resources, sidecar config, and init script.

### Required Kubernetes Resources

| Resource | Name | Purpose |
|----------|------|---------|
| StatefulSet | `valkey-master-replica` | 3 replicas with ordered startup |
| Headless Service | `valkey-master-replica` | Stable DNS for pod-to-pod communication |
| ServiceAccount | `valkey-backup-restore` | Identity for RBAC |
| Role | `valkey-backup-restore` | Lease management permissions |
| RoleBinding | `valkey-backup-restore` | Binds Role to ServiceAccount |
| ConfigMap | `backup-restore-sidecar-config-valkey-master-replica` | Sidecar configuration |
| ConfigMap | `valkey-init-script-master-replica` | Master/replica init script |
