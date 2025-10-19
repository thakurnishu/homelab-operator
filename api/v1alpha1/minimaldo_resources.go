package v1alpha1

import (
	"fmt"
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (mininaldo *MinimalDo) Construct() ([]client.Object, error) {
	var objects []client.Object

	var permissions = map[string]*int64{
		"user":  ptr.To(int64(1000)),
		"group": ptr.To(int64(2000)),
		"fs":    ptr.To(int64(3000)),
	}

	// Database Objects
	databaseObjects, err := createDatabaseObjects(mininaldo)
	if err != nil {
		return []client.Object{}, err
	}
	objects = append(objects, databaseObjects...)

	// Frontend Objects
	frontendObjects := createFrontendObjects(mininaldo, permissions)
	objects = append(objects, frontendObjects...)

	// Backend Objects
	backendObjects := createBackendObjects(mininaldo, permissions)
	objects = append(objects, backendObjects...)

	// Ingress
	ingressObjects := createIngressObjects(mininaldo)
	objects = append(objects, ingressObjects...)

	return objects, nil
}

func createDatabaseObjects(minimaldo *MinimalDo) ([]client.Object, error) {
	var databaseObjects []client.Object
	clusterName := fmt.Sprintf("%s-pg-cluster-v%d", minimaldo.Name, minimaldo.Spec.Database.PGClusterVersion)

	// Cluster Object
	clusterObject := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "postgresql.cnpg.io/v1",
			"kind":       "Cluster",
			"metadata": map[string]interface{}{
				"name":      clusterName,
				"namespace": minimaldo.Namespace,
				"labels": map[string]interface{}{
					"app.kubernetes.io/name": "minimaldo",
					"app.kubernetes.io/type": "postgres",
				},
			},
			"spec": map[string]interface{}{
				"instances": minimaldo.Spec.Database.Instances,
				"imageName": fmt.Sprintf("ghcr.io/cloudnative-pg/postgresql:%s", minimaldo.Spec.Database.PostgresVersion),
				"storage": map[string]interface{}{
					"size": minimaldo.Spec.Database.StorageSize,
				},
			},
		},
	}

	if minimaldo.Spec.Database.Backup != nil {
		spec, ok := clusterObject.Object["spec"].(map[string]interface{})
		if !ok {
			return []client.Object{}, fmt.Errorf("failed to get spec from cluster object")
		}

		// Add backup configuration
		spec["backup"] = map[string]interface{}{
			"barmanObjectStore": map[string]interface{}{
				"destinationPath": fmt.Sprintf("gs://%s/cloudnativepg/%s", minimaldo.Spec.Database.Backup.GCSBucket, minimaldo.Name),
				"googleCredentials": map[string]interface{}{
					"applicationCredentials": map[string]interface{}{
						"name": minimaldo.Spec.Database.Backup.GCPCredentials.SecretName,
						"key":  "gcsCredentials",
					},
				},
				"wal": map[string]interface{}{
					"compression": "gzip",
				},
				"data": map[string]interface{}{
					"compression": "gzip",
				},
			},
			"retentionPolicy": "14d",
		}

		// Add bootstrap configuration if needed
		if minimaldo.Spec.Database.Backup.BootStrapFromPreviousBackup {
			if minimaldo.Spec.Database.PGClusterVersion == 0 {
				return []client.Object{}, fmt.Errorf("cluster does not have previous backup")
			}

			previousVersion := minimaldo.Spec.Database.PGClusterVersion - 1
			previousClusterName := fmt.Sprintf("%s-pg-cluster-v%d", minimaldo.Name, previousVersion)

			spec["bootstrap"] = map[string]interface{}{
				"recovery": map[string]interface{}{
					"source": "clusterBackup",
				},
			}

			spec["externalClusters"] = []interface{}{
				map[string]interface{}{
					"name": "clusterBackup",
					"barmanObjectStore": map[string]interface{}{
						"destinationPath": fmt.Sprintf("gs://%s/cloudnativepg/%s", minimaldo.Spec.Database.Backup.GCSBucket, minimaldo.Name),
						"serverName":      previousClusterName,
						"googleCredentials": map[string]interface{}{
							"applicationCredentials": map[string]interface{}{
								"name": minimaldo.Spec.Database.Backup.GCPCredentials.SecretName,
								"key":  "gcsCredentials",
							},
						},
					},
				},
			}
		}

		// Backup Schedule
		if minimaldo.Spec.Database.Backup.Schedule != "" {
			scheduledBackup := &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "postgresql.cnpg.io/v1",
					"kind":       "ScheduledBackup",
					"metadata": map[string]interface{}{
						"name":      fmt.Sprintf("%s-scheduled-backup", minimaldo.Name),
						"namespace": minimaldo.Namespace,
						"labels": map[string]interface{}{
							"app.kubernetes.io/name": "minimaldo",
							"app.kubernetes.io/type": "postgres-scheduled-backup",
						},
					},
					"spec": map[string]interface{}{
						"immediate":            true,
						"schedule":             minimaldo.Spec.Database.Backup.Schedule,
						"backupOwnerReference": "cluster",
						"cluster": map[string]interface{}{
							"name": clusterName,
						},
					},
				},
			}
			databaseObjects = append(databaseObjects, scheduledBackup)
		}
	}

	databaseObjects = append(databaseObjects, clusterObject)
	return databaseObjects, nil
}

