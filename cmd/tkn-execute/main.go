package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/vdemeester/buildkit-tekton/pkg/runner"
)

var (
	standalone bool
)

func main() {
	flag.BoolVar(&standalone, "standalone", true, "run as standalone (if not, will require a buildkit socket)")
	flag.Parse()

	if err := runner.Run(context.Background(), ".", "test.yaml"); err != nil {
		fmt.Printf("%v\n", err.Error())
		os.Exit(1)
	}
}
