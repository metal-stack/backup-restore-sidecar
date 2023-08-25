package integration_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/avast/retry-go/v4"
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
	postgresContainerImage = "postgres:15-alpine"
)

func Test_Postgres(t *testing.T) {
	const (
		postgresDB       = "postgres"
		postgresPassword = "test123!"
		postgresUser     = "test"
		table            = "precioustestdata"
	)

	var (
		sts = func(namespace string) *appsv1.StatefulSet {
			return &appsv1.StatefulSet{
				TypeMeta: metav1.TypeMeta{
					Kind:       "StatefulSet",
					APIVersion: appsv1.SchemeGroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "postgres",
					Namespace: namespace,
					Labels: map[string]string{
						"app": "postgres",
					},
				},
				Spec: appsv1.StatefulSetSpec{
					ServiceName: "postgres",
					Replicas:    pointer.Pointer(int32(1)),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "postgres",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "postgres",
							},
						},
						Spec: corev1.PodSpec{
							HostNetwork: true,
							Containers: []corev1.Container{
								{
									Name:    "postgres",
									Image:   postgresContainerImage,
									Command: []string{"backup-restore-sidecar", "wait"},
									LivenessProbe: &corev1.Probe{
										ProbeHandler: corev1.ProbeHandler{
											Exec: &corev1.ExecAction{
												Command: []string{"/bin/sh", "-c", "exec", "pg_isready", "-U", postgresUser, "-h", "127.0.0.1", "-p", "5432"},
											},
										},
										InitialDelaySeconds: 30,
										TimeoutSeconds:      5,
										PeriodSeconds:       10,
										SuccessThreshold:    1,
										FailureThreshold:    6,
									},
									ReadinessProbe: &corev1.Probe{
										ProbeHandler: corev1.ProbeHandler{
											Exec: &corev1.ExecAction{
												Command: []string{"/bin/sh", "-c", "exec", "pg_isready", "-U", postgresUser, "-h", "127.0.0.1", "-p", "5432"},
											},
										},
										InitialDelaySeconds: 5,
										TimeoutSeconds:      5,
										PeriodSeconds:       10,
									},
									Env: []corev1.EnvVar{
										{
											Name: "POSTGRES_DB",
											ValueFrom: &corev1.EnvVarSource{
												SecretKeyRef: &corev1.SecretKeySelector{
													LocalObjectReference: corev1.LocalObjectReference{
														Name: "postgres",
													},
													Key: "POSTGRES_DB",
												},
											},
										},
										{
											Name: "POSTGRES_USER",
											ValueFrom: &corev1.EnvVarSource{
												SecretKeyRef: &corev1.SecretKeySelector{
													LocalObjectReference: corev1.LocalObjectReference{
														Name: "postgres",
													},
													Key: "POSTGRES_USER",
												},
											},
										},
										{
											Name: "POSTGRES_PASSWORD",
											ValueFrom: &corev1.EnvVarSource{
												SecretKeyRef: &corev1.SecretKeySelector{
													LocalObjectReference: corev1.LocalObjectReference{
														Name: "postgres",
													},
													Key: "POSTGRES_PASSWORD",
												},
											},
										},
										{
											Name: "PGDATA",
											ValueFrom: &corev1.EnvVarSource{
												SecretKeyRef: &corev1.SecretKeySelector{
													LocalObjectReference: corev1.LocalObjectReference{
														Name: "postgres",
													},
													Key: "POSTGRES_DATA",
												},
											},
										},
									},
									Ports: []corev1.ContainerPort{
										{
											ContainerPort: 5432,
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
									Image:   postgresContainerImage,
									Command: []string{"backup-restore-sidecar", "start", "--log-level=debug"},
									Env: []corev1.EnvVar{
										{
											Name: "BACKUP_RESTORE_SIDECAR_POSTGRES_PASSWORD",
											ValueFrom: &corev1.EnvVarSource{
												SecretKeyRef: &corev1.SecretKeySelector{
													LocalObjectReference: corev1.LocalObjectReference{
														Name: "postgres",
													},
													Key: "POSTGRES_PASSWORD",
												},
											},
										},
										{
											Name: "BACKUP_RESTORE_SIDECAR_POSTGRES_USER",
											ValueFrom: &corev1.EnvVarSource{
												SecretKeyRef: &corev1.SecretKeySelector{
													LocalObjectReference: corev1.LocalObjectReference{
														Name: "postgres",
													},
													Key: "POSTGRES_USER",
												},
											},
										},
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
db: postgres
db-data-directory: /data/postgres/
backup-provider: local
backup-cron-schedule: "*/1 * * * *"
object-prefix: postgres-test
compression-method: tar
post-exec-cmds:
- docker-entrypoint.sh postgres
`,
					},
				},
				&corev1.Secret{
					TypeMeta: metav1.TypeMeta{
						Kind:       "Secret",
						APIVersion: corev1.SchemeGroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "postgres",
						Namespace: namespace,
					},
					StringData: map[string]string{
						"POSTGRES_DB":       postgresDB,
						"POSTGRES_USER":     postgresUser,
						"POSTGRES_PASSWORD": postgresPassword,
						"POSTGRES_DATA":     "/data/postgres/",
					},
				},
				&corev1.Service{
					TypeMeta: metav1.TypeMeta{
						Kind:       "Service",
						APIVersion: corev1.SchemeGroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "postgres",
						Namespace: namespace,
						Labels: map[string]string{
							"app": "postgres",
						},
					},
					Spec: corev1.ServiceSpec{
						Selector: map[string]string{
							"app": "postgres",
						},
						Ports: []corev1.ServicePort{
							{
								Name:       "5432",
								Port:       5432,
								TargetPort: intstr.FromInt32(5432),
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

		newPostgresSession = func(t *testing.T, ctx context.Context) *sql.DB {
			var db *sql.DB
			err := retry.Do(func() error {
				connString := fmt.Sprintf("host=127.0.0.1 port=5432 user=%s password=%s dbname=%s sslmode=disable", postgresUser, postgresPassword, postgresDB)

				var err error
				db, err = sql.Open("postgres", connString)
				if err != nil {
					return err
				}

				return nil
			}, retry.Context(ctx))
			require.NoError(t, err)

			return db
		}

		addTestData = func(t *testing.T, ctx context.Context) {
			db := newPostgresSession(t, ctx)
			defer db.Close()

			var (
				createStmt = `CREATE TABLE backuprestore (
					data text NOT NULL
				 );`
				insertStmt = `INSERT INTO backuprestore("data") VALUES ('I am precious');`
			)

			_, err := db.Exec(createStmt)
			require.NoError(t, err)

			_, err = db.Exec(insertStmt)
			require.NoError(t, err)
		}

		verifyTestData = func(t *testing.T, ctx context.Context) {
			db := newPostgresSession(t, ctx)
			defer db.Close()

			rows, err := db.Query(`SELECT "data" FROM backuprestore`)
			require.NoError(t, err)
			require.NoError(t, rows.Err())
			defer rows.Close()

			require.True(t, rows.Next())
			var data string

			err = rows.Scan(&data)
			require.NoError(t, err)

			assert.Equal(t, "I am precious", data)
			assert.False(t, rows.Next())
		}
	)

	restoreFlow(t, &flowSpec{
		databaseType:     "postgres",
		sts:              sts,
		backingResources: backingResources,
		addTestData:      addTestData,
		verifyTestData:   verifyTestData,
	})
}
