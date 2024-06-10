# Manual Restoration

The advantage of the backup-restore-sidecar is that it automatically restores the latest backup automatically in case your data is lost. There can be situations though where you need to restore a specific backup from the past manually. In order to manually restore a specific backup version with the backup-restore-sidecar, use the following steps:

1. Identify to which version of the database you would like to restore. A list of available backups can be listed with the following command:

   ```bash
   k exec -it -c backup-restore-sidecar <your-database-sts> -- backup-restore-sidecar restore ls
   DATE                                    NAME            VERSION
   2024-06-10 07:42:02.562787194 +0000 UTC 7.tar.gz        0
   2024-06-10 07:39:02.431641352 +0000 UTC 6.tar.gz        1
   2024-06-10 07:36:02.467860042 +0000 UTC 5.tar.gz        2
   2024-06-10 07:33:02.428597034 +0000 UTC 4.tar.gz        3
   2024-06-10 07:30:02.545069335 +0000 UTC 3.tar.gz        4
   2024-06-10 07:27:02.491590389 +0000 UTC 2.tar.gz        5
   2024-06-10 07:24:02.480600114 +0000 UTC 1.tar.gz        6
   2024-06-10 07:21:02.43872843 +0000 UTC  0.tar.gz        7
   2024-06-10 07:15:02.448774428 +0000 UTC 10.tar.gz       8
   2024-06-10 07:12:02.45998671 +0000 UTC  9.tar.gz        9
   2024-06-10 07:09:02.442298142 +0000 UTC 8.tar.gz        10
   ```

2. Take a copy of your existing stateful set by running `kubectl get sts -o yaml <your-database-sts>`
3. Now, get into a clean state, i.e. delete the existing stateful set and the pvc of your database
4. Edit the stateful set and replace the `wait` command with the `restore <desired version>` command.

   replace this:

   ```yaml
      spec:
         containers:
         - command:
         - backup-restore-sidecar
         - wait
   ```

   with this:

   ```yaml
      spec:
         containers:
         - command:
         - backup-restore-sidecar
         - restore
         - "<desired version e.g. 0>"
         - --log-level=debug
   ```

   - For postgres check the example [here](../deploy/postgres_manual_restore.yaml)
   - For rethinkdb check the example [here](../deploy/rethinkdb_manual_restore.yaml)

5. The pods in this stateful set get restarted, wait for the `successfully restored <database>` log entry.
6. The backup was now restored, you can exit the container and remove the "helper" stateful set `kubectl delete sts <helper-sts-name>` but keep the pvc!
7. Now, deploy the regular backup-restore-sidecar stateful set again. It will find out that all the data is in place and the database will start normally
