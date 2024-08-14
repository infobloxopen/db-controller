package webhook

import (
	"context"
	"fmt"

	v1 "github.com/infobloxopen/db-controller/api/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var (
	// Set to {key}=disabled to disable injection
	LabelCheck         = "persistance.atlas.infoblox.com/databaseclaim"
	AnnotationInjected = "persistance.atlas.infoblox.com/injected"
	MountPath          = "/dbproxy"
	VolumeName         = "dbproxydsn"
	ContainerName      = "dbproxy"
	SecretKey          = "uri_dsn.txt"
)

// +kubebuilder:webhook:path=/mutate--v1-pod,mutating=true,failurePolicy=fail,groups="",resources=pods,verbs=create;update,versions=v1,name=persistance.atlas.infoblox.com,sideEffects=None,admissionReviewVersions=v1

// (for webhooks the path is of format /mutate-<group>-<version>-<kind>. Since this documentation uses Pod from the core API group, the group needs to be an empty string).

// podAnnotator annotates Pods
type podAnnotator struct {
	class      string
	namespace  string
	dbProxyImg string
	k8sClient  client.Reader
}

type SetupConfig struct {
	Namespace  string
	Class      string
	DBProxyImg string
}

func SetupWebhookWithManager(mgr ctrl.Manager, cfg SetupConfig) error {

	if cfg.Namespace == "" {
		return fmt.Errorf("namespace must be set")
	}

	if cfg.Class == "" {
		return fmt.Errorf("class must be set")
	}

	if cfg.DBProxyImg == "" {
		return fmt.Errorf("dbproxy image must be set")
	}

	return ctrl.NewWebhookManagedBy(mgr).
		For(&corev1.Pod{}).
		WithDefaulter(&podAnnotator{
			namespace:  cfg.Namespace,
			class:      cfg.Class,
			dbProxyImg: cfg.DBProxyImg,
			k8sClient:  mgr.GetClient(),
		}).
		Complete()
}

func (p *podAnnotator) Default(ctx context.Context, obj runtime.Object) error {

	pod, ok := obj.(*corev1.Pod)
	if !ok {
		logf.FromContext(ctx).Error(fmt.Errorf("expected a Pod but got a %T", obj), "failed to default")
		return fmt.Errorf("expected a Pod but got a %T", obj)
	}

	nn := types.NamespacedName{
		Namespace: pod.Namespace,
		Name:      pod.Name,
	}

	log := logf.FromContext(ctx).WithName("dbproxy-defaulter").WithValues("pod", nn)
	log.Info("processing")

	if pod.Labels == nil || len(pod.Labels[LabelCheck]) == 0 {
		log.Info("Skipping Pod")
		return nil
	}

	claimName := pod.Labels[LabelCheck]

	var claim v1.DatabaseClaim
	if err := p.k8sClient.Get(ctx, types.NamespacedName{Namespace: pod.Namespace, Name: claimName}, &claim); err != nil {
		log.Info("unable to find databaseclaim", "claimName", claimName, "error", err)
		return fmt.Errorf("unable to find databaseclaim %q check label %s: %w", claimName, LabelCheck, err)
	}

	// This is the secret that db-controller manages
	secretName := claim.Spec.SecretName
	dsnKey := claim.Spec.DSNName
	if dsnKey == "" {
		// TODO: link to defaultDSNKey variable
		dsnKey = SecretKey
	}

	if secretName == "" {
		return fmt.Errorf("claim %q does not have secret name, this may resolve on its own", claimName)
	}

	if *claim.Spec.Class != p.class {
		log.Info("Skipping Pod, class mismatch", "claimClass", *claim.Spec.Class, "class", p.class)
		return nil
	}

	err := mutatePod(ctx, pod, secretName, dsnKey, p.dbProxyImg)
	if err == nil {
		log.Info("mutated_pod")
	}
	return nil
}

func mutatePod(ctx context.Context, pod *corev1.Pod, secretName string, dsnKey string, dbProxyImg string) error {

	if pod.Annotations == nil {
		pod.Annotations = map[string]string{}
	}

	// Process volume, check if existing
	var foundVolume bool
	for _, v := range pod.Spec.Volumes {
		if v.Name == VolumeName {
			foundVolume = true
			continue
		}
	}

	if !foundVolume {
		pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
			Name: VolumeName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: secretName,
				},
			},
		})
	}

	var foundContainer bool
	for _, c := range pod.Spec.Containers {
		if c.Name == ContainerName {
			foundContainer = true
			break
		}
	}

	if foundContainer {
		pod.Annotations[AnnotationInjected] = "true"
		return nil
	}

	pod.Spec.Containers = append(pod.Spec.Containers, corev1.Container{
		Name:            ContainerName,
		Image:           dbProxyImg,
		ImagePullPolicy: corev1.PullIfNotPresent,

		Env: []corev1.EnvVar{
			{
				Name:  "DBPROXY_CREDENTIAL",
				Value: fmt.Sprintf("/dbproxy/%s", dsnKey),
			},
			{
				Name:  "DBPROXY_PASSWORD",
				Value: fmt.Sprintf("/dbproxy/%s", "password"),
			},
		},
		// Test pgbouncer
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				Exec: &corev1.ExecAction{
					Command: []string{"psql", "-h", "localhost", "-c", "'SELECT 1'"},
				},
			},
			InitialDelaySeconds: 30,
			PeriodSeconds:       15,
		},
		// Test connection to upstream database
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				Exec: &corev1.ExecAction{
					Command: []string{"psql", "\\$(cat /dbproxy/uri_dsn.txt)", "-c", "'SELECT 1'"},
				},
			},
			InitialDelaySeconds: 30,
			PeriodSeconds:       15,
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      VolumeName,
				MountPath: MountPath,
				ReadOnly:  true,
			},
		},
	})

	pod.Annotations[AnnotationInjected] = "true"

	return nil
}