func createIngressObjects(minimaldo *MinimalDo) []client.Object {
	var ingressObjects []client.Object

	pathToSvc := map[string]string{
		"/api": fmt.Sprintf("%s-backend-svc", minimaldo.Name),
		"/":    fmt.Sprintf("%s-frontend-svc", minimaldo.Name),
	}

	paths := make([]networkingv1.HTTPIngressPath, 0, len(pathToSvc))
	for path, service_name := range pathToSvc {
		temp := networkingv1.HTTPIngressPath{
			Path:     path,
			PathType: ptr.To(networkingv1.PathTypePrefix),
			Backend: networkingv1.IngressBackend{
				Service: &networkingv1.IngressServiceBackend{
					Name: service_name,
					Port: networkingv1.ServiceBackendPort{
						Number: 80,
					},
				},
			},
		}
		paths = append(paths, temp)
	}

	ingress := &networkingv1.Ingress{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "networking.k8s.io/v1",
			Kind:       "Ingress",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-ingress", minimaldo.Name),
			Namespace: minimaldo.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name": "minimaldo",
				"app.kubernetes.io/type": "ingress",
			},
		},

		Spec: networkingv1.IngressSpec{
			Rules: []networkingv1.IngressRule{
				{
					Host: minimaldo.Spec.ApplicaionDomain,
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: paths,
						},
					},
				},
			},
		},
	}

	ingressObjects = append(ingressObjects, ingress)
	return ingressObjects
}

