package tekton

import (
	"regexp"
	"strings"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/tektoncd/pipeline/pkg/apis/pipeline/v1beta1"
	corev1 "k8s.io/api/core/v1"
	k8scheme "k8s.io/client-go/kubernetes/scheme"
)

type objects struct {
	tasks        []*v1beta1.Task
	taskruns     []*v1beta1.TaskRun
	pipelines    []*v1beta1.Pipeline
	pipelineruns []*v1beta1.PipelineRun
	secrets      []*corev1.Secret
	configs      []*corev1.ConfigMap
}

type TaskRun struct {
	main    *v1beta1.TaskRun
	tasks   map[string]*v1beta1.Task
	secrets map[string]*corev1.Secret
	configs map[string]*corev1.ConfigMap
}

type PipelineRun struct {
	main      *v1beta1.PipelineRun
	tasks     map[string]*v1beta1.Task
	pipelines map[string]*v1beta1.Pipeline
	secrets   map[string]*corev1.Secret
	configs   map[string]*corev1.ConfigMap
}

var (
	reg = regexp.MustCompile(`(?m)^\s*#([^#].*?)$`)
)

func readResources(main string, additionals []string) (interface{}, error) {
	s := k8scheme.Scheme
	if err := v1beta1.AddToScheme(s); err != nil {
		return nil, err
	}
	objs, err := parseTektonYAMLs(main)
	if err != nil {
		return nil, err
	}
	switch {
	case len(objs.taskruns) == 1 && len(objs.pipelineruns) == 0:
		r := TaskRun{
			main:    objs.taskruns[0],
			secrets: secretsToMap(objs.secrets),
			configs: configsToMap(objs.configs),
			tasks:   map[string]*v1beta1.Task{},
		}
		return populateTaskRun(r, additionals)
	case len(objs.taskruns) == 0 && len(objs.pipelineruns) == 1:
		r := PipelineRun{
			main:      objs.pipelineruns[0],
			secrets:   secretsToMap(objs.secrets),
			configs:   configsToMap(objs.configs),
			tasks:     map[string]*v1beta1.Task{},
			pipelines: map[string]*v1beta1.Pipeline{},
		}
		return populatePipelineRun(r, additionals)
	case len(objs.taskruns) == 0 && len(objs.pipelineruns) == 0:
		return nil, errors.New("No taskrun or pipelinern to run")
	case len(objs.taskruns) == 1 && len(objs.pipelineruns) == 1:
		return nil, errors.New("taskrun and pipelinerun both present, not supported")
	case len(objs.taskruns) > 1 || len(objs.pipelineruns) > 1:
		return nil, errors.New(" multiple taskruns and/or pipelineruns present, not supported")
	default:
		return nil, errors.Errorf("Document doesn't look like a tekton resource we can Resolve. %s", main)
	}
}

func populateTaskRun(r TaskRun, additionals []string) (TaskRun, error) {
	for _, data := range additionals {
		for _, doc := range strings.Split(strings.Trim(reg.ReplaceAllString(data, ""), "-"), "---") {
			obj, err := parseTektonYAML(doc)
			if err != nil {
				return r, errors.Wrapf(err, "failed to unmarshal %v", doc)
			}
			switch o := obj.(type) {
			case *v1beta1.Task:
				r.tasks[o.Name] = o
			default:
				logrus.Infof("Skipping document not looking like a tekton resource we can Resolve.")
			}
		}
	}
	return r, nil
}

func populatePipelineRun(r PipelineRun, additionals []string) (PipelineRun, error) {
	for _, data := range additionals {
		for _, doc := range strings.Split(strings.Trim(reg.ReplaceAllString(data, ""), "-"), "---") {
			obj, err := parseTektonYAML(doc)
			if err != nil {
				return r, errors.Wrapf(err, "failed to unmarshal %v", doc)
			}
			switch o := obj.(type) {
			case *v1beta1.Task:
				r.tasks[o.Name] = o
			case *v1beta1.Pipeline:
				r.pipelines[o.Name] = o
			default:
				logrus.Infof("Skipping document not looking like a tekton resource we can Resolve.")
			}
		}
	}
	return r, nil
}

func parseTektonYAMLs(s string) (*objects, error) {
	r := &objects{
		tasks:        []*v1beta1.Task{},
		taskruns:     []*v1beta1.TaskRun{},
		pipelines:    []*v1beta1.Pipeline{},
		pipelineruns: []*v1beta1.PipelineRun{},
		secrets:      []*corev1.Secret{},
		configs:      []*corev1.ConfigMap{},
	}

	for _, doc := range strings.Split(strings.Trim(reg.ReplaceAllString(s, ""), "-"), "---") {
		obj, err := parseTektonYAML(doc)
		if err != nil {
			return r, errors.Wrapf(err, "failed to unmarshal %v", doc)
		}
		switch o := obj.(type) {
		case *v1beta1.Task:
			r.tasks = append(r.tasks, o)
		case *v1beta1.TaskRun:
			r.taskruns = append(r.taskruns, o)
		case *v1beta1.Pipeline:
			r.pipelines = append(r.pipelines, o)
		case *v1beta1.PipelineRun:
			r.pipelineruns = append(r.pipelineruns, o)
		case *corev1.Secret:
			r.secrets = append(r.secrets, o)
		case *corev1.ConfigMap:
			r.configs = append(r.configs, o)
		}
	}
	return r, nil
}

func parseTektonYAML(s string) (interface{}, error) {
	decoder := k8scheme.Codecs.UniversalDeserializer()
	obj, _, err := decoder.Decode([]byte(s), nil, nil)
	if err != nil {
		return nil, err
	}
	return obj, nil
}

func secretsToMap(secrets []*corev1.Secret) map[string]*corev1.Secret {
	m := map[string]*corev1.Secret{}
	for _, s := range secrets {
		m[s.Name] = s
	}
	return m
}

func configsToMap(configs []*corev1.ConfigMap) map[string]*corev1.ConfigMap {
	m := map[string]*corev1.ConfigMap{}
	for _, c := range configs {
		m[c.Name] = c
	}
	return m
}
