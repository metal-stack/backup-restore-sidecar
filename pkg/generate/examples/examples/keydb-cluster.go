package examples

import (
	"fmt"

	"github.com/metal-stack/backup-restore-sidecar/pkg/constants"
	"github.com/metal-stack/metal-lib/pkg/pointer"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const KeyDBCluster = "keydb-cluster"

func KeyDBClusterSts(namespace string) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "StatefulSet",
			APIVersion: appsv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "keydb",
			Namespace: namespace,
			Labels: map[string]string{
				"app": "keydb",
			},
		},
		Spec: appsv1.StatefulSetSpec{
			ServiceName: "keydb-headless",
			Replicas:    pointer.Pointer(int32(3)),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "keydb",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "keydb",
					},
				},
				Spec: corev1.PodSpec{
					HostNetwork: true,
					Containers: []corev1.Container{
						{
							Name:    "keydb",
							Image:   keyDBContainerImage,
							Command: []string{"backup-restore-sidecar", "wait"},
							Env: []corev1.EnvVar{
								{
									Name: "MY_POD_NAME",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											FieldPath: "metadata.name",
										},
									},
								},
							},
							// LivenessProbe: &corev1.Probe{
							// 	ProbeHandler: corev1.ProbeHandler{
							// 		Exec: &corev1.ExecAction{
							// 			Command: []string{"keydb-cli", "ping"},
							// 		},
							// 	},
							// 	InitialDelaySeconds: 15,
							// 	TimeoutSeconds:      1,
							// 	PeriodSeconds:       5,
							// 	SuccessThreshold:    1,
							// 	FailureThreshold:    3,
							// },
							// ReadinessProbe: &corev1.Probe{
							// 	ProbeHandler: corev1.ProbeHandler{
							// 		Exec: &corev1.ExecAction{
							// 			Command: []string{"keydb-cli", "ping"},
							// 		},
							// 	},
							// 	InitialDelaySeconds: 15,
							// 	TimeoutSeconds:      1,
							// 	PeriodSeconds:       5,
							// 	SuccessThreshold:    1,
							// 	FailureThreshold:    3,
							// },
							Ports: []corev1.ContainerPort{
								// default ports are taken by kind keydb because running in host network
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
								{
									Name:      "backup-restore-sidecar-utils",
									MountPath: "/utils",
								},
							},
						},
						{
							Name:    "backup-restore-sidecar",
							Image:   keyDBContainerImage,
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
									Name:      "backup-restore-sidecar-utils",
									MountPath: "/utils",
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
										Name: "backup-restore-sidecar-config-keydb",
									},
								},
							},
						},
						{
							Name: "backup-restore-sidecar-utils",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: "backup-restore-sidecar-util-keydb",
									Items: []corev1.KeyToPath{
										{
											Key:  "keydb-cluster.sh",
											Path: "keydb-cluster.sh",
										},
									},
									DefaultMode: pointer.Pointer(int32(0755)),
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

func KeyDBClusterBackingResources(namespace string) []client.Object {
	return []client.Object{
		&corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ConfigMap",
				APIVersion: corev1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "backup-restore-sidecar-config-keydb",
				Namespace: namespace,
			},
			Data: map[string]string{
				"config.yaml": `---
bind-addr: 0.0.0.0
db: keydb
db-data-directory: /data/
backup-provider: local
backup-cron-schedule: "*/1 * * * *"
object-prefix: keydb-test
redis-addr: localhost:6379
post-exec-cmds:
- /utils/keydb-cluster.sh
`,
			},
		},

		&corev1.Secret{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Secret",
				APIVersion: corev1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "backup-restore-sidecar-util-keydb",
				Namespace: namespace,
			},
			StringData: map[string]string{
				"keydb-cluster.sh": fmt.Sprintf(`#!/bin/bash
set -ex

# FIXME: Hostname is actually backup-restore-sidecar-control-plane
env
replicas=()

for node in {0..2}; do
  if [ "${MY_POD_NAME}" != "keydb-${node}" ]; then
	  replicas+=("--replicaof keydb-${node}-headless.keydb.%s.svc.cluster.local 6379")
  fi
done
keydb-server /etc/keydb/redis.conf \
	--active-replica yes \
	--multi-master yes \
	--appendonly no \
	--bind "0.0.0.0" \
	--port 6379 \
	--protected-mode no \
	"${replicas[@]}"
`, namespace),
			},
		},
		&corev1.Service{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Service",
				APIVersion: corev1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "keydb-headless",
				Namespace: namespace,
			},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeClusterIP,
				Ports: []corev1.ServicePort{
					{
						Name:       "server",
						Port:       6379,
						Protocol:   corev1.ProtocolTCP,
						TargetPort: intstr.FromString("keydb"),
					},
				},
				Selector: map[string]string{
					"app": "keydb",
				},
			},
		},
	}
}
