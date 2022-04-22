package files_test

import (
	"testing"

	"github.com/vdemeester/buildkit-tekton/pkg/tekton/files"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSecretMissingItem(t *testing.T) {
	secret := &corev1.Secret{
		Data: map[string][]byte{
			"secret": []byte("value"),
		},
	}
	secretSource := &corev1.SecretVolumeSource{
		Items: []corev1.KeyToPath{{
			Key:  "notfound",
			Path: "foo.txt",
		}},
	}
	_, err := files.Secret(secret, secretSource)
	if err == nil {
		t.Fatalf("expected an error, got nothing")
	} else {
	}
}

func TestSecret(t *testing.T) {
	tests := []struct {
		name         string
		secret       *corev1.Secret
		secretSource *corev1.SecretVolumeSource
	}{{
		name: "all-keys",
		secret: &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "mysecret"},
			Data: map[string][]byte{
				"secret1": []byte("value1"),
				"secret2": []byte("value2"),
			},
		},
		secretSource: &corev1.SecretVolumeSource{},
	}, {
		name: "with-items",
		secret: &corev1.Secret{
			Data: map[string][]byte{
				"secret1": []byte("value1"),
				"secret2": []byte("value2"),
			},
		},
		secretSource: &corev1.SecretVolumeSource{
			Items: []corev1.KeyToPath{{
				Key:  "secret1",
				Path: "foo.txt",
			}},
		},
	}}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, err := files.Secret(tc.secret, tc.secretSource)
			if err != nil {
				t.Fatal(err)
			}
			// FIXME(vdemeester) exercise this better, most likely using buildkit testutil (integration)
		})
	}
}
