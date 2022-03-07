package tekton

import (
	"fmt"
	"io/ioutil"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/tektoncd/pipeline/pkg/apis/pipeline/v1beta1"
	"github.com/tektoncd/pipeline/test/diff"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8scheme "k8s.io/client-go/kubernetes/scheme"
)

func TestReadResources(t *testing.T) {
	tt := []struct {
		main        string
		additionals []string
	}{}
	for _, tc := range tt {
		tc := tc
		name := fmt.Sprintf("%s-%s", tc.main, strings.Join(tc.additionals, "_"))
		t.Run(name, testReadResources(tc.main, tc.additionals))
	}
}

func testReadResources(main string, additionals []string) func(*testing.T) {
	return func(t *testing.T) {
		_, err := ioutil.ReadFile(fmt.Sprintf("testdata/%s", main))
		if err != nil {
			t.Fatalf("ReadFile() = %v", err)
		}

	}
}

func TestParseTektonYAMLInvalid(t *testing.T) {
	s := k8scheme.Scheme
	if err := v1beta1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	tt := []struct {
		yaml string
	}{
		{yaml: ""},
		{yaml: "foo=bar"},
		{yaml: `hello
world

not a valid yaml`},
		{yaml: ` french-hens: 3
 calling-birds:
   - huey
   - dewey
   - louie
   - fred
 xmas-fifth-day:
   calling-birds: four
   french-hens: 3
   golden-rings: 5
   partridges:
     count: 1
     location: "a pear tree"
   turtle-doves: two`},
		{yaml: "kind: Task"},
		{yaml: `apiVersion: foo.bar/baz
kind: Foo`},
		{yaml: `apiVersion: tekton.dev/v1beta1
kind: Task
spec:
  steps: "foo"`},
	}
	for i, tc := range tt {
		tc := tc
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			_, err := parseTektonYAML(tc.yaml)
			if err == nil {
				t.Fatalf("parseTektonYAML should have failed with %s", tc.yaml)
			}
		})
	}
}

func TestParseTektonYAML(t *testing.T) {
	ignoreTypeMeta := cmpopts.IgnoreTypes(metav1.TypeMeta{})
	s := k8scheme.Scheme
	if err := v1beta1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	tt := []struct {
		yaml     string
		expected interface{}
	}{{
		yaml: `apiVersion: tekton.dev/v1beta1
kind: Task`,
		expected: &v1beta1.Task{},
	}, {
		yaml: `apiVersion: tekton.dev/v1beta1
kind: TaskRun`,
		expected: &v1beta1.TaskRun{},
	}, {
		yaml: `apiVersion: tekton.dev/v1beta1
kind: Pipeline`,
		expected: &v1beta1.Pipeline{},
	}, {
		yaml: `apiVersion: tekton.dev/v1beta1
kind: PipelineRun`,
		expected: &v1beta1.PipelineRun{},
	}, {
		yaml: `apiVersion: tekton.dev/v1beta1
kind: Task
metadata:
  name: foo
  namespace: bar
spec:
  steps:
  - name: print
    image: bash:latest
    script: |
      echo foo`,
		expected: &v1beta1.Task{
			ObjectMeta: metav1.ObjectMeta{Name: "foo", Namespace: "bar"},
			Spec: v1beta1.TaskSpec{
				Steps: []v1beta1.Step{{
					Container: corev1.Container{
						Name:  "print",
						Image: "bash:latest",
					},
					Script: "echo foo",
				}},
			},
		},
	}, {
		yaml: `apiVersion: tekton.dev/v1beta1
kind: TaskRun
spec:
  taskRef:
    name: foo`,
		expected: &v1beta1.TaskRun{
			Spec: v1beta1.TaskRunSpec{
				TaskRef: &v1beta1.TaskRef{
					Name: "foo",
				},
			},
		},
	}, {
		yaml: `apiVersion: tekton.dev/v1beta1
kind: Pipeline
metadata:
  name: bar
spec:
  tasks:
  - name: task-1
    taskRef:
      name: foo`,
		expected: &v1beta1.Pipeline{
			ObjectMeta: metav1.ObjectMeta{Name: "bar"},
			Spec: v1beta1.PipelineSpec{
				Tasks: []v1beta1.PipelineTask{{
					Name: "task-1",
					TaskRef: &v1beta1.TaskRef{
						Name: "foo",
					},
				}},
			},
		},
	}, {
		yaml: `apiVersion: tekton.dev/v1beta1
kind: PipelineRun
metadata:
  generateName: bar-
spec:
  pipelineRef:
    name: foo`,
		expected: &v1beta1.PipelineRun{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "bar-"},
			Spec: v1beta1.PipelineRunSpec{
				PipelineRef: &v1beta1.PipelineRef{
					Name: "foo",
				},
			},
		},
	}}
	for i, tc := range tt {
		tc := tc
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			b, err := parseTektonYAML(tc.yaml)
			if err != nil {
				t.Fatal(err)
			}
			if d := cmp.Diff(tc.expected, b, ignoreTypeMeta); d != "" {
				t.Errorf("Pod metadata doesn't match %s", diff.PrintWantGot(d))
			}
		})
	}
}
