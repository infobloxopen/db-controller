package webhook

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
)

var (
	MountPathExec     = "/var/run/db-dsn"
	VolumeNameExec    = "db-dsn"
	ContainerNameExec = "dsnexec"

	MountPathExecConfig  = "/var/run/dsn-exec"
	VolumeNameExecConfig = "dsnexec-config"
)

func mutatePodExec(ctx context.Context, pod *corev1.Pod, secretName string, dsnExecImg string, enableReady, enableLiveness bool) error {

	if pod.Annotations == nil {
		pod.Annotations = map[string]string{}
	}

	dsnExecConfigSecret := pod.Annotations["infoblox.com/dsnexec-config-secret"]
	if dsnExecConfigSecret == "" {
		dsnExecConfigSecret = pod.Labels[LabelConfigExec]
		if dsnExecConfigSecret == "" {
			return fmt.Errorf("%s label was not found", LabelConfigExec)
		}
	}

	// Process volume, check if existing
	var foundVolume, foundCfgVolume bool
	for _, v := range pod.Spec.Volumes {
		if v.Name == VolumeNameExec {
			foundVolume = true
		} else if v.Name == VolumeNameExecConfig {
			foundCfgVolume = true
		}
	}

	if !foundVolume {
		pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
			Name: VolumeNameExec,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: secretName,
				},
			},
		})
	}

	if !foundCfgVolume {
		pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
			Name: VolumeNameExecConfig,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: dsnExecConfigSecret,
				},
			},
		})
	}

	var foundContainer bool
	for _, c := range pod.Spec.Containers {
		if c.Name == ContainerNameExec {
			foundContainer = true
			break
		}
	}

	if foundContainer {
		pod.Annotations[AnnotationInjectedExec] = "true"
		return nil
	}

	var readinessProbe *corev1.Probe
	if enableReady {
		readinessProbe = &corev1.Probe{

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
		}
	}

	var livenessProbe *corev1.Probe

	if enableLiveness {
		livenessProbe = &corev1.Probe{
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
		}
	}

	pod.Spec.Containers = append(pod.Spec.Containers, corev1.Container{
		Name:            ContainerNameExec,
		Image:           dsnExecImg,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Args: []string{
			"run",
			"--config-file",
			fmt.Sprintf("%s/config.yaml", MountPathExecConfig),
		},
		Env: []corev1.EnvVar{
			{
				Name:  "DBPROXY_CREDENTIAL",
				Value: fmt.Sprintf("%s/%s", MountPathExec, SecretKey),
			},
		},
		// Test connection to upstream database
		LivenessProbe: livenessProbe,
		// Test connection to pgbouncer
		ReadinessProbe: readinessProbe,
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      VolumeNameExec,
				MountPath: MountPathExec,
				ReadOnly:  true,
			},
			{
				Name:      VolumeNameExecConfig,
				MountPath: MountPathExecConfig,
				ReadOnly:  true,
			},
		},
	})

	pod.Annotations[AnnotationInjectedExec] = "true"

	return nil
}