func createBackendObjects(minimaldo *MinimalDo, permissions map[string]*int64) []client.Object {
	var backendObjects []client.Object

	pgClusterName := fmt.Sprintf("%s-pg-cluster-v%d", minimaldo.Name, minimaldo.Spec.Database.PGClusterVersion)

	// Backend Service
	backendService := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-backend-svc", minimaldo.Name),
			Namespace: minimaldo.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "minimaldo",
				"app.kubernetes.io/type":      "service",
				"app.kubernetes.io/component": "backend",
			},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app.kubernetes.io/name":      "minimaldo",
				"app.kubernetes.io/type":      "deployment",
				"app.kubernetes.io/component": "backend",
			},
			Ports: []corev1.ServicePort{
				{
					Protocol:   corev1.ProtocolTCP,
					Port:       80,
					TargetPort: intstr.FromInt(8080),
				},
			},
			Type: corev1.ServiceTypeClusterIP,
		},
	}
	backendObjects = append(backendObjects, backendService)

	backendDBSecretRef := map[string]string{
		"DB_HOST":     "host",
		"DB_PORT":     "port",
		"DB_USER":     "username",
		"DB_PASSWORD": "password",
		"DB_NAME":     "dbname",
	}
	backendValueEns := map[string]string{
		"PORT":                             "8080",
		"FRONTEND_URL":                     fmt.Sprintf("https://%s", minimaldo.Spec.ApplicaionDomain),
		"APP_NAME":                         "MinimalDo",
		"OTEL_EXPORTER_OTLP_ENDPOINT_GRPC": "otel-collector-opentelemetry-collector.open-telemetry.svc.cluster.local:4317",
		"GIN_MODE":                         "release",
		"ENABLE_CONSOLE_LOG":               "true",
		"LOG_LEVEL":                        "debug",
	}

	backendEnvs := make([]corev1.EnvVar, 0, len(backendDBSecretRef)+len(backendValueEns))
	for env_name, key_name := range backendDBSecretRef {
		tempEnv := corev1.EnvVar{
			Name: env_name,
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					Key: key_name,
					LocalObjectReference: corev1.LocalObjectReference{
						Name: fmt.Sprintf("%s-app", pgClusterName),
					},
				},
			},
		}
		backendEnvs = append(backendEnvs, tempEnv)
	}

	for env_name, value := range backendValueEns {
		tempEnv := corev1.EnvVar{
			Name:  env_name,
			Value: value,
		}
		backendEnvs = append(backendEnvs, tempEnv)
	}

	// Backend Deployment
	backendDeployment := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-backend", minimaldo.Name),
			Namespace: minimaldo.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "minimaldo",
				"app.kubernetes.io/type":      "deployment",
				"app.kubernetes.io/component": "backend",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &minimaldo.Spec.Backend.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/name":      "minimaldo",
					"app.kubernetes.io/type":      "deployment",
					"app.kubernetes.io/component": "backend",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app.kubernetes.io/name":      "minimaldo",
						"app.kubernetes.io/type":      "deployment",
						"app.kubernetes.io/component": "backend",
					},
				},
				Spec: corev1.PodSpec{
					ImagePullSecrets: []corev1.LocalObjectReference{
						{Name: minimaldo.Spec.ImagePullSecretName},
					},
					SecurityContext: &corev1.PodSecurityContext{
						RunAsUser:    permissions["user"],
						RunAsGroup:   permissions["group"],
						RunAsNonRoot: ptr.To(true),
					},

					InitContainers: []corev1.Container{
						{
							Name:  "wait-for-postgres",
							Image: "busybox:1.36",
							Command: []string{
								"sh",
								"-c",
								fmt.Sprintf("until nc -z %s-rw 5432; do echo 'Waiting for PostgreSQL...'; sleep 2; done", pgClusterName),
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("10m"),
									corev1.ResourceMemory: resource.MustParse("16Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("10m"),
									corev1.ResourceMemory: resource.MustParse("16Mi"),
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:  fmt.Sprintf("%s-backend-container", minimaldo.Name),
							Image: minimaldo.Spec.Backend.Image,

							Ports: []corev1.ContainerPort{{ContainerPort: 8080}},
							Env:   backendEnvs,

							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("10m"),
									corev1.ResourceMemory: resource.MustParse("16Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("20m"),
									corev1.ResourceMemory: resource.MustParse("32Mi"),
								},
							},

							SecurityContext: &corev1.SecurityContext{
								AllowPrivilegeEscalation: ptr.To(false),
								Capabilities: &corev1.Capabilities{
									Drop: []corev1.Capability{"ALL"},
								},
								ReadOnlyRootFilesystem: ptr.To(true),
							},

							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/api/health",
										Port: intstr.FromInt(8080),
									},
								},
								InitialDelaySeconds: 30,
								PeriodSeconds:       10,
								TimeoutSeconds:      5,
								FailureThreshold:    3,
							},

							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/api/health",
										Port: intstr.FromInt(8080),
									},
								},
								InitialDelaySeconds: 5,
								PeriodSeconds:       5,
								TimeoutSeconds:      3,
								FailureThreshold:    3,
							},
						},
					},
				},
			},
		},
	}
	backendObjects = append(backendObjects, backendDeployment)
	return backendObjects
}

