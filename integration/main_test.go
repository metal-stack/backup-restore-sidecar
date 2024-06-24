//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/avast/retry-go/v4"
	v1 "github.com/metal-stack/backup-restore-sidecar/api/v1"
	brsclient "github.com/metal-stack/backup-restore-sidecar/pkg/client"
	"github.com/metal-stack/backup-restore-sidecar/pkg/constants"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

type flowSpec struct {
	databaseType string

	sts                     func(namespace string) *appsv1.StatefulSet
	backingResources        func(namespace string) []client.Object
	addTestData             func(t *testing.T, ctx context.Context)
	addTestDataWithIndex    func(t *testing.T, ctx context.Context, index int)
	verifyTestData          func(t *testing.T, ctx context.Context)
	verifyTestDataWithIndex func(t *testing.T, ctx context.Context, index int)
}

type upgradeFlowSpec struct {
	flowSpec

	// slice of images, executed in order during upgrade
	databaseImages []string
}

var (
	restConfig *rest.Config
	c          client.Client
)

func TestMain(m *testing.M) {
	var err error
	c, err = newKubernetesClient()
	if err != nil {
		fmt.Printf("error creating kubernetes client: %s\n", err)
		os.Exit(1)
	}

	os.Exit(m.Run())
}

func restoreFlow(t *testing.T, spec *flowSpec) {
	t.Log("running restore flow")
	var (
		ctx, cancel = context.WithTimeout(context.Background(), 10*time.Minute)
		ns          = testNamespace(t)
	)

	defer cancel()

	cleanup := func() {
		t.Log("running cleanup")

		err := c.Delete(ctx, ns)
		require.NoError(t, client.IgnoreNotFound(err), "cleanup did not succeed")

		err = waitUntilNotFound(ctx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: ns.Name,
			},
		})
		require.NoError(t, err, "cleanup did not succeed")
	}
	cleanup()
	defer cleanup()

	err := c.Create(ctx, ns)
	require.NoError(t, client.IgnoreAlreadyExists(err))

	t.Log("applying resource manifests")

	objects := func() []client.Object {
		objects := []client.Object{spec.sts(ns.Name)}
		objects = append(objects, spec.backingResources(ns.Name)...)
		return objects
	}

	for _, o := range objects() {
		o := o
		err = c.Create(ctx, o)
		require.NoError(t, err)
	}

	podName := spec.sts(ns.Name).Name + "-0"

	err = waitForPodRunning(ctx, podName, ns.Name)
	require.NoError(t, err)

	t.Log("adding test data to database")

	spec.addTestData(t, ctx)

	t.Log("taking a backup")

	brsc, err := brsclient.New(ctx, "http://localhost:8000")
	require.NoError(t, err)

	_, err = brsc.DatabaseServiceClient().CreateBackup(ctx, &v1.CreateBackupRequest{})
	if err != nil && !errors.Is(err, constants.ErrBackupAlreadyInProgress) {
		require.NoError(t, err)
	}

	var backup *v1.Backup
	err = retry.Do(func() error {
		backups, err := brsc.BackupServiceClient().ListBackups(ctx, &v1.ListBackupsRequest{})
		if err != nil {
			return err
		}

		if len(backups.GetBackups()) == 0 {
			return fmt.Errorf("no backups were made yet")
		}

		backup = backups.GetBackups()[0]

		return nil
	}, retry.Context(ctx), retry.Attempts(0), retry.MaxDelay(2*time.Second))
	require.NoError(t, err)
	require.NotNil(t, backup)

	t.Log("remove sts and delete data volume")

	err = c.Delete(ctx, spec.sts(ns.Name))
	require.NoError(t, err)

	err = c.Delete(ctx, &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "data-" + podName,
			Namespace: ns.Name,
		},
	})
	require.NoError(t, err)

	err = waitUntilNotFound(ctx, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: ns.Name,
		},
	})
	require.NoError(t, err)

	t.Log("recreate sts")

	err = c.Create(ctx, spec.sts(ns.Name))
	require.NoError(t, err)

	err = waitForPodRunning(ctx, podName, ns.Name)
	require.NoError(t, err)

	t.Log("verify that data gets restored")

	spec.verifyTestData(t, ctx)
}

