package integration_test

import (
	"context"
	"fmt"
	"os"
	"path"
	"runtime"
	"strings"
	"testing"

	"github.com/avast/retry-go/v4"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/yaml"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

const (
	backupRestoreSidecarContainerImage = "ghcr.io/metal-stack/backup-restore-sidecar:latest"
)

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

func dumpToExamples(t *testing.T, name string, resources ...client.Object) {
	content := []byte(`# DO NOT EDIT! This is auto-generated by the integration tests
---
`)

	for i, r := range resources {
		r := r

		raw, err := yaml.Marshal(r)
		require.NoError(t, err)

		if i != len(resources)-1 {
			raw = append(raw, []byte("---\n")...)
		}

		content = append(content, raw...)
	}

	_, filename, _, _ := runtime.Caller(1)

	dest := path.Join(path.Dir(filename), "..", "deploy", name)
	t.Logf("example manifest written to %s", dest)

	err := os.WriteFile(dest, content, 0600)
	require.NoError(t, err)
}

func waitForPodRunnig(ctx context.Context, name, namespace string) error {
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
	}, retry.Context(ctx))
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
	}, retry.Context(ctx))
}