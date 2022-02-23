package runner

import (
	"context"
	"os"
	"path/filepath"

	"github.com/containerd/containerd/namespaces"
	controlapi "github.com/moby/buildkit/api/services/control"
	"github.com/moby/buildkit/control"
	"github.com/moby/buildkit/identity"
	"github.com/moby/buildkit/session"
	"github.com/moby/buildkit/session/auth/authprovider"
	"github.com/moby/buildkit/session/filesync"
	"github.com/moby/buildkit/session/testutil"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
)

func Run(ctx context.Context, contextDir, dockerfile string) error {
	localDirs := map[string]string{
		"context":    contextDir,
		"dockerfile": filepath.Dir(dockerfile),
	}
	frontendAttrs := map[string]string{
		"source": "quay.io/vdemeester/buildkit-tekton:latest",
	}

	sessionManager, err := session.NewManager()
	if err != nil {
		return err
	}
	sessionName := "tkn-execute"
	s, err := session.NewSession(ctx, sessionName, "")
	if err != nil {
		return errors.Wrap(err, "failed to create session")
	}
	syncedDirs := make([]filesync.SyncedDir, 0, len(localDirs))
	for name, d := range localDirs {
		syncedDirs = append(syncedDirs, filesync.SyncedDir{Name: name, Dir: d})
	}
	s.Allow(filesync.NewFSSyncProvider(syncedDirs))
	s.Allow(authprovider.NewDockerAuthProvider(os.Stderr))
	dialer := session.Dialer(testutil.TestStream(testutil.Handler(sessionManager.HandleConn)))

	id := identity.NewID()
	ctx = namespaces.WithNamespace(ctx, "buildkit")
	eg, ctx := errgroup.WithContext(ctx)

	ch := make(chan *controlapi.StatusResponse)
	eg.Go(func() error {
		return s.Run(ctx, dialer)
	})
	// Solve the dockerfile.
	eg.Go(func() error {
		defer s.Close()
		return solve(ctx, &controlapi.SolveRequest{
			Ref:     id,
			Session: s.ID(),
			// Exporter:      out.Type,
			// ExporterAttrs: out.Attrs,
			Frontend:      "gateway.v0",
			FrontendAttrs: frontendAttrs,
			Cache:         controlapi.CacheOptions{
				// Exports: cacheToList,
				// Imports: cacheFromList,
			},
		}, sessionManager, ch)
	})
	/*
		eg.Go(func() error {
			return showProgress(ch, cmd.noConsole)
		})
	*/
	if err := eg.Wait(); err != nil {
		return err
	}
	return nil
}

func solve(ctx context.Context, req *controlapi.SolveRequest, sm *session.Manager, ch chan *controlapi.StatusResponse) error {
	controller, err := createController()
	if err != nil {
		return err
	}

	return nil
}

func createController(sm *session.Manager) (control.Controller, error) {
	return nil
}
