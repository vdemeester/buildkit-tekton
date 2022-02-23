package main

import (
	"flag"

	"github.com/moby/buildkit/frontend/gateway/grpcclient"
	"github.com/moby/buildkit/util/appcontext"
	"github.com/sirupsen/logrus"
	"github.com/vdemeester/buildkit-tekton/pkg/build"
)

func main() {
	flag.Parse()

	if err := grpcclient.RunFromEnvironment(appcontext.Context(), build.Build); err != nil {
		logrus.Fatalf("fatal error: %s", err)
	}
}
