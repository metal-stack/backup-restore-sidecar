//go:build integration

package integration_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/avast/retry-go/v4"
	"github.com/meilisearch/meilisearch-go"
	"github.com/metal-stack/backup-restore-sidecar/pkg/constants"
	"github.com/metal-stack/metal-lib/pkg/pointer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	_ "github.com/lib/pq"
)

const (
	meilisearchIndex    = "backup-restore-sidecar"
	meilisearchPassword = "test123!"
)

var (
	meilisearchContainerImage = "getmeili/meilisearch:v1.3.0"
)

func Test_Meilisearch_Restore(t *testing.T) {
	restoreFlow(t, &flowSpec{
		databaseType:     "meilisearch",
		sts:              meilisearchSts,
		backingResources: meilisearchBackingResources,
		addTestData:      addMeilisearchTestData,
		verifyTestData:   verifyMeilisearchTestData,
	})
}

func Test_Meilisearch_Upgrade(t *testing.T) {
	upgradeFlow(t, &flowSpec{
		databaseType: "meilisearch",
		databaseImages: []string{
			"getmeili/meilisearch:v1.1.0",
			// "getmeili/meilisearch:v1.2.0", commented to test if two versions upgrade also work
			"getmeili/meilisearch:v1.3.0",
			"getmeili/meilisearch:v1.3.2",
		},
		sts:              meilisearchSts,
		backingResources: meilisearchBackingResources,
		addTestData:      addMeilisearchTestData,
		verifyTestData:   verifyMeilisearchTestData,
	})
}

func meilisearchSts(namespace, image string) *appsv1.StatefulSet {
	if image == "" {
		image = meilisearchContainerImage
	}

	return &appsv1.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "StatefulSet",
			APIVersion: appsv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "meilisearch",
			Namespace: namespace,
			Labels: map[string]string{
				"app": "meilisearch",
			},
		},
		Spec: appsv1.StatefulSetSpec{
			ServiceName: "meilisearch",
			Replicas:    pointer.Pointer(int32(1)),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "meilisearch",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "meilisearch",
					},
				},
				Spec: corev1.PodSpec{
					HostNetwork: true,
					Containers: []corev1.Container{
						{
							Name:            "meilisearch",
							Image:           image,
							ImagePullPolicy: corev1.PullIfNotPresent,
							Command:         []string{"backup-restore-sidecar", "wait"},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/health",
										Port: intstr.FromString("http"),
									},
								},
								InitialDelaySeconds: 5,
								TimeoutSeconds:      1,
								PeriodSeconds:       5,
								SuccessThreshold:    1,
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/health",
										Port: intstr.FromString("http"),
									},
								},
								InitialDelaySeconds: 5,
								TimeoutSeconds:      1,
								PeriodSeconds:       5,
								SuccessThreshold:    1,
							},
							StartupProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/health",
										Port: intstr.FromString("http"),
									},
								},
								InitialDelaySeconds: 1,
								PeriodSeconds:       1,
								TimeoutSeconds:      1,
								SuccessThreshold:    1,
							},
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: 7700,
									Name:          "http",
									Protocol:      corev1.ProtocolTCP,
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "data",
									MountPath: "/meili_data",
								},
								{
									Name:      "bin-provision",
									SubPath:   "backup-restore-sidecar",
									MountPath: "/usr/local/bin/backup-restore-sidecar",
								},
								{
									Name:      "backup-restore-sidecar-config",
									MountPath: "/etc/backup-restore-sidecar",
								},
							},
						},
						{
							Name:    "backup-restore-sidecar",
							Image:   image,
							Command: []string{"backup-restore-sidecar", "start", "--log-level=debug"},
							Env: []corev1.EnvVar{
								{
									Name: "BACKUP_RESTORE_SIDECAR_MEILISEARCH_APIKEY",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "meilisearch",
											},
											Key: "MEILISEARCH_APIKEY",
										},
									},
								},
								{
									Name: "BACKUP_RESTORE_SIDECAR_MEILISEARCH_URL",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "meilisearch",
											},
											Key: "MEILISEARCH_URL",
										},
									}},
							},
							Ports: []corev1.ContainerPort{
								{
									Name:          "grpc",
									ContainerPort: 8000,
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "backup",
									MountPath: constants.SidecarBaseDir,
								},
								{
									Name:      "data",
									MountPath: "/meili_data",
								},
								{
									Name:      "backup-restore-sidecar-config",
									MountPath: "/etc/backup-restore-sidecar",
								},
								{
									Name:      "bin-provision",
									SubPath:   "backup-restore-sidecar",
									MountPath: "/usr/local/bin/backup-restore-sidecar",
								},
							},
						},
					},
					InitContainers: []corev1.Container{
						{
							Name:            "backup-restore-sidecar-provider",
							Image:           backupRestoreSidecarContainerImage,
							ImagePullPolicy: corev1.PullIfNotPresent,
							Command: []string{
								"cp",
								"/backup-restore-sidecar",
								"/bin-provision",
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "bin-provision",
									MountPath: "/bin-provision",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "data",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: "data",
								},
							},
						},
						{
							Name: "backup",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: "backup",
								},
							},
						},
						{
							Name: "backup-restore-sidecar-config",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "backup-restore-sidecar-config-meilisearch",
									},
								},
							},
						},
						{
							Name: "bin-provision",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
				},
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "data",
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{
							corev1.ReadWriteOnce,
						},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse("1Gi"),
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "backup",
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{
							corev1.ReadWriteOnce,
						},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse("1Gi"),
							},
						},
					},
				},
			},
		},
	}
}

