package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var (
	DeprecatedAnnotationDSNExecConfig = "infoblox.com/dsnexec-config-secret"
	DeprecatedAnnotationRemoteDBDSN   = "infoblox.com/remote-db-dsn-secret"
	DeprecatedAnnotationDBSecretPath  = "infoblox.com/db-secret-path"
	DeprecatedAnnotationMessages      = "persistance.atlas.infoblox.com/deprecation-messages"
)

// +kubebuilder:webhook:path=/convert-deprecated-pod,mutating=true,failurePolicy=fail,groups="",resources=pods,verbs=create;update,versions=v1,name=podconversion.persistance.atlas.infoblox.com,sideEffects=None,timeoutSeconds=10,admissionReviewVersions=v1

// SetupConversionWebhookWithManager converts db-controller v1 pod annotations to use
// v1.10 labels. As this needs to work on existing pods, it is not scoped to a single
// class. Be careful deploying this in a cluster with other conversion webhooks
func SetupConversionWebhookWithManager(mgr ctrl.Manager, cfg SetupConfig) error {

	if cfg.Namespace == "" {
		return fmt.Errorf("namespace must be set")
	}

	if cfg.Class == "" {
		return fmt.Errorf("class must be set")
	}

	mgr.GetWebhookServer().Register("/convert-deprecated-pod", &webhook.Admission{
		Handler: &podConverter{
			class:     cfg.Class,
			namespace: cfg.Namespace,
			k8sClient: mgr.GetClient(),
			decoder:   admission.NewDecoder(mgr.GetScheme()),
		},
	})
	return nil
}

type podConverter struct {
	class     string
	namespace string
	k8sClient client.Reader
	decoder   admission.Decoder
}

// Handle handles the conversion of deprecated annotations to labels
//
// # dsnexec
// annotation: infoblox.com/remote-db-dsn-secret: <name of claim secret>
// label:      persistance.atlas.infoblox.com/claim: <name of dbc or dbroleclaim CR>
//
// annotation: infoblox.com/dsnexec-config-secret: <name of dsnexec config secret>
// label:      persistance.atlas.infoblox.com/dsnexec-config: <name of dsnexec config secret>
//
// # dbproxy
// annotation: infoblox.com/db-secret-path: <name of claim secret>
// label:      persistance.atlas.infoblox.com/claim: <name of dbc or dbroleclaim CR>
func (p *podConverter) Handle(ctx context.Context, req admission.Request) admission.Response {

	log := logf.FromContext(ctx).WithName("conversion")

	if p.decoder == nil {
		log.Error(nil, "decoder should never be nil")
		os.Exit(1)
	}

	pod := &corev1.Pod{}
	err := p.decoder.Decode(req, pod)
	if err != nil {
		log.Error(err, "Could not decode Pod", "uid", req.UID)
		return admission.Errored(http.StatusBadRequest, err)
	}
	log = log.WithValues("pod", fmt.Sprintf("%s/%s", req.Namespace, req.Name))

	if pod.Annotations == nil {
		pod.Annotations = map[string]string{}
	}

	if pod.Labels == nil {
		pod.Labels = map[string]string{}
	}

	// Check if any of the deprecated annotations are present
	dsnExecConfigSecret := pod.Annotations[DeprecatedAnnotationDSNExecConfig]
	remoteDBDSNSecret := pod.Annotations[DeprecatedAnnotationRemoteDBDSN]
	dbSecretPath := pod.Annotations[DeprecatedAnnotationDBSecretPath]

	if dsnExecConfigSecret == "" && remoteDBDSNSecret == "" && dbSecretPath == "" {
		// This would log on every pod creation in the cluster
		// log.V(1).Info("Skipped conversion, no deprecated annotations found", "uid", req.UID)
		return admission.Allowed("Skipped conversion, no deprecated annotations found")
	}

	if err := convertPod(ctx, p.k8sClient, p.class, pod); err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	bs, err := json.Marshal(pod)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	log.Info("converted_pod")
	return admission.PatchResponseFromRaw(req.Object.Raw, bs)
}

