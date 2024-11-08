package csidriverlvm

import (
	"context"
	"fmt"
	"time"

	"github.com/gardener/gardener/extensions/pkg/controller/extension"

	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/gardener/gardener/pkg/client/kubernetes"
	"github.com/gardener/gardener/pkg/utils/managedresources"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/metal-stack/gardener-extension-csi-driver-lvm/pkg/apis/config"
	"github.com/metal-stack/gardener-extension-csi-driver-lvm/pkg/apis/csidriverlvm/v1alpha1"
	"github.com/metal-stack/gardener-extension-csi-driver-lvm/pkg/imagevector"
	"github.com/metal-stack/metal-lib/pkg/pointer"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	namespace string = "kube-system"
)

// NewActuator returns an actuator responsible for Extension resources.
func NewActuator(mgr manager.Manager, config config.ControllerConfiguration) extension.Actuator {
	return &actuator{
		client:  mgr.GetClient(),
		decoder: serializer.NewCodecFactory(mgr.GetScheme(), serializer.EnableStrict).UniversalDecoder(),
		config:  config,
	}
}

type actuator struct {
	client  client.Client
	decoder runtime.Decoder
	config  config.ControllerConfiguration
}

// Reconcile the Extension resource.
func (a *actuator) Reconcile(ctx context.Context, log logr.Logger, ex *extensionsv1alpha1.Extension) error {
	csidriverlvmConfig := &v1alpha1.CsiDriverLvmConfig{}
	if ex.Spec.ProviderConfig != nil {
		_, _, err := a.decoder.Decode(ex.Spec.ProviderConfig.Raw, nil, csidriverlvmConfig)
		if err != nil {
			return fmt.Errorf("failed to decode provider config: %w", err)
		}
	}

	var hostwritepath = csidriverlvmConfig.HostWritePath
	var devicepattern = csidriverlvmConfig.DevicePattern

	if hostwritepath == nil {
		csidriverlvmConfig.HostWritePath = a.config.DefaultHostWritePath
	}

	if devicepattern == nil {
		csidriverlvmConfig.DevicePattern = a.config.DefaultDevicePattern
	}

	controllerObjects, err := a.controllerObjects(namespace)
	if err != nil {
		return err
	}

	pluginObjects, err := a.pluginObjects(namespace, csidriverlvmConfig, log)
	if err != nil {
		return err
	}

	objects := []client.Object{}
	objects = append(objects, controllerObjects...)
	objects = append(objects, pluginObjects...)

	seedResources, err := managedresources.NewRegistry(kubernetes.SeedScheme, kubernetes.SeedCodec, kubernetes.SeedSerializer).AddAllAndSerialize(objects...)
	if err != nil {
		return err
	}

	err = managedresources.CreateForSeed(ctx, a.client, namespace, v1alpha1.SeedCsiDriverLvmResourceName, false, seedResources)

	if err != nil {
		return nil
	}

	log.Info("managed resource created succesfully", "name", v1alpha1.SeedCsiDriverLvmResourceName)

	return nil
}

