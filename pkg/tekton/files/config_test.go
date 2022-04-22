package files_test

import (
	"testing"

	"github.com/vdemeester/buildkit-tekton/pkg/tekton/files"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestConfigMapMissingItem(t *testing.T) {
	configmap := &corev1.ConfigMap{
		Data: map[string]string{
			"configmap": "value",
		},
	}
	configmapSource := &corev1.ConfigMapVolumeSource{
		Items: []corev1.KeyToPath{{
			Key:  "notfound",
			Path: "foo.txt",
		}},
	}
	_, err := files.ConfigMap(configmap, configmapSource)
	if err == nil {
		t.Fatalf("expected an error, got nothing")
	} else {
	}
}

func TestConfigMap(t *testing.T) {
	tests := []struct {
		name            string
		configmap       *corev1.ConfigMap
		configmapSource *corev1.ConfigMapVolumeSource
	}{{
		name: "all-keys",
		configmap: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "myconfigmap"},
			Data: map[string]string{
				"configmap1": "value1",
				"configmap2": "value2",
			},
		},
		configmapSource: &corev1.ConfigMapVolumeSource{},
	}, {
		name: "with-items",
		configmap: &corev1.ConfigMap{
			Data: map[string]string{
				"configmap1": "value1",
				"configmap2": "value2",
			},
		},
		configmapSource: &corev1.ConfigMapVolumeSource{
			Items: []corev1.KeyToPath{{
				Key:  "configmap1",
				Path: "foo.txt",
			}},
		},
	}}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, err := files.ConfigMap(tc.configmap, tc.configmapSource)
			if err != nil {
				t.Fatal(err)
			}
			// FIXME(vdemeester) exercise this better, most likely using buildkit testutil (integration)
		})
	}
}