func createFrontendObjects(minimaldo *MinimalDo, permissions map[string]*int64) []client.Object {
	var frontendObjects []client.Object

	// Frontend Service
	frontendService := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-frontend-svc", minimaldo.Name),
			Namespace: minimaldo.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "minimaldo",
				"app.kubernetes.io/type":      "service",
				"app.kubernetes.io/component": "frontend",
			},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app.kubernetes.io/name":      "minimaldo",
				"app.kubernetes.io/type":      "deployment",
				"app.kubernetes.io/component": "frontend",
			},
			Ports: []corev1.ServicePort{
				{
					Protocol:   corev1.ProtocolTCP,
					Port:       80,
					TargetPort: intstr.FromInt(80),
				},
			},
			Type: corev1.ServiceTypeClusterIP,
		},
	}
	frontendObjects = append(frontendObjects, frontendService)

	// Frontend Deployment
	frontendDeployment := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-frontend", minimaldo.Name),
			Namespace: minimaldo.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "minimaldo",
				"app.kubernetes.io/type":      "deployment",
				"app.kubernetes.io/component": "frontend",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &minimaldo.Spec.Frontend.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/name":      "minimaldo",
					"app.kubernetes.io/type":      "deployment",
					"app.kubernetes.io/component": "frontend",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app.kubernetes.io/name":      "minimaldo",
						"app.kubernetes.io/type":      "deployment",
						"app.kubernetes.io/component": "frontend",
					},
				},
				Spec: corev1.PodSpec{
					ImagePullSecrets: []corev1.LocalObjectReference{
						{Name: minimaldo.Spec.ImagePullSecretName},
					},
					SecurityContext: &corev1.PodSecurityContext{
						FSGroup: permissions["fs"],
					},

					Volumes: []corev1.Volume{
						{
							Name: "env-file",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
						{
							Name: "nginx-cache",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
						{
							Name: "run",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},

					InitContainers: []corev1.Container{
						{
							Name:  "wait-for-backend",
							Image: "busybox:1.36",
							Command: []string{
								"sh",
								"-c",
								fmt.Sprintf("until nc -z %s-backend-svc 80; do echo 'Waiting for Minimal Backend...'; sleep 2; done", minimaldo.Name),
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("10m"),
									corev1.ResourceMemory: resource.MustParse("16Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("10m"),
									corev1.ResourceMemory: resource.MustParse("16Mi"),
								},
							},
						},
						{
							Name:  "init-create-writable",
							Image: "busybox:1.36",
							Command: []string{
								"sh",
								"-c",
								fmt.Sprintf(`
								mkdir -p /work;
								touch /work/env-config.js;
								mkdir -p /cache;
								mkdir -p /run
								chown -R %s:%s /work /cache /run;
								`, strconv.Itoa(int(*permissions["user"])), strconv.Itoa(int(*permissions["group"]))),
							},
							SecurityContext: &corev1.SecurityContext{
								RunAsUser:  ptr.To(int64(0)),
								RunAsGroup: ptr.To(int64(0)),
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "nginx-cache", MountPath: "/cache"},
								{Name: "env-file", MountPath: "/work"},
								{Name: "run", MountPath: "/run"},
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("10m"),
									corev1.ResourceMemory: resource.MustParse("16Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("10m"),
									corev1.ResourceMemory: resource.MustParse("16Mi"),
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:  fmt.Sprintf("%s-frontend-container", minimaldo.Name),
							Image: minimaldo.Spec.Frontend.Image,

							Ports: []corev1.ContainerPort{{ContainerPort: 80}},
							Env: []corev1.EnvVar{
								{
									Name:  "REACT_APP_API_URL",
									Value: fmt.Sprintf("https://%s/api", minimaldo.Spec.ApplicaionDomain),
								},
							},

							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("10m"),
									corev1.ResourceMemory: resource.MustParse("16Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("20m"),
									corev1.ResourceMemory: resource.MustParse("32Mi"),
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "env-file",
									MountPath: "/usr/share/nginx/html/env-config.js",
									SubPath:   "env-config.js",
								},
								{
									Name:      "nginx-cache",
									MountPath: "/var/cache/nginx",
								},
								{
									Name:      "run",
									MountPath: "/run",
								},
							},

							SecurityContext: &corev1.SecurityContext{
								RunAsUser:  permissions["user"],
								RunAsGroup: permissions["group"],

								AllowPrivilegeEscalation: ptr.To(false),
								Capabilities: &corev1.Capabilities{
									Drop: []corev1.Capability{"ALL"},
								},
								ReadOnlyRootFilesystem: ptr.To(true),
							},

							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/",
										Port: intstr.FromInt(80),
									},
								},
								InitialDelaySeconds: 30,
								PeriodSeconds:       10,
								TimeoutSeconds:      5,
								FailureThreshold:    3,
							},

							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/",
										Port: intstr.FromInt(80),
									},
								},
								InitialDelaySeconds: 5,
								PeriodSeconds:       5,
								TimeoutSeconds:      3,
								FailureThreshold:    3,
							},
						},
					},
				},
			},
		},
	}
	frontendObjects = append(frontendObjects, frontendDeployment)
	return frontendObjects
}