// Delete the Extension resource.
func (a *actuator) Delete(ctx context.Context, log logr.Logger, ex *extensionsv1alpha1.Extension) error {

	err := managedresources.Delete(ctx, a.client, namespace, v1alpha1.SeedCsiDriverLvmResourceName, false)

	if err != nil {
		return err
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	err = managedresources.WaitUntilDeleted(timeoutCtx, a.client, namespace, v1alpha1.SeedCsiDriverLvmResourceName)
	if err != nil {
		return err
	}

	return nil
}

// ForceDelete the Extension resource
func (a *actuator) ForceDelete(_ context.Context, _ logr.Logger, _ *extensionsv1alpha1.Extension) error {
	return nil
}

// Restore the Extension resource.
func (a *actuator) Restore(ctx context.Context, log logr.Logger, ex *extensionsv1alpha1.Extension) error {
	return a.Reconcile(ctx, log, ex)
}

// Migrate the Extension resource.
func (a *actuator) Migrate(ctx context.Context, log logr.Logger, ex *extensionsv1alpha1.Extension) error {
	return nil
}

func (a *actuator) controllerObjects(namespace string) ([]client.Object, error) {

	csidriverlvmServiceAccountController := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "csi-driver-lvm-controller",
			Namespace: namespace,
		},
	}

	csidriverlvmClusterRoleController := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "csi-driver-lvm-controller",
			Namespace: namespace,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"persistentvolumes"},
				Verbs:     []string{"get", "list", "watch", "update", "patch", "create", "delete"},
			},
			{
				APIGroups: []string{"storage.k8s.io"},
				Resources: []string{"csinodes"},
				Verbs:     []string{"get", "list", "watch"},
			},
			{
				APIGroups: []string{"storage.k8s.io"},
				Resources: []string{"volumeattachments"},
				Verbs:     []string{"get", "list", "watch", "update", "patch"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"persistentvolumeclaims"},
				Verbs:     []string{"get", "list", "watch", "update", "patch"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"persistentvolumeclaims/status"},
				Verbs:     []string{"update", "patch"},
			},
			{
				APIGroups: []string{"storage.k8s.io"},
				Resources: []string{"storageclaess"},
				Verbs:     []string{"get", "list", "watch"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"events"},
				Verbs:     []string{"get", "list", "watch", "update", "patch", "create", "delete"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"nodes"},
				Verbs:     []string{"get", "list", "watch"},
			},
			{
				APIGroups: []string{"storage.k8s.io"},
				Resources: []string{"volumeattachments/status"},
				Verbs:     []string{"patch"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"pods"},
				Verbs:     []string{"get", "list", "watch"},
			},
		},
	}

	csidriverlvmClusterRoleBindingController := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "csi-driver-lvm-controller",
			Namespace: namespace,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      "csi-driver-lvm-controller",
				Namespace: namespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     "csi-driver-lvm-controller",
		},
	}

	csiAttacherImage, err := imagevector.ImageVector().FindImage("csi-attacher")
	if err != nil {
		return nil, fmt.Errorf("failed to find csi-attacher image: %w", err)
	}

	csiResizerImage, err := imagevector.ImageVector().FindImage("csi-resizer")
	if err != nil {
		return nil, fmt.Errorf("failed to find csi-resizer image: %w", err)
	}

	var hostPathType corev1.HostPathType = corev1.HostPathDirectoryOrCreate
	var replicas = int32(1)

	csidriverlvmStatefulsetController := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "csi-driver-lvm-controller",
			Namespace:   namespace,
			Annotations: map[string]string{},
			Labels:      map[string]string{},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas:    &replicas,
			ServiceName: "csi-driver-lvm-controller",
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "csi-driver-lvm-controller",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "csi-driver-lvm",
					},
					Annotations: map[string]string{},
				},
				Spec: corev1.PodSpec{
					Affinity: &corev1.Affinity{
						PodAntiAffinity: &corev1.PodAntiAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
								{
									LabelSelector: &metav1.LabelSelector{
										MatchExpressions: []metav1.LabelSelectorRequirement{
											{
												Key:      "app",
												Operator: "In",
												Values:   []string{"csi-driver-lvm-controller"},
											},
										},
									},
									TopologyKey: "kubernetes.io/hostname",
								},
							},
						},
					},
					NodeSelector:       map[string]string{},
					Tolerations:        []corev1.Toleration{},
					ServiceAccountName: "csi-driver-lvm-controller",
					Containers: []corev1.Container{
						{
							Name:            "csi-attacher",
							Image:           csiAttacherImage.String(),
							ImagePullPolicy: "IfNotPresent",
							Args:            []string{"--v=5", "--csi-address=/csi/csi.sock", "--feature-gates=Topology=true"},
							SecurityContext: &corev1.SecurityContext{
								ReadOnlyRootFilesystem: pointer.Pointer(true),
								Privileged:             pointer.Pointer(true),
							},
							VolumeMounts: []corev1.VolumeMount{
								{MountPath: "/csi", Name: "socket-dir"},
							},
						},
						{
							Name:            "csi-resizer",
							Image:           csiResizerImage.String(),
							ImagePullPolicy: "IfNotPresent",
							Args:            []string{"--v=5", "--csi-address=/csi/csi.sock"},
							SecurityContext: &corev1.SecurityContext{
								ReadOnlyRootFilesystem: pointer.Pointer(true),
								Privileged:             pointer.Pointer(true),
							},
							VolumeMounts: []corev1.VolumeMount{
								{MountPath: "/csi", Name: "socket-dir"},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "socket-dir",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/var/lib/kubelet/plugins/csi-driver-lvm",
									Type: &hostPathType,
								},
							},
						},
					},
				},
			},
		},
	}

	objects := []client.Object{
		csidriverlvmServiceAccountController,
		csidriverlvmClusterRoleController,
		csidriverlvmClusterRoleBindingController,
		csidriverlvmStatefulsetController,
	}

	return objects, nil
}

