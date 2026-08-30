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
	Localfs               = "localfs"
	LocalfsContainerImage = "alpine:3.22"
)

func LocalfsSts(namespace string) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		Kind:       "StatefulSet",
		APIVersion: appsv1.SchemeGroupVersion.String(),
		Name:       "localfs",
		Namespace:  namespace,
		Labels: map[string]string{
			"app": "localfs",
		},
		Spec: appsv1.StatefulSetSpec{
			ServiceName: "localfs",
			Replicas:    new(int32(1)),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "localfs",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "localfs",
					},
				},
				Spec: corev1.PodSpec{
					HostNetwork: true,
					Containers: []corev1.Container{
						{
							Name:    "localfs",
							Image:   LocalfsContainerImage,
							Command: []string{"backup-restore-sidecar", "wait"},

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
							Image:   LocalfsContainerImage,
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
								Name: "backup-restore-sidecar-config-localfs",
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

func LocalfsBackingResources(namespace string) []client.Object {
	return []client.Object{
		&corev1.ConfigMap{
			Kind:       "ConfigMap",
			APIVersion: corev1.SchemeGroupVersion.String(),
			Name:       "backup-restore-sidecar-config-localfs",
			Namespace:  namespace,
			Data: map[string]string{
				"config.yaml": `---
bind-addr: 0.0.0.0
db: localfs
db-data-directory: /data/
backup-provider: local
backup-cron-schedule: "*/1 * * * *"
object-prefix: localfs-test
redis-addr: localhost:6379
encryption-key: "01234567891234560123456789123456"
post-exec-cmds:
- tail -f /etc/hosts
`,
			},
		},
	}
}
