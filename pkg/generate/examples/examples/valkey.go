package examples

import (
	"github.com/metal-stack/backup-restore-sidecar/pkg/constants"
	"github.com/metal-stack/metal-lib/pkg/pointer"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	Valkey               = "valkey"
	valkeyContainerImage = "ghcr.io/valkey-io/valkey:8.1-alpine"
)

func ValkeySts(namespace string) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "StatefulSet",
			APIVersion: appsv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "valkey",
			Namespace: namespace,
			Labels: map[string]string{
				"app": "valkey",
			},
		},
		Spec: appsv1.StatefulSetSpec{
			ServiceName: "valkey",
			Replicas:    pointer.Pointer(int32(1)),
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
					HostNetwork:        true,
					ServiceAccountName: "valkey-backup-restore",
					Containers: []corev1.Container{
						{
							Name:            "valkey",
							Image:           valkeyContainerImage,
							ImagePullPolicy: corev1.PullIfNotPresent,
							Command:         []string{"backup-restore-sidecar", "wait"},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									Exec: &corev1.ExecAction{
										Command: []string{"valkey-cli", "ping"},
									},
								},
								InitialDelaySeconds: 15,
								TimeoutSeconds:      1,
								PeriodSeconds:       5,
								SuccessThreshold:    1,
								FailureThreshold:    3,
							},
							Env: []corev1.EnvVar{
								{
									Name: "POD_NAMESPACE",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											FieldPath: "metadata.namespace",
										},
									},
								},
								{
									Name: "POD_NAME",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											FieldPath: "metadata.name",
										},
									},
								},
								{
									Name:  "STATEFUL_NAME",
									Value: "valkey",
								},
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									Exec: &corev1.ExecAction{
										Command: []string{"valkey-cli", "ping"},
									},
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
							Env: []corev1.EnvVar{
								{
									Name: "POD_NAMESPACE",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											FieldPath: "metadata.namespace",
										},
									},
								},
								{
									Name: "POD_NAME",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											FieldPath: "metadata.name",
										},
									},
								},
								{
									Name:  "STATEFUL_NAME",
									Value: "valkey",
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
										Name: "backup-restore-sidecar-config-valkey",
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

func ValkeyBackingResources(namespace string) []client.Object {
	return []client.Object{
		&corev1.Service{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Service",
				APIVersion: corev1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "valkey",
				Namespace: namespace,
				Labels: map[string]string{
					"app": "valkey",
				},
			},
			Spec: corev1.ServiceSpec{
				ClusterIP: "None",
				Ports: []corev1.ServicePort{
					{
						Name:       "client",
						Port:       6379,
						TargetPort: intstr.FromInt(6379),
						Protocol:   corev1.ProtocolTCP,
					},
				},
				Selector: map[string]string{
					"app": "valkey",
				},
			},
		},
		&corev1.ServiceAccount{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ServiceAccount",
				APIVersion: corev1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "valkey-backup-restore",
				Namespace: namespace,
			},
		},
		&rbacv1.Role{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Role",
				APIVersion: rbacv1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "valkey-backup-restore",
				Namespace: namespace,
			},
			Rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{"coordination.k8s.io"},
					Resources: []string{"leases"},
					Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
				},
			},
		},
		&rbacv1.RoleBinding{
			TypeMeta: metav1.TypeMeta{
				Kind:       "RoleBinding",
				APIVersion: rbacv1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "valkey-backup-restore",
				Namespace: namespace,
			},
			Subjects: []rbacv1.Subject{
				{
					Kind:      "ServiceAccount",
					Name:      "valkey-backup-restore",
					Namespace: namespace,
				},
			},
			RoleRef: rbacv1.RoleRef{
				Kind:     "Role",
				Name:     "valkey-backup-restore",
				APIGroup: "rbac.authorization.k8s.io",
			},
		},
		&corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ConfigMap",
				APIVersion: corev1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "backup-restore-sidecar-config-valkey",
				Namespace: namespace,
			},
			Data: map[string]string{
				"config.yaml": `---
db: valkey
valkey-cluster-mode: false
valkey-statefulset-name: valkey

bind-addr: 0.0.0.0
db-data-directory: /data/
backup-provider: local
backup-cron-schedule: "*/1 * * * *"
object-prefix: valkey-test-${POD_NAME}
redis-addr: localhost:6379
encryption-key: "01234567891234560123456789123456"
post-exec-cmds:
  - valkey-server --port 6379 --bind 0.0.0.0
`,
			},
		},
		&corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ConfigMap",
				APIVersion: corev1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "valkey-init-script",
				Namespace: namespace,
			},
			Data: map[string]string{
				"init.sh": `#!/bin/sh
set -e

# Extract pod ordinal from hostname (valkey-0, valkey-1, etc.)
ORDINAL=$(hostname | sed 's/.*-//')

# Pod 0 is the master, others are replicas
if [ "$ORDINAL" = "0" ]; then
  echo "I am the master (pod-0)"		
  exec valkey-server --port 6379 --bind 0.0.0.0
else
  echo "I am a replica (pod-$ORDINAL), connecting to master at valkey-0.valkey.${POD_NAMESPACE}.svc.cluster.local"
  exec valkey-server --port 6379 --bind 0.0.0.0 --replicaof valkey-0.valkey.${POD_NAMESPACE}.svc.cluster.local 6379
fi
`,
			},
		},
	}
}

