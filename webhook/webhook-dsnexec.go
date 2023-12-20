package hook

import (
	"context"
	"encoding/json"
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
	Decoder              *admission.Decoder
	DsnExecSidecarConfig *Config
}

var (
	dsnexecLog             = ctrl.Log.WithName("dsnexec-controller")
	sidecarImageForDsnExec = os.Getenv("DSNEXEC_IMAGE")
)

func dsnExecSideCarInjectionRequired(pod *corev1.Pod) (bool, string, string) {
	remoteDbSecretName, ok := pod.Annotations["infoblox.com/remote-db-dsn-secret"]
	if !ok {
		dsnexecLog.Info("remote-db-dsn-secret can not be seen in the annotations.", pod.Name, pod.Annotations)
		return false, "", ""
	}

	dsnExecConfigSecret, ok := pod.Annotations["infoblox.com/dsnexec-config-secret"]
	if !ok {
		dsnexecLog.Info("dsnexec-config-secret can not be seen in the annotations.", pod.Name, pod.Annotations)
		return false, "", ""
	}

	alreadyInjected, err := strconv.ParseBool(pod.Annotations["infoblox.com/dsnexec-injected"])

	if err == nil && alreadyInjected {
		dsnexecLog.Info("DsnExec sidecar already injected: ", pod.Name, pod.Annotations)
		return false, remoteDbSecretName, dsnExecConfigSecret
	}

	dsnexecLog.Info("DsnExec sidecar Injection required: ", pod.Name, pod.Annotations)

	return true, remoteDbSecretName, dsnExecConfigSecret
}

// DsnExecInjector adds an annotation to every incoming pods.
func (dbpi *DsnExecInjector) Handle(ctx context.Context, req admission.Request) admission.Response {
	pod := &corev1.Pod{}

	err := dbpi.Decoder.Decode(req, pod)
	if err != nil {
		dsnexecLog.Info("Sdecar-Injector: cannot decode")
		return admission.Errored(http.StatusBadRequest, err)
	}

	if pod.Annotations == nil {
		pod.Annotations = map[string]string{}
	}

	shoudInjectDsnExec, remoteDbSecretName, dsnExecConfigSecret := dsnExecSideCarInjectionRequired(pod)

	if shoudInjectDsnExec {
		dsnexecLog.Info("Injecting sidecar...")

		dbpi.DsnExecSidecarConfig.Containers[0].Image = sidecarImageForDsnExec
		dbpi.DsnExecSidecarConfig.Volumes[0].Secret.SecretName = remoteDbSecretName
		dbpi.DsnExecSidecarConfig.Volumes[1].Secret.SecretName = dsnExecConfigSecret

		pod.Spec.Volumes = append(pod.Spec.Volumes, dbpi.DsnExecSidecarConfig.Volumes...)
		pod.Spec.Containers = append(pod.Spec.Containers, dbpi.DsnExecSidecarConfig.Containers...)

		pod.Annotations["infoblox.com/dsnexec-injected"] = "true"
		shareProcessNamespace := true
		pod.Spec.ShareProcessNamespace = &shareProcessNamespace

		dsnexecLog.Info("sidecar ontainer for ", dbpi.Name, " injected.", pod.Name, pod.APIVersion)

	} else {
		dsnexecLog.Info("dsnexec sidecar not needed.", pod.Name, pod.APIVersion)
	}
	marshaledPod, err := json.Marshal(pod)

	if err != nil {
		dsnexecLog.Info("dsnexec sidecar injection: cannot marshal")
		return admission.Errored(http.StatusInternalServerError, err)
	}

	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledPod)
}
