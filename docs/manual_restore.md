# Manual Restoration

The advantage of the backup-restore-sidecar is that it automatically restores the latest backup automatically in case your data is lost. There can be situations though where you need to restore a specific backup from the past manually. In order to manually restore a specific backup version with the backup-restore-sidecar, use the following steps:

1. Take a copy of your existing stateful set by running `kubectl get sts -o yaml <your-database-sts>`
2. Now, get into a clean state, i.e. delete the existing stateful set and the pvc of your database
3. Deploy the exact stateful set you had but only with the backup-restore-sidecar container and tail some file such that container does not die. This is your "helper" stateful set, which you can use for manual administration. 
   - For postgres check the example [here](../deploy/postgres_manual_restore.yaml)
   - For rethinkdb check the example [here](../deploy/rethinkdb_manual_restore.yaml)
4. Enter the container in your "helper" pod by running `kubectl exec -it <your-database-helper-pod>-0 -c backup-restore-container -- bash`
5. Inside the container, you can view the existing backup versions using `backup-restore-sidecar restore ls`
6. Choose the version to restore by running `backup-restore-sidecar restore <version>`
7. The backup was now restored, you can exit the container and remove the "helper" stateful set `kubectl delete sts <helper-sts-name>` but keep the pvc!
8. Now, deploy the regular backup-restore-sidecar stateful set again. It will find out that all the data is in place and the database will start normally
