package webhook

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
)

var (
	MountPathProxy     = "/dbproxy"
	VolumeNameProxy    = "dbproxydsn"
	ContainerNameProxy = "dbproxy"
)

func mutatePodProxy(ctx context.Context, pod *corev1.Pod, secretName string, dbProxyImg string) error {

	if pod.Annotations == nil {
		pod.Annotations = map[string]string{}
	}

	// Process volume, check if existing
	var foundVolume bool
	for _, v := range pod.Spec.Volumes {
		if v.Name == VolumeNameProxy {
			foundVolume = true
			continue
		}
	}

	if !foundVolume {
		pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
			Name: VolumeNameProxy,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: secretName,
				},
			},
		})
	}

	var foundContainer bool
	for _, c := range pod.Spec.Containers {
		if c.Name == ContainerNameProxy {
			foundContainer = true
			break
		}
	}

	if foundContainer {
		pod.Annotations[AnnotationInjectedProxy] = "true"
		return nil
	}

	pod.Spec.Containers = append(pod.Spec.Containers, corev1.Container{
		Name:            ContainerNameProxy,
		Image:           dbProxyImg,
		ImagePullPolicy: corev1.PullIfNotPresent,

		Env: []corev1.EnvVar{
			{
				Name:  "DBPROXY_CREDENTIAL",
				Value: fmt.Sprintf("/dbproxy/%s", SecretKey),
			},
		},
		// Test pgbouncer
		ReadinessProbe: &corev1.Probe{

			ProbeHandler: corev1.ProbeHandler{
				Exec: &corev1.ExecAction{
					Command: []string{
						"/bin/sh",
						"-c",
						"psql -h localhost -c \"SELECT 1\"",
					},
				},
			},
			InitialDelaySeconds: 5,
			PeriodSeconds:       15,
			TimeoutSeconds:      5,
		},
		// FIXME: turn these back on when timeouts can be tuned. It was restarting
		// the pod too often.
		// Test connection to upstream database
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				Exec: &corev1.ExecAction{
					Command: []string{
						"/bin/sh",
						"-c",
						fmt.Sprintf("psql \"$(cat /dbproxy/%s)\" -c \"SELECT 1\"", SecretKey),
					},
				},
			},
			InitialDelaySeconds: 30,
			PeriodSeconds:       15,
			TimeoutSeconds:      5,
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      VolumeNameProxy,
				MountPath: MountPathProxy,
				ReadOnly:  true,
			},
		},
	})

	pod.Annotations[AnnotationInjectedProxy] = "true"

	return nil
}
