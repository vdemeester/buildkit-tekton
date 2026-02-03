package config

import (
	"context"
	"strings"

	"github.com/moby/buildkit/frontend/gateway/client"
	"github.com/tektoncd/pipeline/pkg/apis/config"
)

// Config holds the frontend configuration options.
// It "brings" some from upstream tekton own set of configuration.
type Config struct {
	Defaults     config.Defaults
	FeatureFlags config.FeatureFlags
}

// Parse converts BuildKit BuildOpts into a Config object
func Parse(opts client.BuildOpts) (*Config, error) {
	c := &Config{}

	for name, value := range opts.Opts {
		// we use --build-arg to pass option through "docker build"
		// so we need to strip it to get the "real" option
		if strings.HasPrefix(name, "build-arg:") {
			name = strings.TrimPrefix(name, "build-arg:")
		}
		// TODO: Support more options
		switch name {
		case "enable-api-fields":
			c.FeatureFlags.EnableAPIFields = value
		case "enable-tekton-oci-bundles":
			// OCI bundles are now handled via resolvers, this option is deprecated
			_ = value
		}
	}

	return c, nil
}

// ToContext enriches a context with Tekton configuration object
func (c *Config) ToContext(ctx context.Context) context.Context {
	return config.ToContext(ctx, &config.Config{
		Defaults:     &c.Defaults,
		FeatureFlags: &c.FeatureFlags,
	})
}
