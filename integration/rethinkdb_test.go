package integration_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	v1 "github.com/metal-stack/backup-restore-sidecar/api/v1"
	"github.com/metal-stack/backup-restore-sidecar/pkg/constants"
	"github.com/metal-stack/metal-lib/pkg/pointer"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/avast/retry-go/v4"
	r "gopkg.in/rethinkdb/rethinkdb-go.v6"

	brsclient "github.com/metal-stack/backup-restore-sidecar/pkg/client"
)

const (
	rethinkDbContainerImage = "rethinkdb:2.4.0"
)

func Test_RethinkDB(t *testing.T) {
	const (
		rethinkdbPassword = "test123!"
		db                = "backup-restore"
		table             = "precioustestdata"
		rethinkdbPodName  = "rethinkdb-0"
	)
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
	// defer cleanup()

	err := c.Create(ctx, ns)
	require.NoError(t, client.IgnoreAlreadyExists(err))

	var (
		cm = func() *corev1.ConfigMap {
			return &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "backup-restore-sidecar-config-rethinkdb",
					Namespace: ns.Name,
				},
				Data: map[string]string{
					"config.yaml": `---
bind-addr: 0.0.0.0
db: rethinkdb
db-data-directory: /data/rethinkdb/
backup-provider: local
rethinkdb-passwordfile: /rethinkdb-secret/rethinkdb-password.txt
backup-cron-schedule: "*/1 * * * *"
object-prefix: rethinkdb-test
post-exec-cmds:
# IMPORTANT: the --directory needs to point to the exact sidecar data dir, otherwise the database will be restored to the wrong location
- rethinkdb --bind all --directory /data/rethinkdb --initial-password ${RETHINKDB_PASSWORD}
`,
				},
			}
		}

		secret = func() *corev1.Secret {
			return &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rethinkdb",
					Namespace: ns.Name,
				},
				StringData: map[string]string{
					"rethinkdb-password": rethinkdbPassword,
				},
			}
		}

		service = func() *corev1.Service {
			return &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rethinkdb",
					Namespace: ns.Name,
					Labels: map[string]string{
						"app": "rethinkdb",
					},
				},
				Spec: corev1.ServiceSpec{
					Selector: map[string]string{
						"app": "rethinkdb",
					},
					Ports: []corev1.ServicePort{
						{
							Name:       "10080",
							Port:       10080,
							TargetPort: intstr.FromInt32(10080),
						},
						{
							Name:       "28015",
							Port:       28015,
							TargetPort: intstr.FromInt32(28015),
						},
						{
							Name:       "metrics",
							Port:       2112,
							TargetPort: intstr.FromInt32(2112),
						},
					},
				},
			}
		}

		sts = func() *appsv1.StatefulSet {
			return &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rethinkdb",
					Namespace: ns.Name,
					Labels: map[string]string{
						"app": "rethinkdb",
					},
				},
				Spec: appsv1.StatefulSetSpec{
					ServiceName: "rethinkdb",
					Replicas:    pointer.Pointer(int32(1)),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "rethinkdb",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "rethinkdb",
							},
						},
						Spec: corev1.PodSpec{
							HostNetwork: true,
							Containers: []corev1.Container{
								{
									Name:    "rethinkdb",
									Image:   rethinkDbContainerImage,
									Command: []string{"backup-restore-sidecar", "wait"},
									Env: []corev1.EnvVar{
										{
											Name: "RETHINKDB_PASSWORD",
											ValueFrom: &corev1.EnvVarSource{
												SecretKeyRef: &corev1.SecretKeySelector{
													LocalObjectReference: corev1.LocalObjectReference{
														Name: "rethinkdb",
													},
													Key: "rethinkdb-password",
												},
											},
										},
									},
									Ports: []corev1.ContainerPort{
										{
											ContainerPort: 8080,
										},
										{
											ContainerPort: 28015,
										},
									},
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      "rethinkdb",
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
									Image:   rethinkDbContainerImage,
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
											Name:      "rethinkdb",
											MountPath: "/data",
										},
										{
											Name:      "rethinkdb-credentials",
											MountPath: "/rethinkdb-secret",
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
										{
											Name:      "bin-provision",
											SubPath:   "rethinkdb-dump",
											MountPath: "/usr/local/bin/rethinkdb-dump",
										},
										{
											Name:      "bin-provision",
											SubPath:   "rethinkdb-restore",
											MountPath: "/usr/local/bin/rethinkdb-restore",
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
										"/rethinkdb/rethinkdb-dump",
										"/rethinkdb/rethinkdb-restore",
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
									Name: "rethinkdb",
									VolumeSource: corev1.VolumeSource{
										PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
											ClaimName: "rethinkdb",
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
									Name: "rethinkdb-credentials",
									VolumeSource: corev1.VolumeSource{
										Secret: &corev1.SecretVolumeSource{
											SecretName: "rethinkdb",
											Items: []corev1.KeyToPath{
												{
													Key:  "rethinkdb-password",
													Path: "rethinkdb-password.txt",
												},
											},
										},
									},
								},
								{
									Name: "backup-restore-sidecar-config",
									VolumeSource: corev1.VolumeSource{
										ConfigMap: &corev1.ConfigMapVolumeSource{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "backup-restore-sidecar-config-rethinkdb",
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
								Name: "rethinkdb",
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
	)

	t.Log("applying resource manifests")

	objects := []client.Object{cm(), secret(), service(), sts()}
	dumpToExamples(t, "rethinkdb-test.yaml", objects...)
	for _, o := range objects {
		o := o
		err = c.Create(ctx, o)
		require.NoError(t, err)
	}

	err = waitForPodRunnig(ctx, rethinkdbPodName, ns.Name)
	require.NoError(t, err)

	t.Log("adding test data to database")

	var session *r.Session
	err = retry.Do(func() error {
		var err error
		session, err = r.Connect(r.ConnectOpts{
			Addresses: []string{"localhost:28015"},
			Database:  db,
			Username:  "admin",
			Password:  rethinkdbPassword,
			MaxIdle:   10,
			MaxOpen:   20,
		})
		if err != nil {
			return fmt.Errorf("cannot connect to DB: %w", err)
		}

		return nil
	}, retry.Context(ctx), retry.Attempts(0))
	require.NoError(t, err)

	_, _ = r.DBDrop(db).RunWrite(session)

	_, err = r.DBCreate(db).RunWrite(session)
	require.NoError(t, err)

	_, err = r.DB(db).TableCreate(table).RunWrite(session)
	require.NoError(t, err)

	type testData struct {
		ID   string `rethinkdb:"id"`
		Data string `rethinkdb:"data"`
	}

	_, err = r.DB(db).Table(table).Insert(testData{
		ID:   "1",
		Data: "i am precious",
	}).RunWrite(session)
	require.NoError(t, err)

	cursor, err := r.DB(db).Table(table).Get("1").Run(session)
	require.NoError(t, err)

	var d1 testData
	err = cursor.One(&d1)
	require.NoError(t, err)
	require.Equal(t, "i am precious", d1.Data)

	t.Log("waiting for backup to be created")

	brsc, err := brsclient.New(ctx, "http://localhost:8000")
	require.NoError(t, err)

	var backup *v1.Backup
	err = retry.Do(func() error {
		backups, err := brsc.BackupServiceClient().ListBackups(ctx, &v1.Empty{})
		if err != nil {
			return err
		}

		if len(backups.Backups) == 0 {
			return fmt.Errorf("no backups were made yet")
		}

		backup = backups.Backups[0]

		return nil
	}, retry.Context(ctx), retry.Attempts(0), retry.MaxDelay(2*time.Second))
	require.NoError(t, err)
	require.NotNil(t, backup)

	t.Log("remove sts and delete data volume")

	err = c.Delete(ctx, sts())
	require.NoError(t, err)

	err = c.Delete(ctx, &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rethinkdb-rethinkdb-0",
			Namespace: ns.Name,
		},
	})
	require.NoError(t, err)

	err = waitUntilNotFound(ctx, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rethinkdbPodName,
			Namespace: ns.Name,
		},
	})
	require.NoError(t, err)

	t.Log("recreate sts")

	err = c.Create(ctx, sts())
	require.NoError(t, err)

	err = waitForPodRunnig(ctx, rethinkdbPodName, ns.Name)
	require.NoError(t, err)

	t.Log("verify that data gets restored")

	cursor, err = r.DB(db).Table(table).Get("1").Run(session)
	require.NoError(t, err)

	var d2 testData
	err = cursor.One(&d2)
	require.NoError(t, err)
	require.Equal(t, "i am precious", d2.Data)
}
