package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/avast/retry-go/v4"
	"github.com/metal-stack/backup-restore-sidecar/pkg/constants"
	"github.com/metal-stack/metal-lib/pkg/pointer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	clientv3 "go.etcd.io/etcd/client/v3"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	_ "github.com/lib/pq"
)

const (
	etcdContainerImage = "quay.io/coreos/etcd:v3.5.7"
)

func Test_ETCD(t *testing.T) {
	var (
		sts = func(namespace string) *appsv1.StatefulSet {
			return &appsv1.StatefulSet{
				TypeMeta: metav1.TypeMeta{
					Kind:       "StatefulSet",
					APIVersion: appsv1.SchemeGroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "etcd",
					Namespace: namespace,
					Labels: map[string]string{
						"app": "etcd",
					},
				},
				Spec: appsv1.StatefulSetSpec{
					ServiceName: "etcd",
					Replicas:    pointer.Pointer(int32(1)),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "etcd",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "etcd",
							},
						},
						Spec: corev1.PodSpec{
							HostNetwork: true,
							Containers: []corev1.Container{
								{
									Name:    "etcd",
									Image:   etcdContainerImage,
									Command: []string{"backup-restore-sidecar", "wait"},
									LivenessProbe: &corev1.Probe{
										ProbeHandler: corev1.ProbeHandler{
											Exec: &corev1.ExecAction{
												Command: []string{"/usr/local/bin/etcdctl", "endpoint", "health", "--endpoints=127.0.0.1:32379"},
											},
										},
										InitialDelaySeconds: 15,
										TimeoutSeconds:      1,
										PeriodSeconds:       5,
										SuccessThreshold:    1,
										FailureThreshold:    3,
									},
									ReadinessProbe: &corev1.Probe{
										ProbeHandler: corev1.ProbeHandler{
											HTTPGet: &corev1.HTTPGetAction{
												Path:   "/health",
												Port:   intstr.FromInt(32381),
												Scheme: corev1.URISchemeHTTP,
											},
										},
										InitialDelaySeconds: 15,
										TimeoutSeconds:      1,
										PeriodSeconds:       5,
										SuccessThreshold:    1,
										FailureThreshold:    3,
									},
									Ports: []corev1.ContainerPort{
										// default ports are taken by kind etcd because running in host network
										{
											ContainerPort: 32379,
											Name:          "client",
											Protocol:      corev1.ProtocolTCP,
										},
										{
											ContainerPort: 32380,
											Name:          "server",
											Protocol:      corev1.ProtocolTCP,
										},
									},
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      "data",
											MountPath: "/data",
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
									Image:   etcdContainerImage,
									Command: []string{"backup-restore-sidecar", "start", "--log-level=debug"},
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
											MountPath: "/data",
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
												Name: "backup-restore-sidecar-config-postgres",
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

		backingResources = func(namespace string) []client.Object {
			return []client.Object{
				&corev1.ConfigMap{
					TypeMeta: metav1.TypeMeta{
						Kind:       "ConfigMap",
						APIVersion: corev1.SchemeGroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "backup-restore-sidecar-config-postgres",
						Namespace: namespace,
					},
					Data: map[string]string{
						"config.yaml": `---
bind-addr: 0.0.0.0
db: etcd
db-data-directory: /data/etcd/
backup-provider: local
backup-cron-schedule: "*/1 * * * *"
object-prefix: etcd-test
etcd-endpoints: http://localhost:32379
post-exec-cmds:
  - etcd --data-dir=/data/etcd --listen-client-urls http://0.0.0.0:32379 --advertise-client-urls http://0.0.0.0:32379 --listen-peer-urls http://0.0.0.0:32380 --initial-advertise-peer-urls http://0.0.0.0:32380 --initial-cluster default=http://0.0.0.0:32380 --listen-metrics-urls http://0.0.0.0:32381
`,
					},
				},
			}
		}

		newEtcdClient = func(t *testing.T, ctx context.Context) *clientv3.Client {
			var cli *clientv3.Client

			err := retry.Do(func() error {
				var err error
				cli, err = clientv3.New(clientv3.Config{
					Endpoints:   []string{"localhost:32379"},
					DialTimeout: 5 * time.Second,
				})
				if err != nil {
					return err
				}

				return nil
			}, retry.Context(ctx))
			require.NoError(t, err)

			return cli
		}

		addTestData = func(t *testing.T, ctx context.Context) {
			cli := newEtcdClient(t, ctx)
			defer cli.Close()

			_, err := cli.Put(ctx, "1", "I am precious")
			require.NoError(t, err)
		}

		verifyTestData = func(t *testing.T, ctx context.Context) {
			cli := newEtcdClient(t, ctx)
			defer cli.Close()

			resp, err := cli.Get(ctx, "1")
			require.NoError(t, err)
			require.Len(t, resp.Kvs, 1)

			ev := resp.Kvs[0]
			assert.Equal(t, "1", string(ev.Key))
			assert.Equal(t, "I am precious", string(ev.Value))
		}
	)

	restoreFlow(t, &flowSpec{
		databaseType:     "etcd",
		sts:              sts,
		backingResources: backingResources,
		addTestData:      addTestData,
		verifyTestData:   verifyTestData,
	})
}
