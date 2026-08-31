package examples

import (
	"github.com/metal-stack/backup-restore-sidecar/pkg/constants"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	Valkey               = "valkey"
	valkeyContainerImage = "ghcr.io/valkey-io/valkey:8.1-alpine"
)

func ValkeySts(namespace string) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		Kind:       "StatefulSet",
		APIVersion: appsv1.SchemeGroupVersion.String(),
		Name:       "valkey",
		Namespace:  namespace,
		Labels: map[string]string{
			"app": "valkey",
		},
		Spec: appsv1.StatefulSetSpec{
			ServiceName: "valkey",
			Replicas:    new(int32(1)),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "valkey",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "valkey",
					},
				},
				Spec: corev1.PodSpec{
					HostNetwork: true,
					Containers: []corev1.Container{
						{
							Name:            "valkey",
							Image:           valkeyContainerImage,
							ImagePullPolicy: corev1.PullIfNotPresent,
							Command:         []string{"backup-restore-sidecar", "wait"},
							LivenessProbe: &corev1.Probe{
								Exec: &corev1.ExecAction{
									Command: []string{"valkey-cli", "ping"},
								},
								InitialDelaySeconds: 15,
								TimeoutSeconds:      1,
								PeriodSeconds:       5,
								SuccessThreshold:    1,
								FailureThreshold:    3,
							},
							ReadinessProbe: &corev1.Probe{
								Exec: &corev1.ExecAction{
									Command: []string{"valkey-cli", "ping"},
								},
								InitialDelaySeconds: 15,
								TimeoutSeconds:      1,
								PeriodSeconds:       5,
								SuccessThreshold:    1,
								FailureThreshold:    3,
							},
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: 6379,
									Name:          "client",
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
							Name:            "backup-restore-sidecar",
							Image:           valkeyContainerImage,
							ImagePullPolicy: corev1.PullIfNotPresent,
							Command:         []string{"backup-restore-sidecar", "start", "--log-level=debug"},
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
							PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
								ClaimName: "data",
							},
						},
						{
							Name: "backup",
							PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
								ClaimName: "backup",
							},
						},
						{
							Name: "backup-restore-sidecar-config",
							ConfigMap: &corev1.ConfigMapVolumeSource{
								Name: "backup-restore-sidecar-config-valkey",
							},
						},
						{
							Name:     "bin-provision",
							EmptyDir: &corev1.EmptyDirVolumeSource{},
						},
					},
				},
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					Name: "data",
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
					Name: "backup",
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

func ValkeyBackingResources(namespace string) []client.Object {
	return []client.Object{
		&corev1.ConfigMap{
			Kind:       "ConfigMap",
			APIVersion: corev1.SchemeGroupVersion.String(),
			Name:       "backup-restore-sidecar-config-valkey",
			Namespace:  namespace,
			Data: map[string]string{
				"config.yaml": `---
bind-addr: 0.0.0.0
db: valkey
db-data-directory: /data/
backup-provider: local
backup-cron-schedule: "*/1 * * * *"
object-prefix: valkey-test
redis-addr: localhost:6379
encryption-key: "01234567891234560123456789123456"
post-exec-cmds:
- valkey-server
`,
			},
		},
	}
}