func convertPod(ctx context.Context, reader client.Reader, class string, pod *corev1.Pod) error {

	var deprecationMsgs []string
	log := logf.FromContext(ctx)

	// Initialize these for unit tests
	if pod.Annotations == nil {
		pod.Annotations = map[string]string{}
	}

	if pod.Labels == nil {
		pod.Labels = map[string]string{}
	}

	dsnExecConfigSecret := pod.Annotations[DeprecatedAnnotationDSNExecConfig]
	remoteDBDSNSecret := pod.Annotations[DeprecatedAnnotationRemoteDBDSN]
	dbSecretPath := pod.Annotations[DeprecatedAnnotationDBSecretPath]

	secretName := remoteDBDSNSecret
	if secretName == "" {
		secretName = dbSecretPath
	}

	log = log.WithValues("secret", secretName)

	// db-secret-path has a key in it, so remove the key
	parts := strings.Split(secretName, "/")
	if len(parts) > 1 {
		secretName = parts[0]
	}

	labelConfigExec := pod.Labels[LabelConfigExec]
	if labelConfigExec == "" && dsnExecConfigSecret != "" {
		pod.Labels[LabelConfigExec] = pod.Annotations[DeprecatedAnnotationDSNExecConfig]
		pod.Labels[LabelCheckExec] = "enabled"
		deprecationMsgs = append(deprecationMsgs, fmt.Sprintf(`Label "%s" replaces annotation "%s"`, LabelConfigExec, DeprecatedAnnotationDSNExecConfig))
	}

	// Process claims label
	if pod.Labels[LabelClaim] == "" {

		if pod.Annotations[DeprecatedAnnotationRemoteDBDSN] != "" {
			deprecationMsgs = append(deprecationMsgs, fmt.Sprintf(`Label "%s" replaces annotation "%s"`, LabelClaim, DeprecatedAnnotationRemoteDBDSN))
		}
		if pod.Annotations[DeprecatedAnnotationDBSecretPath] != "" {
			deprecationMsgs = append(deprecationMsgs, fmt.Sprintf(`Label "%s" replaces annotation "%s"`, LabelClaim, DeprecatedAnnotationDBSecretPath))
		}

		var claimName string
		var err error
		if claimName, err = getClaimName(ctx, reader, pod.GetNamespace(), secretName); err != nil {
			log.Error(err, "unable to find claim")
			return err
		}

		pod.Labels[LabelClaim] = claimName
		pod.Labels[LabelClass] = class
		pod.Labels[LabelCheckProxy] = "enabled"
	}

	// Remove deprecated annotations
	delete(pod.Annotations, DeprecatedAnnotationRemoteDBDSN)
	delete(pod.Annotations, DeprecatedAnnotationDSNExecConfig)
	delete(pod.Annotations, DeprecatedAnnotationDBSecretPath)

	// Log deprecation messages
	if len(deprecationMsgs) > 0 {
		pod.Annotations[DeprecatedAnnotationMessages] = fmt.Sprintf("Warning: Deprecated annotations detected.\nSee docs for updated syntax: https://infobloxopen.github.io/db-controller/#quick-start\n%s", strings.Join(deprecationMsgs, "\n"))
	}

	return nil
}

func getClaimName(ctx context.Context, reader client.Reader, namespace, secretName string) (string, error) {
	var secret corev1.Secret
	if err := reader.Get(ctx, client.ObjectKey{Namespace: namespace, Name: secretName}, &secret); err != nil {
		return "", err
	}

	var claimName string
	for _, owner := range secret.OwnerReferences {
		if owner.Kind == "DatabaseClaim" {
			claimName = owner.Name
		}
		if owner.Kind == "DbRoleClaim" {
			claimName = owner.Name
		}
	}

	if claimName == "" {
		err := fmt.Errorf("unable to find claim owner reference")
		return "", err
	}

	return claimName, nil
}