func meilisearchBackingResources(namespace string) []client.Object {
	return []client.Object{
		&corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ConfigMap",
				APIVersion: corev1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "backup-restore-sidecar-config-meilisearch",
				Namespace: namespace,
			},
			Data: map[string]string{
				"config.yaml": `---
bind-addr: 0.0.0.0
db: meilisearch
db-data-directory: /meili_data/
backup-provider: local
backup-cron-schedule: "*/1 * * * *"
object-prefix: meilisearch-test
compression-method: tar
post-exec-cmds:
- meilisearch
`,
			},
		},
		&corev1.Secret{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Secret",
				APIVersion: corev1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "meilisearch",
				Namespace: namespace,
			},
			StringData: map[string]string{
				"MEILISEARCH_APIKEY": meilisearchPassword,
				"MEILISEARCH_URL":    "http://localhost:7700",
			},
		},
		&corev1.Service{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Service",
				APIVersion: corev1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "meilisearch",
				Namespace: namespace,
				Labels: map[string]string{
					"app": "meilisearch",
				},
			},
			Spec: corev1.ServiceSpec{
				Selector: map[string]string{
					"app": "meilisearch",
				},
				Ports: []corev1.ServicePort{
					{
						Name:       "7700",
						Port:       7700,
						TargetPort: intstr.FromInt32(7700),
					},
					{
						Name:       "metrics",
						Port:       2112,
						TargetPort: intstr.FromInt32(2112),
					},
				},
			},
		},
	}
}

func newMeilisearchSession(t *testing.T, ctx context.Context) *meilisearch.Client {
	var client *meilisearch.Client
	err := retry.Do(func() error {

		client = meilisearch.NewClient(meilisearch.ClientConfig{
			Host:   "http://localhost:7700",
			APIKey: meilisearchPassword,
		})

		ok := client.IsHealthy()
		if !ok {
			return fmt.Errorf("meilisearch is not yet healthy")
		}
		return nil
	}, retry.Context(ctx))
	require.NoError(t, err)

	return client
}

func addMeilisearchTestData(t *testing.T, ctx context.Context) {
	client := newMeilisearchSession(t, ctx)
	creationTask, err := client.CreateIndex(&meilisearch.IndexConfig{
		Uid:        meilisearchIndex,
		PrimaryKey: "id",
	})
	require.NoError(t, err)
	_, err = client.WaitForTask(creationTask.TaskUID)
	require.NoError(t, err)

	index := client.Index(meilisearchIndex)
	testdata := map[string]any{
		"id":  "1",
		"key": "I am precious",
	}
	indexTask, err := index.AddDocuments(testdata, "id")
	require.NoError(t, err)
	_, err = client.WaitForTask(indexTask.TaskUID)
	require.NoError(t, err)
}

func verifyMeilisearchTestData(t *testing.T, ctx context.Context) {
	client := newMeilisearchSession(t, ctx)
	index, err := client.GetIndex(meilisearchIndex)
	require.NoError(t, err)
	testdata := make(map[string]any)
	err = index.GetDocument("1", &meilisearch.DocumentQuery{}, &testdata)
	require.NoError(t, err)
	assert.Equal(t, "I am precious", testdata["key"])
}
