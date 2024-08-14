package webhook

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"os"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// DsnExecInjector annotates Pods

type DsnExecInjector struct {
	Name                 string
	Client               client.Client
	Decoder              admission.Decoder
	DsnExecSidecarConfig *Config
}

var (
	dsnexecLog             = ctrl.Log.WithName("dsnexec-controller")
	sidecarImageForDsnExec = os.Getenv("DSNEXEC_IMAGE")
)

func dsnExecSideCarInjectionRequired(pod *corev1.Pod) (bool, string, string) {
	remoteDbSecretName, ok := pod.Annotations["infoblox.com/remote-db-dsn-secret"]
	if !ok {
		return false, "", ""
	}

	dsnExecConfigSecret, ok := pod.Annotations["infoblox.com/dsnexec-config-secret"]
	if !ok {
		return false, "", ""
	}

	alreadyInjected, err := strconv.ParseBool(pod.Annotations["infoblox.com/dsnexec-injected"])

	if err == nil && alreadyInjected {
		dsnexecLog.V(1).Info("DsnExec sidecar already injected: ", pod.Name, pod.Annotations)
		return false, remoteDbSecretName, dsnExecConfigSecret
	}

	dsnexecLog.V(1).Info("DsnExec sidecar Injection required: ", pod.Name, pod.Annotations)

	return true, remoteDbSecretName, dsnExecConfigSecret
}

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

// DsnExecInjector adds an annotation to every incoming pods.
func (dbpi *DsnExecInjector) Handle(ctx context.Context, req admission.Request) admission.Response {
	pod := &corev1.Pod{}

	decoder := admission.NewDecoder(dbpi.Client.Scheme())
	err := decoder.Decode(req, pod)

	if err != nil {
		dsnexecLog.Error(err, "Sdecar-Injector: cannot decode")
		return admission.Errored(http.StatusBadRequest, err)
	}

	if pod.Annotations == nil {
		pod.Annotations = map[string]string{}
	}

	shoudInjectDsnExec, remoteDbSecretName, dsnExecConfigSecret := dsnExecSideCarInjectionRequired(pod)

	if shoudInjectDsnExec {
		dsnexecLog.V(1).Info("Injecting sidecar...")

		dbpi.DsnExecSidecarConfig.Containers[0].Image = sidecarImageForDsnExec
		dbpi.DsnExecSidecarConfig.Volumes[0].Secret.SecretName = remoteDbSecretName
		dbpi.DsnExecSidecarConfig.Volumes[1].Secret.SecretName = dsnExecConfigSecret

		pod.Spec.Volumes = append(pod.Spec.Volumes, dbpi.DsnExecSidecarConfig.Volumes...)
		pod.Spec.Containers = append(pod.Spec.Containers, dbpi.DsnExecSidecarConfig.Containers...)

		pod.Annotations["infoblox.com/dsnexec-injected"] = "true"
		shareProcessNamespace := true
		pod.Spec.ShareProcessNamespace = &shareProcessNamespace

		dsnexecLog.V(1).Info("sidecar ontainer for ", dbpi.Name, " injected.", pod.Name, pod.APIVersion)

	} else {
		dsnexecLog.V(1).Info("dsnexec sidecar not needed.", pod.Name, pod.APIVersion)
	}
	marshaledPod, err := json.Marshal(pod)

	if err != nil {
		dsnexecLog.Error(err, "dsnexec sidecar injection: cannot marshal")
		return admission.Errored(http.StatusInternalServerError, err)
	}

	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledPod)
}
