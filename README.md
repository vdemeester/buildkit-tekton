[![buildkit-tekton in action](https://asciinema.org/a/469475.svg)](https://asciinema.org/a/469475)

# buildkit-tekton

[Buildkit](https://github.com/moby/buildkit) frontend to run
[Tekton](https://tekton.dev) objects locally.

## Usage

### With Docker (v20.04+ with `DOCKER_BUILDKIT=1`)

Add `#syntax=quay.io/vdemeest/buildkit-tekton:v0.0.1` as the first
line of you tekton yaml:

```bash
docker build -f taskrun.yaml .
```

### With `buildctl`

```bash
buildctl build --frontend=gateway.v0 --opt source=quay.io/vdemeest/buildkit-tekton:v0.0.1 --local context=.
```

### Options

No options yet, but there will be a lot.

## Examples

There is a [examples](./examples) folder to try things out.

```bash
❯ docker build -t foo -f examples/0-taskrun-simple/run.yaml .
[+] Building 2.9s (11/11) FINISHED
 => [internal] load build definition from run.yaml                                                                   0.0s
 => => transferring dockerfile: 35B                                                                                  0.0s
 => [internal] load .dockerignore                                                                                    0.0s
 => => transferring context: 34B                                                                                     0.0s
 => resolve image config for quay.io/vdemeest/buildkit-tekton:latest                                                 0.0s
 => CACHED docker-image://quay.io/vdemeest/buildkit-tekton:latest                                                    0.0s
 => [internal] load tekton from run.yaml                                                                             0.0s
 => => transferring dockerfile: 131B                                                                                 0.0s
 => resolve image config for docker.io/library/bash:latest                                                           0.2s
 => CACHED load metadata from +docker.io/library/bash:latest                                                         0.0s
 => => resolve docker.io/library/bash:latest                                                                         0.1s
 => simple-task-generated/print-date-unix-timestamp                                                                  0.3s
 => simple-task-generated/print-date-human-readable                                                                  0.3s
 => simple-task-generated/list-results                                                                               0.3s
 => exporting to image                                                                                               0.0s
 => => exporting layers                                                                                              0.0s
 => => writing image sha256:2bbb3dfbce5e7b4e07672cdd39dda83d0079a90d21914768cb0aff6329533053                         0.0s
 => => naming to docker.io/library/foo                                                                               0.0s
```

The same `PipelineRun` on `buildkit-tekton` and in a kubernetes
cluster with tekton installed (both without pre-cached images, … and
with approximately the same hardware)
- `buildkit-tekton`: 4m5s
- `tekton` in `k8s`: 7m
