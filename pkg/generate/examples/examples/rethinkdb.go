package examples

import (
	"github.com/metal-stack/backup-restore-sidecar/pkg/constants"
	"github.com/metal-stack/metal-lib/pkg/pointer"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	RethinkDB = "rethinkdb"

	RethinkDbPassword = "test123!"

	rethinkDbContainerImage = "rethinkdb:2.4.4"
)

func RethinkDbSts(namespace string) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "StatefulSet",
			APIVersion: appsv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rethinkdb",
			Namespace: namespace,
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
									Name:      "data",
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
						Name: "data",
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{
							corev1.ReadWriteOnce,
						},
						Resources: corev1.VolumeResourceRequirements{
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
						Resources: corev1.VolumeResourceRequirements{
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

func RethinkDbBackingResources(namespace string) []client.Object {
	return []client.Object{
		&corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ConfigMap",
				APIVersion: corev1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "backup-restore-sidecar-config-rethinkdb",
				Namespace: namespace,
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
		},
		&corev1.Secret{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Secret",
				APIVersion: corev1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "rethinkdb",
				Namespace: namespace,
			},
			StringData: map[string]string{
				"rethinkdb-password": RethinkDbPassword,
			},
		},
		&corev1.Service{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Service",
				APIVersion: corev1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "rethinkdb",
				Namespace: namespace,
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
		},
	}
}