func restoreWithEmptyDatadirFlow(t *testing.T, spec *flowSpec) {
	t.Log("running restore with empty datadir flow")
	var (
		ctx, cancel = context.WithTimeout(context.Background(), 10*time.Minute)
		ns          = testNamespace(t)
	)

	defer cancel()

	cleanup := func() {
		t.Log("running cleanup")

		err := c.Delete(ctx, ns)
		require.NoError(t, client.IgnoreNotFound(err), "cleanup did not succeed")

		err = waitUntilNotFound(ctx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: ns.Name,
			},
		})
		require.NoError(t, err, "cleanup did not succeed")
	}
	cleanup()
	defer cleanup()

	err := c.Create(ctx, ns)
	require.NoError(t, client.IgnoreAlreadyExists(err))

	t.Log("applying resource manifests")

	objects := func() []client.Object {
		objects := []client.Object{spec.sts(ns.Name)}
		objects = append(objects, spec.backingResources(ns.Name)...)
		return objects
	}

	for _, o := range objects() {
		o := o
		err = c.Create(ctx, o)
		require.NoError(t, err)
	}

	podName := spec.sts(ns.Name).Name + "-0"

	err = waitForPodRunning(ctx, podName, ns.Name)
	require.NoError(t, err)

	t.Log("adding multiple test data to database")

	brsc, err := brsclient.New(ctx, "http://localhost:8000")
	require.NoError(t, err)

	lastindex := 0
	for i := range 10 {
		t.Log("adding test data", "index", i)
		spec.addTestDataWithIndex(t, ctx, i)

		t.Log("taking a backup")
		_, err = brsc.DatabaseServiceClient().CreateBackup(ctx, &v1.CreateBackupRequest{})
		if err != nil && !errors.Is(err, constants.ErrBackupAlreadyInProgress) {
			require.NoError(t, err)
		}
		lastindex = i
	}

	var backup *v1.Backup
	err = retry.Do(func() error {
		backups, err := brsc.BackupServiceClient().ListBackups(ctx, &v1.ListBackupsRequest{})
		if err != nil {
			return err
		}

		if len(backups.GetBackups()) == 0 {
			return fmt.Errorf("no backups were made yet")
		}

		backup = backups.GetBackups()[0]

		return nil
	}, retry.Context(ctx), retry.Attempts(0), retry.MaxDelay(2*time.Second))
	require.NoError(t, err)
	require.NotNil(t, backup)

	t.Log("remove sts and delete data volume")

	err = c.Delete(ctx, spec.sts(ns.Name))
	require.NoError(t, err)

	err = c.Delete(ctx, &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "data-" + podName,
			Namespace: ns.Name,
		},
	})
	require.NoError(t, err)

	err = waitUntilNotFound(ctx, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: ns.Name,
		},
	})
	require.NoError(t, err)

	t.Log("recreate sts")

	err = c.Create(ctx, spec.sts(ns.Name))
	require.NoError(t, err)

	err = waitForPodRunning(ctx, podName, ns.Name)
	require.NoError(t, err)

	t.Log("verify that data gets restored")

	spec.verifyTestDataWithIndex(t, ctx, lastindex)
}

func upgradeFlow(t *testing.T, spec *upgradeFlowSpec) {
	t.Log("running upgrade flow")

	require.GreaterOrEqual(t, len(spec.databaseImages), 2, "at least 2 database images must be specified for the upgrade test")

	var (
		ctx, cancel  = context.WithTimeout(context.Background(), 10*time.Minute)
		ns           = testNamespace(t)
		initialImage = spec.databaseImages[0]
		nextImages   = spec.databaseImages[1:]
	)

	defer cancel()

	cleanup := func() {
		t.Log("running cleanup")

		err := c.Delete(ctx, ns)
		require.NoError(t, client.IgnoreNotFound(err), "cleanup did not succeed")

		err = waitUntilNotFound(ctx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: ns.Name,
			},
		})
		require.NoError(t, err, "cleanup did not succeed")
	}
	cleanup()
	defer cleanup()

	err := c.Create(ctx, ns)
	require.NoError(t, client.IgnoreAlreadyExists(err))

	t.Logf("starting database with initial image %q", initialImage)

	sts := spec.sts(ns.Name)
	for i := range sts.Spec.Template.Spec.Containers {
		sts.Spec.Template.Spec.Containers[i].Image = initialImage
	}

	objects := func() []client.Object {
		objects := []client.Object{sts}
		objects = append(objects, spec.backingResources(ns.Name)...)
		return objects
	}

	for _, o := range objects() {
		o := o
		err = c.Create(ctx, o)
		require.NoError(t, err)
	}

	podName := spec.sts(ns.Name).Name + "-0"

	err = waitForPodRunning(ctx, podName, ns.Name)
	require.NoError(t, err)

	t.Log("adding test data to database")

	spec.addTestData(t, ctx)

	for _, image := range nextImages {
		t.Log("taking a backup")

		brsc, err := brsclient.New(ctx, "http://localhost:8000")
		require.NoError(t, err)

		var backup *v1.Backup
		err = retry.Do(func() error {
			_, err = brsc.DatabaseServiceClient().CreateBackup(ctx, &v1.CreateBackupRequest{})
			if err != nil {
				t.Log(err)
				return err
			}

			backups, err := brsc.BackupServiceClient().ListBackups(ctx, &v1.ListBackupsRequest{})
			if err != nil {
				return err
			}

			if len(backups.GetBackups()) == 0 {
				return fmt.Errorf("no backups were made yet")
			}

			backup = backups.GetBackups()[0]

			return nil
		}, retry.Context(ctx), retry.Attempts(0), retry.MaxDelay(2*time.Second))
		require.NoError(t, err)
		require.NotNil(t, backup)

		nextSts := spec.sts(ns.Name)
		for i := range nextSts.Spec.Template.Spec.Containers {
			nextSts.Spec.Template.Spec.Containers[i].Image = image
		}
		t.Logf("deploy sts with next database version %q, container %q", image, nextSts.Spec.Template.Spec.Containers[0].Image)

		err = c.Update(ctx, nextSts, &client.UpdateOptions{})
		require.NoError(t, err)

		time.Sleep(1 * time.Second)

		err = waitForStsRunning(ctx, sts.Name, ns.Name)
		require.NoError(t, err)

		err = waitForPodRunning(ctx, podName, ns.Name)
		require.NoError(t, err)

		t.Log("verify that data is still the same")

		spec.verifyTestData(t, ctx)
	}
}

