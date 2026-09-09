//go:build integration

package integration_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/avast/retry-go/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/metal-stack/backup-restore-sidecar/pkg/generate/examples/examples"
)

const (
	valkeyMasterReplicaStsName = "valkey-master-replica"
	valkeyContainer            = "valkey"
	sidecarContainer           = "backup-restore-sidecar"
)

func Test_Valkey_MasterReplica_Restore(t *testing.T) {
	var (
		ctx, cancel = context.WithTimeout(t.Context(), 20*time.Minute)
		ns          = testNamespace(t)
	)
	defer cancel()

	cleanup := func() {
		t.Log("running cleanup")
		err := c.Delete(ctx, ns)
		require.NoError(t, client.IgnoreNotFound(err), "cleanup did not succeed")
		err = waitUntilNotFound(ctx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: ns.Name},
		})
		require.NoError(t, err, "cleanup did not succeed")
	}
	cleanup()
	defer cleanup()

	err := c.Create(ctx, ns)
	require.NoError(t, client.IgnoreAlreadyExists(err))

	t.Log("applying resource manifests")

	sts := examples.ValkeyMasterReplicaSts(ns.Name)
	objects := []client.Object{sts}
	objects = append(objects, examples.ValkeyMasterReplicaBackingResources(ns.Name)...)

	for _, o := range objects {
		err = c.Create(ctx, o)
		require.NoError(t, err)
	}

	t.Log("waiting for all pods to be running")

	for i := int32(0); i < *sts.Spec.Replicas; i++ {
		podName := fmt.Sprintf("%s-%d", valkeyMasterReplicaStsName, i)
		err = waitForPodRunning(ctx, podName, ns.Name)
		require.NoError(t, err, "pod %s did not become ready", podName)
	}

	masterPod := valkeyMasterReplicaStsName + "-0"
	replicaPod := valkeyMasterReplicaStsName + "-1"

	t.Log("writing test data to master")

	_, err = execCommand(ctx, masterPod, ns.Name, valkeyContainer, []string{
		"valkey-cli", "SET", "test-key", "I am precious master-replica data",
	})
	require.NoError(t, err)

	t.Log("verifying replication to replica pod")

	err = retry.Do(func() error {
		resp, err := execCommand(ctx, replicaPod, ns.Name, valkeyContainer, []string{
			"valkey-cli", "GET", "test-key",
		})
		if err != nil {
			return err
		}
		if !strings.Contains(resp, "I am precious master-replica data") {
			return fmt.Errorf("data not yet replicated, got: %s", resp)
		}
		return nil
	}, retry.Context(ctx), retry.Attempts(30), retry.Delay(2*time.Second))
	require.NoError(t, err, "data was not replicated to replica pod")

	t.Log("triggering backup on master's sidecar")

	_, err = execCommand(ctx, masterPod, ns.Name, sidecarContainer, []string{
		"sh", "-c",
		"BACKUP_RESTORE_SIDECAR_INITIALIZER_ENDPOINT=http://127.0.0.1:8000/ backup-restore-sidecar create-backup",
	})
	require.NoError(t, err)

	t.Log("verifying backup was created")

	err = retry.Do(func() error {
		resp, err := execCommand(ctx, masterPod, ns.Name, sidecarContainer, []string{
			"sh", "-c", "ls /backup/local-provider/",
		})
		if err != nil {
			return err
		}
		if !strings.Contains(resp, ".aes") {
			return fmt.Errorf("no encrypted backup found, got: %s", resp)
		}
		return nil
	}, retry.Context(ctx), retry.Attempts(30), retry.Delay(2*time.Second))
	require.NoError(t, err, "backup was not created")

	t.Log("deleting statefulset and data PVCs")

	err = c.Delete(ctx, examples.ValkeyMasterReplicaSts(ns.Name))
	require.NoError(t, err)

	for i := int32(0); i < *sts.Spec.Replicas; i++ {
		pvcName := fmt.Sprintf("data-%s-%d", valkeyMasterReplicaStsName, i)
		err = c.Delete(ctx, &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      pvcName,
				Namespace: ns.Name,
			},
		})
		require.NoError(t, err, "failed to delete PVC %s", pvcName)
	}

	for i := int32(0); i < *sts.Spec.Replicas; i++ {
		podName := fmt.Sprintf("%s-%d", valkeyMasterReplicaStsName, i)
		err = waitUntilNotFound(ctx, &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName,
				Namespace: ns.Name,
			},
		})
		require.NoError(t, err, "pod %s did not get deleted", podName)
	}

	t.Log("recreating statefulset")

	err = c.Create(ctx, examples.ValkeyMasterReplicaSts(ns.Name))
	require.NoError(t, err)

	t.Log("waiting for all pods to be running after restore")

	for i := int32(0); i < *sts.Spec.Replicas; i++ {
		podName := fmt.Sprintf("%s-%d", valkeyMasterReplicaStsName, i)
		err = waitForPodRunning(ctx, podName, ns.Name)
		require.NoError(t, err, "pod %s did not become ready after restore", podName)
	}

	t.Log("verifying data was restored on master")

	err = retry.Do(func() error {
		resp, err := execCommand(ctx, masterPod, ns.Name, valkeyContainer, []string{
			"valkey-cli", "GET", "test-key",
		})
		if err != nil {
			return err
		}
		if !strings.Contains(resp, "I am precious master-replica data") {
			return fmt.Errorf("data not restored on master, got: %s", resp)
		}
		return nil
	}, retry.Context(ctx), retry.Attempts(30), retry.Delay(2*time.Second))
	require.NoError(t, err, "data was not restored on master")

	t.Log("verifying data was replicated to replica after restore")

	err = retry.Do(func() error {
		resp, err := execCommand(ctx, replicaPod, ns.Name, valkeyContainer, []string{
			"valkey-cli", "GET", "test-key",
		})
		if err != nil {
			return err
		}
		if !strings.Contains(resp, "I am precious master-replica data") {
			return fmt.Errorf("data not replicated to replica after restore, got: %s", resp)
		}
		return nil
	}, retry.Context(ctx), retry.Attempts(30), retry.Delay(2*time.Second))
	require.NoError(t, err, "data was not replicated to replica after restore")
}

