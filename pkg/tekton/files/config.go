package files

import (
	"github.com/moby/buildkit/client/llb"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
)

func ConfigMap(configmap *corev1.ConfigMap, configmapSource *corev1.ConfigMapVolumeSource) (llb.State, error) {
	state := llb.Scratch().Dir("/")
	if len(configmapSource.Items) == 0 {
		for name, value := range configmap.Data {
			state = addConfigMap(state, configmap.Name, name, value)
		}
	} else {
		for _, item := range configmapSource.Items {
			value, ok := configmap.Data[item.Key]
			if !ok {
				return llb.State{}, errors.Errorf("key %s from configmap %s not found in context", item.Key, configmap.Name)
			}
			state = addConfigMap(state, configmap.Name, item.Path, value)
		}
	}
	return state, nil
}

func addConfigMap(state llb.State, configmapName, name, value string) llb.State {
	return state.File(
		llb.Mkfile(name, 0755, []byte(value)),
		llb.WithCustomName("[tekton] configmap "+configmapName+"/"+name+": preparing file"),
	)
}
