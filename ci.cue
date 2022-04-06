package buildkittekton

import (
	"dagger.io/dagger"
	"dagger.io/dagger/core"

	"universe.dagger.io/go"
	"universe.dagger.io/docker"
	"universe.dagger.io/bash"
	"universe.dagger.io/alpine"
)

dagger.#Plan & {
	client: {
		filesystem: {
			"./": read: {
				contents: dagger.#FS
			}
		}
		network: "unix:///var/run/docker.sock": connect: dagger.#Socket
	}

	actions: {
		// Cache
		_goBuildCache: core.#CacheDir & {
			id: "go-build-cache"
		}
		_goBuildCacheMount: "goBuildCache": {
			dest:     *"/root/.cache/go-build" | string
			contents: _goBuildCache
		}
		_goModCache: core.#CacheDir & {
			id: "go-mod-cache"
		}
		_goModCacheMount: "goModCache": {
			dest:     *"/go/pkg/mod" | string
			contents: _goModCache
		}

		"tkn-local": go.#Build & {
			source:    client.filesystem."./".read.contents
			package:   "./cmd/tkn-local"
			container: go.#Container & {
				mounts: _goBuildCacheMount & _goModCacheMount
			}
		}
		"build-tekton": go.#Build & {
			source:    client.filesystem."./".read.contents
			package:   "./cmd/buildkit-tekton"
			container: go.#Container & {
				mounts: _goBuildCacheMount & _goModCacheMount
			}
		}
		image: docker.#Dockerfile & {
			// This is the context.
			source: client.filesystem."./".read.contents
			dockerfile: path: "Dockerfile.docker"
		}
		test: {
			_image: alpine.#Build & {
				packages: {
					bash: {}
					moreutils: {}
					make: {}
					git: {}
					"docker-cli": {}
				}
			}
			docker: bash.#Run & {
				input:   _image.output
				workdir: "/bash/scripts"
				script: {
					directory: client.filesystem."./".read.contents
					filename:  "hack/test.sh"
				}
				mounts: docker: {
					dest:     "/var/run/docker.sock"
					contents: client.network."unix:///var/run/docker.sock".connect
				}
			}
		}
	}
}
