# Manual Restoration

The advantage of the backup-restore-sidecar is that it automatically restores the latest backup automatically in case your data is lost. There can be situations though where you need to restore a specific backup from the past manually. In order to manually restore a specific backup version with the backup-restore-sidecar, use the following steps:

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
