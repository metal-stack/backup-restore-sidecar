package main

import (
	"fmt"
	"os"
	"path"
	"runtime"

	"github.com/metal-stack/backup-restore-sidecar/pkg/generate/examples/examples"
	appsv1 "k8s.io/api/apps/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

func main() {
	for _, localExample := range []struct {
		db      string
		sts     func(namespace string) *appsv1.StatefulSet
		backing func(namespace string) []client.Object
	}{
		{
			db:      examples.Etcd,
			sts:     examples.EtcdSts,
			backing: examples.EtcdBackingResources,
		},
		{
			db:      examples.Postgres,
			sts:     examples.PostgresSts,
			backing: examples.PostgresBackingResources,
		},
		{
			db:      examples.RethinkDB,
			sts:     examples.RethinkDbSts,
			backing: examples.RethinkDbBackingResources,
		},
		{
			db:      examples.Redis,
			sts:     examples.RedisSts,
			backing: examples.RedisBackingResources,
		},
		{
			db:      examples.KeyDB,
			sts:     examples.KeyDBSts,
			backing: examples.KeyDBBackingResources,
		},
		{
			db:      examples.Valkey,
			sts:     examples.ValkeySts,
			backing: examples.ValkeyBackingResources,
		},
		{
			db:      "valkey-master-replica",
			sts:     examples.ValkeyMasterReplicaSts,
			backing: examples.ValkeyMasterReplicaBackingResources,
		},
		{
			db:      examples.Localfs,
			sts:     examples.LocalfsSts,
			backing: examples.LocalfsBackingResources,
		},
	} {
		err := dumpToExamples(localExample.db+"-local.yaml", append([]client.Object{localExample.sts("default")}, localExample.backing("default")...)...)
		if err != nil {
			panic(err)
		}
	}

	// TODO: add backup provider examples
}

func dumpToExamples(name string, resources ...client.Object) error {
	content := []byte(`# THESE EXAMPLES ARE GENERATED!
# Use them as a template for your deployment, but do not commit manual changes to these files.
---
`)

	for i, r := range resources {
		r.SetNamespace("") // not needed for example manifests

		r := r.DeepCopyObject()

		if sts, ok := r.(*appsv1.StatefulSet); ok {
			// host network is only for integration testing purposes
			sts.Spec.Template.Spec.HostNetwork = false
		}

		raw, err := yaml.Marshal(r)
		if err != nil {
			return err
		}

		if i != len(resources)-1 {
			raw = append(raw, []byte("---\n")...)
		}

		content = append(content, raw...)
	}

	_, filename, _, _ := runtime.Caller(1)

	dest := path.Join(path.Dir(filename), "../../..", "deploy", name)
	fmt.Printf("example manifest written to %s\n", dest)

	err := os.WriteFile(dest, content, 0600)
	if err != nil {
		return err
	}

	return nil
}
