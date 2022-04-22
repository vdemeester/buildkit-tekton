package files

import (
	"github.com/moby/buildkit/client/llb"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
)

func Secret(secret *corev1.Secret, secretSource *corev1.SecretVolumeSource) (llb.State, error) {
	state := llb.Scratch().Dir("/")
	if len(secretSource.Items) == 0 {
		for name, value := range secret.Data {
			state = addSecret(state, secret.Name, name, value)
		}
	} else {
		for _, item := range secretSource.Items {
			value, ok := secret.Data[item.Key]
			if !ok {
				return llb.State{}, errors.Errorf("key %s from secret %s not found in context", item.Key, secret.Name)
			}
			state = addSecret(state, secret.Name, item.Path, value)
		}
	}
	return state, nil
}

func addSecret(state llb.State, secretName, name string, value []byte) llb.State {
	return state.File(
		llb.Mkfile(name, 0755, value),
		llb.WithCustomName("[tekton] secret "+secretName+"/"+name+": preparing file"),
	)
}
