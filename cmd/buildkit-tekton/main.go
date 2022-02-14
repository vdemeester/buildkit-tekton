package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"os"

	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/frontend/gateway/grpcclient"
	"github.com/moby/buildkit/util/appcontext"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/vdemeester/buildkit-tekton/pkg/build"
	"github.com/vdemeester/buildkit-tekton/pkg/tekton"
)

var fgraph bool
var ffilename string

func main() {
	flag.BoolVar(&fgraph, "graph", false, "output a graph and exit")
	flag.StringVar(&ffilename, "filename", "task.yaml", "the file to read from")
	flag.Parse()

	if fgraph {
		fmt.Println("filename:", ffilename)
		if err := printGraph(ffilename, os.Stdout); err != nil {
			logrus.Fatalf("fatal error: %s", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	if err := grpcclient.RunFromEnvironment(appcontext.Context(), build.Build); err != nil {
		logrus.Fatalf("fatal error: %s", err)
		panic(err)
	}
}

func printGraph(filename string, out io.Writer) error {
	b, _ := ioutil.ReadFile(filename)
	st, err := tekton.TektonToLLB(string(b))
	if err != nil {
		return errors.Wrap(err, "to llb")
	}

	dt, err := st.Marshal(context.Background())
	if err != nil {
		return errors.Wrap(err, "marshaling llb state")
	}

	return llb.WriteTo(dt, out)
}