func testNamespace(t *testing.T) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespaceName(t),
		},
	}
}

func namespaceName(t *testing.T) string {
	const max = 63 // max length for k8s namespaces is 63 chars
	n := strings.ToLower(strings.ReplaceAll(t.Name(), "_", "-"))
	if len(n) > max {
		return n[:max]
	}
	return n
}

func newKubernetesClient() (client.Client, error) {
	restConfig = config.GetConfigOrDie()
	c, err := client.New(restConfig, client.Options{})
	if err != nil {
		return nil, err
	}

	nodes := &corev1.NodeList{}
	err = c.List(context.Background(), nodes)
	if err != nil {
		return nil, err
	}

	for _, n := range nodes.Items {
		n := n
		if !strings.HasPrefix(n.Spec.ProviderID, "kind://") && os.Getenv("SKIP_KIND_VALIDATIONS") != "1" {
			return nil, fmt.Errorf("for security reasons only running against kind clusters")
		}
	}

	return c, nil
}

func waitForPodRunning(ctx context.Context, name, namespace string) error {
	return retry.Do(func() error {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
		}

		err := c.Get(ctx, client.ObjectKeyFromObject(pod), pod)
		if err != nil {
			return err
		}

		if pod.Status.Phase != corev1.PodRunning {
			return fmt.Errorf("pod is not yet running running")
		}

		if len(pod.Spec.Containers) != len(pod.Status.ContainerStatuses) {
			return fmt.Errorf("not all containers available in status")
		}

		for _, status := range pod.Status.ContainerStatuses {
			if !status.Ready {
				return fmt.Errorf("container not yet ready: %s", status.Name)
			}
		}

		return nil
	}, retry.Context(ctx), retry.Attempts(0), retry.MaxDelay(2*time.Second))
}

func waitForStsRunning(ctx context.Context, name, namespace string) error {
	return retry.Do(func() error {
		sts := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
		}

		err := c.Get(ctx, client.ObjectKeyFromObject(sts), sts)
		if err != nil {
			return err
		}

		if sts.Status.CurrentRevision != sts.Status.UpdateRevision {
			return fmt.Errorf("sts revision is not yet running running")
		}

		if sts.Status.ReadyReplicas != sts.Status.Replicas {
			return fmt.Errorf("not all replicas in ready status")
		}

		return nil
	}, retry.Context(ctx), retry.Attempts(0), retry.MaxDelay(2*time.Second))
}

func waitUntilNotFound(ctx context.Context, obj client.Object) error {
	return retry.Do(func() error {
		err := c.Get(ctx, client.ObjectKeyFromObject(obj), obj)
		if err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}
			return err
		}

		return fmt.Errorf("resource is still running: %s", obj.GetName())
	}, retry.Context(ctx), retry.Attempts(0), retry.MaxDelay(2*time.Second))
}

func execCommand(ctx context.Context, podName string, namespace string, containerName string, cmd []string) (string, error) {
	var stdout, stderr bytes.Buffer
	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return "", err
	}

	req := client.CoreV1().RESTClient().Post().Resource("pods").Name(podName).Namespace(namespace).SubResource("exec")
	option := &corev1.PodExecOptions{
		Command:   cmd,
		Stdin:     false,
		Stdout:    true,
		Stderr:    true,
		TTY:       true,
		Container: containerName,
	}

	req.VersionedParams(
		option,
		scheme.ParameterCodec,
	)
	exec, err := remotecommand.NewSPDYExecutor(restConfig, "POST", req.URL())
	if err != nil {
		return "", err
	}
	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:  nil,
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(stdout.String()), nil
}
