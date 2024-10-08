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
	Meilisearch         = "meilisearch"
	MeilisearchPassword = "test123!"

	meilisearchContainerImage = "getmeili/meilisearch:v1.8.1"
)

func MeilisearchSts(namespace string) *appsv1.StatefulSet {
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
							Image:           meilisearchContainerImage,
							ImagePullPolicy: corev1.PullIfNotPresent,
							Command:         []string{"backup-restore-sidecar", "wait"},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/health",
										Port: intstr.FromString("http"),
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
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/health",
										Port: intstr.FromString("http"),
									},
								},
								InitialDelaySeconds: 5,
								TimeoutSeconds:      5,
								PeriodSeconds:       10,
							},
							StartupProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/health",
										Port: intstr.FromString("http"),
									},
								},
								InitialDelaySeconds: 5,
								PeriodSeconds:       5,
								TimeoutSeconds:      10,
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
									Name:      "backup",
									MountPath: constants.SidecarBaseDir,
								},
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
							Image:   meilisearchContainerImage,
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

func MeilisearchBackingResources(namespace string) []client.Object {
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
db-data-directory: /data/data.ms/
backup-provider: local
backup-cron-schedule: "*/1 * * * *"
object-prefix: meilisearch-test
compression-method: targz
encryption-key: "01234567891234560123456789123456"
post-exec-cmds:
- meilisearch --db-path=/data/data.ms/ --dump-dir=/backup/upload/files
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
				"MEILISEARCH_APIKEY": MeilisearchPassword,
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