func Test_Valkey_MasterReplica_RestoreLatestFromMultipleBackups(t *testing.T) {
	var (
		ctx, cancel = context.WithTimeout(t.Context(), 20*time.Minute)
		ns          = testNamespace(t)
	)
	defer cancel()

	cleanup := func() {
		t.Log("running cleanup")
		err := c.Delete(ctx, ns)
		require.NoError(t, client.IgnoreNotFound(err), "cleanup did not succeed")
		err = waitUntilNotFound(ctx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: ns.Name},
		})
		require.NoError(t, err, "cleanup did not succeed")
	}
	cleanup()
	defer cleanup()

	err := c.Create(ctx, ns)
	require.NoError(t, client.IgnoreAlreadyExists(err))

	t.Log("applying resource manifests")

	sts := examples.ValkeyMasterReplicaSts(ns.Name)
	objects := []client.Object{sts}
	objects = append(objects, examples.ValkeyMasterReplicaBackingResources(ns.Name)...)

	for _, o := range objects {
		err = c.Create(ctx, o)
		require.NoError(t, err)
	}

	t.Log("waiting for all pods to be running")

	for i := int32(0); i < *sts.Spec.Replicas; i++ {
		podName := fmt.Sprintf("%s-%d", valkeyMasterReplicaStsName, i)
		err = waitForPodRunning(ctx, podName, ns.Name)
		require.NoError(t, err, "pod %s did not become ready", podName)
	}

	masterPod := valkeyMasterReplicaStsName + "-0"
	replicaPod := valkeyMasterReplicaStsName + "-1"

	t.Log("adding multiple test data entries and taking backups")

	lastIndex := 0
	for i := range 10 {
		t.Logf("adding test data index=%d", i)

		_, err = execCommand(ctx, masterPod, ns.Name, valkeyContainer, []string{
			"valkey-cli", "SET", fmt.Sprintf("valkey-mr-%d", i), fmt.Sprintf("valkey-mr-idx-%d", i),
		})
		require.NoError(t, err)

		t.Log("taking a backup")

		_, err = execCommand(ctx, masterPod, ns.Name, sidecarContainer, []string{
			"sh", "-c",
			"BACKUP_RESTORE_SIDECAR_INITIALIZER_ENDPOINT=http://127.0.0.1:8000/ backup-restore-sidecar create-backup",
		})
		require.NoError(t, err)

		lastIndex = i
	}

	t.Log("verifying backup was created")

	err = retry.Do(func() error {
		resp, err := execCommand(ctx, masterPod, ns.Name, sidecarContainer, []string{
			"sh", "-c", "ls /backup/local-provider/",
		})
		if err != nil {
			return err
		}
		if !strings.Contains(resp, ".aes") {
			return fmt.Errorf("no encrypted backup found, got: %s", resp)
		}
		return nil
	}, retry.Context(ctx), retry.Attempts(30), retry.Delay(2*time.Second))
	require.NoError(t, err, "backup was not created")

	t.Log("deleting statefulset and data PVCs")

	err = c.Delete(ctx, examples.ValkeyMasterReplicaSts(ns.Name))
	require.NoError(t, err)

	for i := int32(0); i < *sts.Spec.Replicas; i++ {
		pvcName := fmt.Sprintf("data-%s-%d", valkeyMasterReplicaStsName, i)
		err = c.Delete(ctx, &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      pvcName,
				Namespace: ns.Name,
			},
		})
		require.NoError(t, err, "failed to delete PVC %s", pvcName)
	}

	for i := int32(0); i < *sts.Spec.Replicas; i++ {
		podName := fmt.Sprintf("%s-%d", valkeyMasterReplicaStsName, i)
		err = waitUntilNotFound(ctx, &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName,
				Namespace: ns.Name,
			},
		})
		require.NoError(t, err, "pod %s did not get deleted", podName)
	}

	t.Log("recreating statefulset")

	err = c.Create(ctx, examples.ValkeyMasterReplicaSts(ns.Name))
	require.NoError(t, err)

	t.Log("waiting for all pods to be running after restore")

	for i := int32(0); i < *sts.Spec.Replicas; i++ {
		podName := fmt.Sprintf("%s-%d", valkeyMasterReplicaStsName, i)
		err = waitForPodRunning(ctx, podName, ns.Name)
		require.NoError(t, err, "pod %s did not become ready after restore", podName)
	}

	t.Log("verifying latest data was restored on master")

	err = retry.Do(func() error {
		resp, err := execCommand(ctx, masterPod, ns.Name, valkeyContainer, []string{
			"valkey-cli", "GET", fmt.Sprintf("valkey-mr-%d", lastIndex),
		})
		if err != nil {
			return err
		}
		expected := fmt.Sprintf("valkey-mr-idx-%d", lastIndex)
		if !strings.Contains(resp, expected) {
			return fmt.Errorf("latest data not restored on master, got: %s, expected: %s", resp, expected)
		}
		return nil
	}, retry.Context(ctx), retry.Attempts(30), retry.Delay(2*time.Second))
	require.NoError(t, err, "latest data was not restored on master")

	t.Log("verifying latest data was replicated to replica after restore")

	err = retry.Do(func() error {
		resp, err := execCommand(ctx, replicaPod, ns.Name, valkeyContainer, []string{
			"valkey-cli", "GET", fmt.Sprintf("valkey-mr-%d", lastIndex),
		})
		if err != nil {
			return err
		}
		expected := fmt.Sprintf("valkey-mr-idx-%d", lastIndex)
		if !strings.Contains(resp, expected) {
			return fmt.Errorf("latest data not replicated to replica after restore, got: %s, expected: %s", resp, expected)
		}
		return nil
	}, retry.Context(ctx), retry.Attempts(30), retry.Delay(2*time.Second))
	require.NoError(t, err, "latest data was not replicated to replica after restore")

	t.Log("verifying data on master with assert")

	resp, err := execCommand(ctx, masterPod, ns.Name, valkeyContainer, []string{
		"valkey-cli", "GET", fmt.Sprintf("valkey-mr-%d", lastIndex),
	})
	require.NoError(t, err)
	assert.Contains(t, resp, fmt.Sprintf("valkey-mr-idx-%d", lastIndex))
}