func ValkeyClusterSts(namespace string) *appsv1.StatefulSet {
	sts := ValkeySts(namespace)
	sts.Name = "valkey-cluster"
	sts.ObjectMeta.Labels["app"] = "valkey-cluster"
	sts.Spec.Selector.MatchLabels["app"] = "valkey-cluster"
	sts.Spec.Template.ObjectMeta.Labels["app"] = "valkey-cluster"
	sts.Spec.ServiceName = "valkey-cluster"
	sts.Spec.Replicas = pointer.Pointer(int32(3))

	for i := range sts.Spec.Template.Spec.Containers {
		if sts.Spec.Template.Spec.Containers[i].Name == "valkey" {
			sts.Spec.Template.Spec.Containers[i].VolumeMounts = append(
				sts.Spec.Template.Spec.Containers[i].VolumeMounts,
				corev1.VolumeMount{
					Name:      "init-script",
					MountPath: "/usr/local/bin/init.sh",
					SubPath:   "init.sh",
				},
			)
		}
		if sts.Spec.Template.Spec.Containers[i].Name == "backup-restore-sidecar" {
			for j := range sts.Spec.Template.Spec.Containers[i].Env {
				if sts.Spec.Template.Spec.Containers[i].Env[j].Name == "STATEFUL_NAME" {
					sts.Spec.Template.Spec.Containers[i].Env[j].Value = "valkey-cluster"
				}
			}
		}
	}

	for i := range sts.Spec.Template.Spec.Volumes {
		if sts.Spec.Template.Spec.Volumes[i].Name == "backup-restore-sidecar-config" {
			if sts.Spec.Template.Spec.Volumes[i].ConfigMap != nil {
				sts.Spec.Template.Spec.Volumes[i].ConfigMap.Name = "backup-restore-sidecar-config-valkey-cluster"
			}
		}
	}

	sts.Spec.Template.Spec.Volumes = append(sts.Spec.Template.Spec.Volumes, corev1.Volume{
		Name: "init-script",
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: "valkey-init-script-cluster",
				},
				DefaultMode: pointer.Pointer(int32(0755)),
			},
		},
	})

	return sts
}

func ValkeyClusterBackingResources(namespace string) []client.Object {
	baseResources := ValkeyBackingResources(namespace)

	for i, obj := range baseResources {
		if svc, ok := obj.(*corev1.Service); ok {
			svc.Name = "valkey-cluster"
			svc.ObjectMeta.Labels["app"] = "valkey-cluster"
			svc.Spec.Selector["app"] = "valkey-cluster"
			baseResources[i] = svc
		}
		if cm, ok := obj.(*corev1.ConfigMap); ok && cm.Name == "backup-restore-sidecar-config-valkey" {
			cm.Name = "backup-restore-sidecar-config-valkey-cluster"
			cm.Data["config.yaml"] = `---
db: valkey
valkey-cluster-mode: true
valkey-cluster-size: 3
valkey-statefulset-name: valkey-cluster

bind-addr: 0.0.0.0
db-data-directory: /data/
backup-provider: local
backup-cron-schedule: "*/1 * * * *"
object-prefix: valkey-cluster-test-${POD_NAME}
redis-addr: localhost:6379
encryption-key: "01234567891234560123456789123456"
post-exec-cmds:
  - /usr/local/bin/init.sh
`
			baseResources[i] = cm
		}
	}

	baseResources = append(baseResources, &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "valkey-init-script-cluster",
			Namespace: namespace,
		},
		Data: map[string]string{
			"init.sh": `#!/bin/sh
set -e

# Validate required environment variables
if [ -z "$POD_NAME" ]; then
  echo "ERROR: POD_NAME environment variable is not set"
  exit 1
fi

if [ -z "$POD_NAMESPACE" ]; then
  echo "ERROR: POD_NAMESPACE environment variable is not set"
  exit 1
fi

# Extract pod ordinal from POD_NAME
# Expected format: <statefulset-name>-<ordinal>
ORDINAL="${POD_NAME##*-}"

# Validate that ORDINAL is a number
case "$ORDINAL" in
  ''|*[!0-9]*)
    echo "ERROR: Could not extract valid ordinal from POD_NAME: $POD_NAME"
    exit 1
    ;;
esac

# Extract StatefulSet name by removing the ordinal suffix
# E.g., valkey-cluster-0 -> valkey-cluster
STATEFULSET_NAME="${POD_NAME%-*}"

echo "Pod ordinal: $ORDINAL"
echo "StatefulSet name: $STATEFULSET_NAME"

# Pod 0 is the master, others are replicas
if [ "$ORDINAL" -eq 0 ]; then
  echo "Starting as master (pod-0)"
  exec valkey-server --port 6379 --bind 0.0.0.0 --dir /data
else
  # Headless service DNS: <pod-name>.<service-name>.<namespace>.svc.cluster.local
  # For StatefulSet, service name typically matches StatefulSet name
  MASTER_ADDR="${STATEFULSET_NAME}-0.${STATEFULSET_NAME}.${POD_NAMESPACE}.svc.cluster.local"
  echo "Starting as replica (pod-$ORDINAL), master: $MASTER_ADDR"
  exec valkey-server --port 6379 --bind 0.0.0.0 --dir /data --replicaof "$MASTER_ADDR" 6379
fi
`,
		},
	})

	return baseResources
}
