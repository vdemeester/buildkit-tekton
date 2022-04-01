package buildkittekton

import (
	"dagger.io/dagger"
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
        _goimage: go.#Image & {
            version: "1.18.0" // 1.17.8 # FIXME(vdemeester) do a matrix/param here
            packages: { git: {} }
        }
        "tkn-local": go.#Build & {
            source: client.filesystem."./".read.contents
            package: "./cmd/tkn-local"
            container: go.#Container & {
                input: _goimage.output
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
                input: _image.output
                workdir: "/bash/scripts"
                script: {
                    directory: client.filesystem."./".read.contents
                    filename: "hack/test.sh"
                }
                mounts: docker: {
        			dest:     "/var/run/docker.sock"
		        	contents: client.network."unix:///var/run/docker.sock".connect
        		}
            }
        }
    }
}