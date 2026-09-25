/*
Copyright 2026 UnMango.

Licensed under the MIT License. See LICENSE in the project root for details.
*/

package controller

import (
	"fmt"
	"maps"
	"path"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	enclavev1alpha1 "github.com/unmango/enclave/api/v1alpha1"
)

const (
	// DefaultGitImage runs the clone init container.
	DefaultGitImage = "docker.io/alpine/git:v2.54.0"

	workspaceVolume  = "enclave-workspace"
	claimVolume      = "enclave-claim"
	cloneContainer   = "enclave-clone"
	cloneTmpVolume   = "enclave-clone-tmp"
	gitCredsMountDir = "/var/run/enclave/git"
)

func claimSecretName(enclave *enclavev1alpha1.Enclave) string {
	return enclave.Name + "-claim"
}

func workspacePVCName(enclave *enclavev1alpha1.Enclave) string {
	return enclave.Name + "-workspace"
}

func workspaceMountPath(env *enclavev1alpha1.EnvironmentSpec) string {
	if env.Workspace == nil || env.Workspace.MountPath == "" {
		return enclavev1alpha1.DefaultWorkspaceMountPath
	}
	return env.Workspace.MountPath
}

// buildPod renders the Pod for an Enclave: the user's template plus the
// workspace and claim volumes, and a clone init container when repositories
// are listed.
func buildPod(enclave *enclavev1alpha1.Enclave, gitImage string) *corev1.Pod {
	env := &enclave.Spec.EnvironmentSpec
	tmpl := env.Template.DeepCopy()

	labels := map[string]string{}
	maps.Copy(labels, tmpl.Labels)
	labels[enclavev1alpha1.EnclaveLabel] = enclave.Name

	pod := &corev1.Pod{
		Name:        enclave.Name,
		Namespace:   enclave.Namespace,
		Labels:      labels,
		Annotations: tmpl.Annotations,
		Spec:        tmpl.Spec,
	}

	mountPath := workspaceMountPath(env)
	pod.Spec.Volumes = append(pod.Spec.Volumes, workspaceVolumeFor(enclave), corev1.Volume{
		Name:   claimVolume,
		Secret: &corev1.SecretVolumeSource{SecretName: claimSecretName(enclave)},
	})

	mounts := []corev1.VolumeMount{
		{Name: workspaceVolume, MountPath: mountPath},
		{Name: claimVolume, MountPath: enclavev1alpha1.ClaimMountPath, ReadOnly: true},
	}
	for i := range pod.Spec.InitContainers {
		c := &pod.Spec.InitContainers[i]
		c.VolumeMounts = append(c.VolumeMounts, mounts...)
	}
	for i := range pod.Spec.Containers {
		c := &pod.Spec.Containers[i]
		c.VolumeMounts = append(c.VolumeMounts, mounts...)
	}

	if env.ServiceAccount != nil {
		pod.Spec.ServiceAccountName = enclave.Name
	}

	if len(env.Repositories) > 0 {
		clone, volumes := cloneInitContainer(env, mountPath, gitImage)
		if len(pod.Spec.Containers) > 0 {
			clone.SecurityContext = pod.Spec.Containers[0].SecurityContext.DeepCopy()
		}
		pod.Spec.InitContainers = append([]corev1.Container{clone}, pod.Spec.InitContainers...)
		pod.Spec.Volumes = append(pod.Spec.Volumes, volumes...)
	}

	return pod
}

func workspaceVolumeFor(enclave *enclavev1alpha1.Enclave) corev1.Volume {
	ws := enclave.Spec.Workspace
	if ws == nil || ws.Storage == nil {
		return corev1.Volume{
			Name:     workspaceVolume,
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		}
	}
	return corev1.Volume{
		Name:                  workspaceVolume,
		PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: workspacePVCName(enclave)},
	}
}

// cloneScript clones each repository whose destination has no .git directory.
// Repository values reach the script through the environment, so a URL or ref
// is never interpreted by the shell.
const cloneScript = `set -eu
i=0
while [ "$i" -lt "$REPO_COUNT" ]; do
  eval "url=\$REPO_${i}_URL ref=\$REPO_${i}_REF dest=\$REPO_${i}_DEST creds=\$REPO_${i}_CREDS"
  i=$((i + 1))
  if [ -d "$dest/.git" ]; then
    echo "Skipping $url, $dest already exists"
    continue
  fi
  set --
  if [ -n "$ref" ]; then set -- --branch "$ref"; fi
  if [ -f "$creds/ssh-privatekey" ]; then
    install -m 0600 "$creds/ssh-privatekey" /tmp/ssh-key
    export GIT_SSH_COMMAND="ssh -i /tmp/ssh-key -o StrictHostKeyChecking=accept-new -o UserKnownHostsFile=/tmp/known_hosts"
  else
    unset GIT_SSH_COMMAND
  fi
  if [ -f "$creds/password" ]; then
    set -- "$@" -c "credential.helper=!f() { echo username=\$(cat $creds/username); echo password=\$(cat $creds/password); }; f"
  fi
  mkdir -p "$(dirname "$dest")"
  git clone "$@" -- "$url" "$dest"
done
`

func cloneInitContainer(env *enclavev1alpha1.EnvironmentSpec, mountPath, gitImage string) (corev1.Container, []corev1.Volume) {
	vars := make([]corev1.EnvVar, 0, 2+4*len(env.Repositories))
	vars = append(vars,
		corev1.EnvVar{Name: "HOME", Value: "/tmp"},
		corev1.EnvVar{Name: "REPO_COUNT", Value: fmt.Sprint(len(env.Repositories))},
	)
	// The clone writes SSH keys and known_hosts under /tmp, which has to stay
	// writable when the copied securityContext sets readOnlyRootFilesystem.
	mounts := []corev1.VolumeMount{
		{Name: cloneTmpVolume, MountPath: "/tmp"},
		{Name: workspaceVolume, MountPath: mountPath},
	}
	volumes := []corev1.Volume{{
		Name:     cloneTmpVolume,
		EmptyDir: &corev1.EmptyDirVolumeSource{},
	}}

	for i, repo := range env.Repositories {
		dest := repo.Path
		if dest == "" {
			dest = repo.Name
		}
		creds := ""
		if repo.CredentialsSecretRef != nil {
			vol := fmt.Sprintf("enclave-git-%d", i)
			creds = path.Join(gitCredsMountDir, repo.Name)
			volumes = append(volumes, corev1.Volume{
				Name: vol,
				Secret: &corev1.SecretVolumeSource{
					SecretName:  repo.CredentialsSecretRef.Name,
					DefaultMode: ptr.To[int32](0o444),
				},
			})
			mounts = append(mounts, corev1.VolumeMount{Name: vol, MountPath: creds, ReadOnly: true})
		}
		prefix := fmt.Sprintf("REPO_%d_", i)
		vars = append(vars,
			corev1.EnvVar{Name: prefix + "URL", Value: repo.URL},
			corev1.EnvVar{Name: prefix + "REF", Value: repo.Ref},
			corev1.EnvVar{Name: prefix + "DEST", Value: path.Join(mountPath, dest)},
			corev1.EnvVar{Name: prefix + "CREDS", Value: creds},
		)
	}

	return corev1.Container{
		Name:         cloneContainer,
		Image:        gitImage,
		Command:      []string{"/bin/sh", "-c", strings.TrimSpace(cloneScript)},
		Env:          vars,
		VolumeMounts: mounts,
	}, volumes
}