func (a *actuator) pluginObjects(namespace string, csidriverlvmConfig *v1alpha1.CsiDriverLvmConfig, log logr.Logger) ([]client.Object, error) {

	csidriverlvmDriver := &storagev1.CSIDriver{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "csi-driver-lvm",
			Namespace: namespace,
		},
		Spec: storagev1.CSIDriverSpec{
			VolumeLifecycleModes: []storagev1.VolumeLifecycleMode{"Persistent", "Ephemeral"},
			PodInfoOnMount:       pointer.Pointer(true),
			AttachRequired:       pointer.Pointer(false),
		},
	}

	csidriverlvmServiceAccountPlugin := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "csi-driver-lvm-plugin",
			Namespace: namespace,
		},
	}

	csidriverlvmClusterRolePlugin := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "csi-driver-lvm-plugin",
			Namespace: namespace,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"persistentvolumes"},
				Verbs:     []string{"get", "list", "watch", "update", "patch", "create", "delete"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"persistentvolumeclaims"},
				Verbs:     []string{"get", "list", "watch"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"persistentvolumeclaims/status"},
				Verbs:     []string{"update", "patch"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"events"},
				Verbs:     []string{"list", "watch", "update", "patch", "create"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"nodes"},
				Verbs:     []string{"get", "list", "watch"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"pods"},
				Verbs:     []string{"get", "list", "watch", "create", "delete"},
			},
		},
	}

	csidriverlvmClusterRoleBindingPlugin := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "csi-driver-lvm-plugin",
			Namespace: namespace,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      "csi-driver-lvm-plugin",
				Namespace: namespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     "csi-driver-lvm-plugin",
		},
	}

	var reclaimPolicy corev1.PersistentVolumeReclaimPolicy = corev1.PersistentVolumeReclaimDelete
	var volumeBindingMode storagev1.VolumeBindingMode = storagev1.VolumeBindingWaitForFirstConsumer

	csidriverlvmLinearStorageClass := &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "csi-driver-lvm-linear",
		},
		Provisioner:          "lvm.csi.metal-stack.io",
		ReclaimPolicy:        &reclaimPolicy,
		VolumeBindingMode:    &volumeBindingMode,
		AllowVolumeExpansion: pointer.Pointer(true),
		Parameters: map[string]string{
			"type": "linear",
		},
	}

	csidriverlvmMirrorStorageClass := &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "csi-driver-lvm-mirror",
		},
		Provisioner:          "lvm.csi.metal-stack.io",
		ReclaimPolicy:        &reclaimPolicy,
		VolumeBindingMode:    &volumeBindingMode,
		AllowVolumeExpansion: pointer.Pointer(true),
		Parameters: map[string]string{
			"type": "mirror",
		},
	}

	csidriverlvmStripedStorageClass := &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "csi-driver-lvm-striped",
		},
		Provisioner:          "lvm.csi.metal-stack.io",
		ReclaimPolicy:        &reclaimPolicy,
		VolumeBindingMode:    &volumeBindingMode,
		AllowVolumeExpansion: pointer.Pointer(true),
		Parameters: map[string]string{
			"type": "striped",
		},
	}

	csidriverlvmDefaultStorageClass := &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "csi-lvm",
		},
		Provisioner:          "lvm.csi.metal-stack.io",
		ReclaimPolicy:        &reclaimPolicy,
		VolumeBindingMode:    &volumeBindingMode,
		AllowVolumeExpansion: pointer.Pointer(true),
		Parameters: map[string]string{
			"type": "linear",
		},
	}

	csiNodeDriverRegistrarImage, err := imagevector.ImageVector().FindImage("csi-node-driver-registrar")
	if err != nil {
		return nil, fmt.Errorf("failed to find csi-node-driver-registrar image: %w", err)
	}

	livenessprobeImage, err := imagevector.ImageVector().FindImage("livenessprobe")
	if err != nil {
		return nil, fmt.Errorf("failed to find livenessprobe image: %w", err)
	}

	csiDriverLvmImage, err := imagevector.ImageVector().FindImage("csi-driver-lvm")
	if err != nil {
		return nil, fmt.Errorf("failed to find csi-driver-lvm image: %w", err)
	}

	csiDriverLvmProvisionerImage, err := imagevector.ImageVector().FindImage("csi-driver-lvm-provisioner")
	if err != nil {
		return nil, fmt.Errorf("failed to find csi-driver-lvm-provisioner image: %w", err)
	}

	// var terminationPolicy corev1.TerminationMessagePolicy = corev1.TerminationMessageReadFile
	var mountPropagation corev1.MountPropagationMode = corev1.MountPropagationBidirectional

	var hostPathTypeCreate corev1.HostPathType = corev1.HostPathDirectoryOrCreate
	var hostPathTypeDir corev1.HostPathType = corev1.HostPathDirectory
	var revisionHistoryLimit = int32(10)
	var healthPort = int32(9898)

	csidriverlvmDaemonSetPlugin := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "csi-driver-lvm-plugin",
			Namespace: namespace,
		},
		Spec: appsv1.DaemonSetSpec{
			RevisionHistoryLimit: &revisionHistoryLimit,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "csi-driver-lvm-plugin",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "csi-driver-lvm-plugin",
					},
				}, Spec: corev1.PodSpec{
					ServiceAccountName: "csi-driver-lvm-plugin",
					Tolerations:        []corev1.Toleration{},
					NodeSelector:       map[string]string{},
					Containers: []corev1.Container{
						{
							Name:            "csi-node-driver-registrar",
							Image:           csiNodeDriverRegistrarImage.String(),
							ImagePullPolicy: "IfNotPresent",
							Args:            []string{"--v=5", "--csi-address=/csi/csi.sock", "--kubelet-registration-path=/var/lib/kubelet/plugins/csi-driver-lvm/csi.sock"},
							SecurityContext: &corev1.SecurityContext{
								ReadOnlyRootFilesystem: pointer.Pointer(false),
								// Privileged:             pointer.Pointer(true),
							},
							Env: []corev1.EnvVar{
								{
									Name: "KUBE_NODE_NAME",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											APIVersion: "v1",
											FieldPath:  "spec.nodeName",
										},
									},
								},
							},
							// TerminationMessagePath:   "/dev/termination-log",
							// TerminationMessagePolicy: terminationPolicy,
							VolumeMounts: []corev1.VolumeMount{
								{MountPath: "/csi", Name: "socket-dir"},
								{MountPath: "/var/lib/kubelet/plugins/csi-driver-lvm/csi.sock", Name: "socket-dir"},
								{MountPath: "/registration", Name: "registration-dir"},
							},
						},
						{
							Name:            "csi-driver-lvm-plugin",
							Image:           csiDriverLvmImage.String(),
							ImagePullPolicy: "IfNotPresent",
							Args: []string{
								"--drivername=lvm.csi.metal-stack.io",
								"--endpoint=unix:///csi/csi.sock",
								"--hostwritepath=" + pointer.SafeDeref(csidriverlvmConfig.HostWritePath),
								"--devices=" + pointer.SafeDeref(csidriverlvmConfig.HostWritePath),
								"--nodeid=$(KUBE_NODE_NAME)",
								"--vgname=csi-lvm",
								"--namespace=kube-system",
								"--provisionerImage=" + csiDriverLvmProvisionerImage.String(),
								"--pullpolicy=IfNotPresent",
							},
							SecurityContext: &corev1.SecurityContext{
								ReadOnlyRootFilesystem: pointer.Pointer(false),
								Privileged:             pointer.Pointer(true),
							},
							Env: []corev1.EnvVar{
								{
									Name: "KUBE_NODE_NAME",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											APIVersion: "v1",
											FieldPath:  "spec.nodeName",
										},
									},
								},
							},
							LivenessProbe: &corev1.Probe{
								FailureThreshold:    5,
								InitialDelaySeconds: 10,
								PeriodSeconds:       2,
								SuccessThreshold:    1,
								TimeoutSeconds:      3,
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path:   "/healthz",
										Port:   intstr.FromInt32(healthPort),
										Scheme: corev1.URISchemeHTTP,
									},
								},
							},
							Ports: []corev1.ContainerPort{{
								Name:          "healthz",
								Protocol:      corev1.ProtocolTCP,
								ContainerPort: 9898,
							}},
							// TerminationMessagePath:   "/dev/termination-log",
							// TerminationMessagePolicy: terminationPolicy,
							VolumeMounts: []corev1.VolumeMount{
								{MountPath: "/csi", Name: "socket-dir"},
								{MountPath: "/var/lib/kubelet/pods", Name: "mountpoint-dir", MountPropagation: &mountPropagation},
								{MountPath: "/var/lib/kubelet/plugins", Name: "plugins-dir", MountPropagation: &mountPropagation},
								{MountPath: "/dev", Name: "dev-dir", MountPropagation: &mountPropagation},
								{MountPath: "/lib/modules", Name: "mod-dir"},
								{MountPath: "/etc/lvm/backup", Name: "lvmbackup", MountPropagation: &mountPropagation},
								{MountPath: "/etc/lvm/cache", Name: "lvmcache", MountPropagation: &mountPropagation},
								{MountPath: "/etc/lvm/archive", Name: "lvmarchive", MountPropagation: &mountPropagation},
								{MountPath: "/etc/lvm/lock", Name: "lvmlock", MountPropagation: &mountPropagation},
							},
						},
						{
							Name:            "livenessprobe",
							Image:           livenessprobeImage.String(),
							ImagePullPolicy: "IfNotPresent",
							Args: []string{
								"--csi-address=/csi/csi.sock",
								"--health-port=9898",
							},
							SecurityContext: &corev1.SecurityContext{
								ReadOnlyRootFilesystem: pointer.Pointer(true),
							},
							// TerminationMessagePath:   "/dev/termination-log",
							// TerminationMessagePolicy: terminationPolicy,
							VolumeMounts: []corev1.VolumeMount{
								{MountPath: "/csi", Name: "socket-dir"},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "socket-dir",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/var/lib/kubelet/plugins/csi-driver-lvm",
									Type: &hostPathTypeCreate,
								},
							},
						},
						{
							Name: "mountpoint-dir",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/var/lib/kubelet/pods",
									Type: &hostPathTypeCreate,
								},
							},
						},
						{
							Name: "registration-dir",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/var/lib/kubelet/plugins_registry",
									Type: &hostPathTypeDir,
								},
							},
						},
						{
							Name: "plugins-dir",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/var/lib/kubelet/plugins",
									Type: &hostPathTypeDir,
								},
							},
						},
						{
							Name: "dev-dir",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/dev",
									Type: &hostPathTypeDir,
								},
							},
						},
						{
							Name: "mod-dir",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/lib/modules",
								},
							},
						},
						{
							Name: "lvmcache",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: pointer.SafeDeref(csidriverlvmConfig.HostWritePath) + "/cache",
									Type: &hostPathTypeCreate,
								},
							},
						},
						{
							Name: "lvmarchive",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: pointer.SafeDeref(csidriverlvmConfig.HostWritePath) + "/archive",
									Type: &hostPathTypeCreate,
								},
							},
						},
						{
							Name: "lvmbackup",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: pointer.SafeDeref(csidriverlvmConfig.HostWritePath) + "/backup",
									Type: &hostPathTypeCreate,
								},
							},
						},
						{
							Name: "lvmlock",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: pointer.SafeDeref(csidriverlvmConfig.HostWritePath) + "/lock",
									Type: &hostPathTypeCreate,
								},
							},
						},
					},
				},
			},
		},
	}

	objects := []client.Object{
		csidriverlvmDriver,
		csidriverlvmServiceAccountPlugin,
		csidriverlvmClusterRolePlugin,
		csidriverlvmClusterRoleBindingPlugin,
		csidriverlvmDefaultStorageClass,
		csidriverlvmLinearStorageClass,
		csidriverlvmMirrorStorageClass,
		csidriverlvmStripedStorageClass,
		csidriverlvmDaemonSetPlugin,
	}

	return objects, nil
}
