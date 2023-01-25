package hook

import (
	"context"
	"encoding/json"
	"io/ioutil"
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

func ParseConfig(configFile string) (*Config, error) {
	data, err := ioutil.ReadFile(configFile)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func dbProxySideCardInjectionRequired(pod *corev1.Pod) (bool, string) {
	dbSecretPath, ok := pod.Annotations["infoblox.com/db-secret-path"]

	if !ok {
		return false, ""
	}

	alreadyInjected, err := strconv.ParseBool(pod.Annotations["infoblox.com/dbproxy-injected"])

	if err == nil && alreadyInjected {
		dbProxyLog.Info("DB Proxy sidecar already injected: ", pod.Name, dbSecretPath)
		return false, dbSecretPath
	}

	dbProxyLog.Info("DB Proxy sidecar Injection required: ", pod.Name, dbSecretPath)

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

	shoudInjectDBProxy, dbSecretPath := dbProxySideCardInjectionRequired(pod)

	if shoudInjectDBProxy {
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

			pod.Annotations["infoblox.com/dbproxy-injected"] = "true"

			dbProxyLog.Info("sidecar ontainer for ", dbpi.Name, " injected.", pod.Name, pod.APIVersion)
		} else {
			dbProxyLog.Error(nil, "could not parse db secret path", "infoblox.com/db-secret-path", dbSecretPath)
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
// InjectDecoder injects the decoder.
func (dbpi *DBProxyInjector) InjectDecoder(d *admission.Decoder) error {
	dbpi.decoder = d
	return nil
}
