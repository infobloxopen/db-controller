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
	LabelCheckProxy         = "persistance.atlas.infoblox.com/dbproxy"
	LabelCheckExec          = "persistance.atlas.infoblox.com/dsnexec"
	LabelConfigExec         = "persistance.atlas.infoblox.com/dsnexec-config"
	LabelClaim              = "persistance.atlas.infoblox.com/claim"
	LabelClass              = "persistance.atlas.infoblox.com/class"
	AnnotationInjectedProxy = "persistance.atlas.infoblox.com/injected-dbproxy"
	AnnotationInjectedExec  = "persistance.atlas.infoblox.com/injected-dsnexec"
	SecretKey               = v1.DSNURIKey
)

// +kubebuilder:webhook:path=/mutate--v1-pod,mutating=true,failurePolicy=fail,groups="",resources=pods,verbs=create;update,versions=v1,name=dbproxy.persistance.atlas.infoblox.com,sideEffects=None,admissionReviewVersions=v1,timeoutSeconds=10,reinvocationPolicy=IfNeeded
// +kubebuilder:webhook:path=/mutate--v1-pod,mutating=true,failurePolicy=fail,groups="",resources=pods,verbs=create;update,versions=v1,name=dsnexec.persistance.atlas.infoblox.com,sideEffects=None,admissionReviewVersions=v1,timeoutSeconds=10,reinvocationPolicy=IfNeeded

// (for webhooks the path is of format /mutate-<group>-<version>-<kind>. Since this documentation uses Pod from the core API group, the group needs to be an empty string).

// podAnnotator annotates Pods
type podAnnotator struct {
	class      string
	namespace  string
	dbProxyImg string
	dsnExecImg string
	k8sClient  client.Reader
}

type SetupConfig struct {
	Namespace  string
	Class      string
	DBProxyImg string
	DSNExecImg string
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

	if cfg.DSNExecImg == "" {
		return fmt.Errorf("dsnexec image must be set")
	}

	return ctrl.NewWebhookManagedBy(mgr).
		For(&corev1.Pod{}).
		WithDefaulter(&podAnnotator{
			namespace:  cfg.Namespace,
			class:      cfg.Class,
			dbProxyImg: cfg.DBProxyImg,
			dsnExecImg: cfg.DSNExecImg,
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

	log := logf.FromContext(ctx).WithName("defaulter").WithValues("pod", nn)
	log.Info("processing")

	if pod.Labels == nil || len(pod.Labels[LabelClaim]) == 0 {
		log.Info("Skipping Pod")
		return nil
	}

	claimName := pod.Labels[LabelClaim]

	var claim v1.DatabaseClaim
	if err := p.k8sClient.Get(ctx, types.NamespacedName{Namespace: pod.Namespace, Name: claimName}, &claim); err != nil {
		//log.Info("unable to find databaseclaim", "claimName", claimName, "error", err)
		// return fmt.Errorf("unable to find databaseclaim %q check label %s: %s", claimName, LabelClaim, err)
	}
	var role v1.DbRoleClaim
	if rErr := p.k8sClient.Get(ctx, types.NamespacedName{Namespace: pod.Namespace, Name: claimName}, &role); rErr != nil {
		//log.Info("unable to find roleclaim", "claimName", claimName, "error", err)
		// return fmt.Errorf("unable to find databaseclaim %q check label %s: %s", claimName, LabelClaim, err)
	}

	if role.Name == "" && claim.Name == "" {
		log.Info("Skipping Pod, unable to find claim", "name", claimName)
		return fmt.Errorf("unable to locate databaseclaim/dbroleclaim %q", claimName)
	}

	class := pod.Labels[LabelClass]
	if class != p.class {
		log.Info("Skipping Pod, class mismatch", "class", class, "expected", p.class)
		return nil
	}

	secretName, err := getSecretNameClass(claim, role)
	if err != nil {
		return err
	}

	if pod.Annotations == nil {
		pod.Annotations = map[string]string{}
	}

	if pod.Labels[LabelCheckProxy] == "enabled" &&
		pod.Annotations[AnnotationInjectedProxy] != "true" {
		err = mutatePodProxy(ctx, pod, secretName, p.dbProxyImg)
		if err == nil {
			log.Info("mutated_pod_dbproxy")
		}
	} else if pod.Labels[LabelCheckExec] == "enabled" &&
		pod.Annotations[AnnotationInjectedExec] != "true" {
		err = mutatePodExec(ctx, pod, secretName, p.dsnExecImg)
		if err == nil {
			log.Info("mutated_pod_dsnexec")
		}
	} else {
		// No label found, skip
		log.Info("Skipping Pod, no matching proxy or role label found")
		return nil
	}

	if err != nil {
		log.Error(err, "failed to mutate pod")
	}

	return err
}

// getSecretNameClass returns the secret managed by db-controller and class for the claim
func getSecretNameClass(claim v1.DatabaseClaim, role v1.DbRoleClaim) (string, error) {
	var secretName string
	var claimName string
	if role.Name != "" {
		claimName = role.Name
		secretName = role.Spec.SecretName
	} else {
		claimName = claim.Name
		secretName = claim.Spec.SecretName
	}

	if secretName == "" {
		return "", fmt.Errorf("claim %q does not have secret reference", claimName)
	}
	return secretName, nil
}
