package hook

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// DBProxyInjector annotates Pods
type DBProxyInjector struct {
	Name                 string
	Client               client.Client
	decoder              *admission.Decoder
	DBProxySidecarConfig *Config
}

var (
	dbProxyLog = ctrl.Log.WithName("dbproxy-controller")
)

type Config struct {
	Containers []corev1.Container `json:"containers"`
	Volumes    []corev1.Volume    `json:"volumes"`
}

func dbProxySideCardInjectionRequired(pod *corev1.Pod) (bool, string) {
	dbSecretPath, ok := pod.Annotations["db-secret-path"]

	if !ok {
		return false, ""
	}

	alreadyInjected, err := strconv.ParseBool(pod.Annotations["dbproxy-injected"])

	if err == nil && alreadyInjected {
		return false, dbSecretPath
	}

	dbProxyLog.Info("Should Inject: ", pod.Name, dbSecretPath)

	return true, dbSecretPath
}

// DBProxyInjector adds an annotation to every incoming pods.
func (dbpi *DBProxyInjector) Handle(ctx context.Context, req admission.Request) admission.Response {
	//dbProxyLog.Info("Sidecar config", "parsed:", dbpi.DBProxySidecarConfig)
	pod := &corev1.Pod{}

	err := dbpi.decoder.Decode(req, pod)
	if err != nil {
		dbProxyLog.Info("Sdecar-Injector: cannot decode")
		return admission.Errored(http.StatusBadRequest, err)
	}

	if pod.Annotations == nil {
		pod.Annotations = map[string]string{}
	}

	shoudInjectSidecar, dbSecretPath := dbProxySideCardInjectionRequired(pod)

	if shoudInjectSidecar {
		dbProxyLog.Info("Injecting sidecar...")

		f := func(c rune) bool {
			return c == '/'
		}

		dbSecretFields := strings.FieldsFunc(dbSecretPath, f)

		if len(dbSecretFields) == 2 {
			dbSecretRef := dbSecretFields[0]
			dbSecretItem := dbSecretFields[1]

			dbpi.DBProxySidecarConfig.Volumes[0].Secret.SecretName = dbSecretRef
			dbpi.DBProxySidecarConfig.Volumes[0].Secret.Items[0].Key = dbSecretItem

			pod.Spec.Volumes = append(pod.Spec.Volumes, dbpi.DBProxySidecarConfig.Volumes...)
			pod.Spec.Containers = append(pod.Spec.Containers, dbpi.DBProxySidecarConfig.Containers...)

			pod.Annotations["dbproxy-injected"] = "true"

			dbProxyLog.Info("sidecar ontainer for ", dbpi.Name, " injected.", pod.Name, pod.APIVersion)
		} else {
			dbProxyLog.Error(nil, "could not parse db secret path", "db-secret-path", dbSecretPath)
		}
	} else {
		dbProxyLog.Info("DB Proxy sidecar not needed.", pod.Name, pod.APIVersion)
	}

	marshaledPod, err := json.Marshal(pod)

	// dbProxyLog.Info("pod definition:", "pod", marshaledPod)

	if err != nil {
		dbProxyLog.Info("DB Proxy sidecar injection: cannot marshal")
		return admission.Errored(http.StatusInternalServerError, err)
	}

	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledPod)
}

// DBProxyInjector implements admission.DecoderInjector.
// A decoder will be automatically inj1ected.

// InjectDecoder injects the decoder.
func (dbpi *DBProxyInjector) InjectDecoder(d *admission.Decoder) error {
	dbpi.decoder = d
	return nil
}
