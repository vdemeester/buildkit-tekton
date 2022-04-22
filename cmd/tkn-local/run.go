package main

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/docker/cli/cli/streams"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/session"
	"github.com/moby/buildkit/session/auth/authprovider"
	"github.com/moby/buildkit/util/appcontext"
	"github.com/moby/buildkit/util/progress/progresswriter"
	"github.com/moby/term"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/vdemeester/buildkit-tekton/pkg/build"
	"github.com/vdemeester/buildkit-tekton/pkg/buildkit"
	"golang.org/x/sync/errgroup"
)

type runOption struct {
	filename string
	dirs     []string
	host     string
	// mimics buildctl opt, should control even more the UX
	options []string
}

func runCommand() *cobra.Command {
	opts := &runOption{}
	cmd := &cobra.Command{
		Use:     "run",
		Aliases: []string{},
		Short:   "Run a tekton resource",
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(opts)
		},
	}
	cmd.Flags().StringVar(&opts.host, "host", "", "Host to use")
	cmd.Flags().StringVarP(&opts.filename, "filename", "f", "", "Main file to load")
	cmd.Flags().StringArrayVarP(&opts.dirs, "dir", "d", []string{}, "Folder(s) to add to the context")
	cmd.Flags().StringArrayVar(&opts.options, "opt", []string{}, "Option to pass")

	return cmd
}

func run(opts *runOption) error {
	stdin, _, _ := term.StdStreams()
	if opts.filename == "" {
		return errors.New("must specify -f")
	}
	if len(opts.dirs) > 1 {
		return errors.New("multiple -d not yet supported")
	}
	var dir string
	var filename string
	if len(opts.dirs) == 0 {
		abs, err := filepath.Abs(opts.filename)
		if err != nil {
			return err
		}
		dir = filepath.Dir(abs)
		filename = filepath.Base(abs)
	} else {
		dir = opts.dirs[0]
		filename = opts.filename
	}

	if filename == "-" {
		s := streams.NewIn(stdin)
		content, err := io.ReadAll(s)
		s.Close()
		if err != nil {
			return err
		}
		// Create a "temporary" file and pass it in context
		d, err := ioutil.TempDir("", "buildkit-tekton")
		if err != nil {
			return err
		}

		defer os.RemoveAll(d) // clean up
		file, err := ioutil.TempFile(d, "*.yaml")
		if err != nil {
			return err
		}
		if _, err := file.Write(content); err != nil {
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
		filename = filepath.Base(file.Name())
		dir = filepath.Dir(file.Name())
	}

	eg, ctx := errgroup.WithContext(appcontext.Context())
	// Connect or start buildkit
	c, err := buildkit.NewClient(ctx, opts.host)
	if err != nil {
		return err
	}

	attachable := []session.Attachable{authprovider.NewDockerAuthProvider(os.Stderr)}
	buildopts := client.SolveOpt{
		LocalDirs: map[string]string{
			"context":    dir,
			"dockerfile": dir,
		},
		Session: attachable,
		// CacheExports: c.cfg.CacheExports,
		// CacheImports: c.cfg.CacheImports,
	}
	buildopts.FrontendAttrs, err = parseOpt(opts.options)
	if err != nil {
		return errors.Wrap(err, "invalid opt")
	}
	buildopts.FrontendAttrs["filename"] = filename

	pw, err := progresswriter.NewPrinter(context.TODO(), os.Stderr, "auto")
	if err != nil {
		return err
	}
	mw := progresswriter.NewMultiWriter(pw)
	var writers []progresswriter.Writer
	for _, at := range attachable {
		if s, ok := at.(interface {
			SetLogger(progresswriter.Logger)
		}); ok {
			w := mw.WithPrefix("", false)
			s.SetLogger(func(s *client.SolveStatus) {
				w.Status() <- s
			})
			writers = append(writers, w)
		}
	}

	eg.Go(func() error {
		defer func() {
			for _, w := range writers {
				close(w.Status())
			}
		}()
		r, err := c.Build(ctx, buildopts, "foo-is-bar", build.Build, progresswriter.ResetTime(mw.WithPrefix("", false)).Status())
		if err != nil {
			return err
		}
		for k, v := range r.ExporterResponse {
			fmt.Println(k, " -- ", v)
		}
		return nil
	})

	eg.Go(func() error {
		<-pw.Done()
		return pw.Err()
	})
	return eg.Wait()
}

func parseOpt(opts []string) (map[string]string, error) {
	m := make(map[string]string)
	modern, err := attrMap(opts)
	if err != nil {
		return nil, err
	}
	for k, v := range modern {
		m[k] = v
	}
	return m, nil
}

func attrMap(sl []string) (map[string]string, error) {
	m := map[string]string{}
	for _, v := range sl {
		parts := strings.SplitN(v, "=", 2)
		if len(parts) != 2 {
			return nil, errors.Errorf("invalid value %s", v)
		}
		m[parts[0]] = parts[1]
	}
	return m, nil
}
