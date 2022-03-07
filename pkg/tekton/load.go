package tekton

import (
	"strings"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/tektoncd/pipeline/pkg/apis/pipeline/v1beta1"
	k8scheme "k8s.io/client-go/kubernetes/scheme"
)

type objects struct {
	tasks        []*v1beta1.Task
	taskruns     []*v1beta1.TaskRun
	pipelines    []*v1beta1.Pipeline
	pipelineruns []*v1beta1.PipelineRun
}

type TaskRun struct {
	main  *v1beta1.TaskRun
	tasks map[string]*v1beta1.Task
}

type PipelineRun struct {
	main      *v1beta1.PipelineRun
	tasks     map[string]*v1beta1.Task
	pipelines map[string]*v1beta1.Pipeline
}

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
		return populateTaskRun(objs.taskruns[0], additionals)
	case len(objs.taskruns) == 0 && len(objs.pipelineruns) == 1:
		return populatePipelineRun(objs.pipelineruns[0], additionals)
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

func populateTaskRun(tr *v1beta1.TaskRun, additionals []string) (TaskRun, error) {
	r := TaskRun{
		main:  tr,
		tasks: map[string]*v1beta1.Task{},
	}
	for _, data := range additionals {
		for _, doc := range strings.Split(strings.Trim(data, "-"), "---") {
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

func populatePipelineRun(pr *v1beta1.PipelineRun, additionals []string) (PipelineRun, error) {
	r := PipelineRun{
		main:      pr,
		tasks:     map[string]*v1beta1.Task{},
		pipelines: map[string]*v1beta1.Pipeline{},
	}
	for _, data := range additionals {
		for _, doc := range strings.Split(strings.Trim(data, "-"), "---") {
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
	}
	for _, doc := range strings.Split(strings.Trim(s, "-"), "---") {
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
