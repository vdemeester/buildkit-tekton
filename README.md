[![buildkit-tekton in action](https://asciinema.org/a/469475.svg)](https://asciinema.org/a/469475)

# buildkit-tekton

[Buildkit](https://github.com/moby/buildkit) frontend to run
[Tekton](https://tekton.dev) objects locally.

## Usage

### With Docker (v20.04+ with `DOCKER_BUILDKIT=1`)

Add `#syntax=quay.io/vdemeest/buildkit-tekton:v0.1.0` as the first
line of you tekton yaml:

```bash
docker build -f taskrun.yaml .
```

### With `buildctl`

```bash
buildctl build --frontend=gateway.v0 --opt source=quay.io/vdemeest/buildkit-tekton:v0.1.0 --local context=. --opt-filename=pipelienrun.yaml --local dockerfile=.
```

### Options

No options yet, but there will be a lot.

## Examples

There is a [examples](./examples) folder to try things out.

```bash
❯ docker build -t foo -f examples/0-taskrun-simple/run.yaml .
[+] Building 1.6s (12/12) FINISHED
 => [internal] load build definition from run.yaml                                0.0s
 => => transferring dockerfile: 887B                                              0.0s
 => [internal] load .dockerignore                                                 0.0s
 => => transferring context: 34B                                                  0.0s
 => resolve image config for quay.io/vdemeest/buildkit-tekton:latest              0.0s
 => CACHED docker-image://quay.io/vdemeest/buildkit-tekton:latest                 0.0s
 => [tekton] load resource(s) from run.yaml                                       0.0s
 => => transferring dockerfile: 131B                                              0.0s
 => [tekton] load yaml files from context                                         0.0s
 => => transferring context: 33.45kB                                              0.0s
 => resolve image config for docker.io/library/bash:latest                        0.0s
 => CACHED docker-image://docker.io/library/bash:latest                           0.0s
 => [tekton] simple-task-generated/print-date-unix-timestamp                      0.4s
 => [tekton] simple-task-generated/print-date-human-readable                      0.3s
 => [tekton] simple-task-generated/list-results                                   0.3s
 => exporting to image                                                            0.0s
 => => exporting layers                                                           0.0s
 => => writing image sha256:2ff10579bf3e33cf7cda836d8bdd5962f77d9c995fd342bf3b9e  0.0s
 => => naming to docker.io/library/foo                                            0.0s```

The same `PipelineRun` on `buildkit-tekton` and in a kubernetes
cluster with tekton installed (both without pre-cached images, … and
with approximately the same hardware)
- `buildkit-tekton`: 4m5s
- `tekton` in `k8s`: 7m
