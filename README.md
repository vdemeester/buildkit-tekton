[![asciinema example](https://asciinema.org/a/469173.svg)](https://asciinema.org/a/469173)

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
$ docker build -t foo -f examples/taskrun.yaml .
[+] Building 1.4s (10/10) FINISHED                                                => [internal] load build definition from task.yaml                         0.0s
 => => transferring dockerfile: 1.06kB                                      0.0s
 => [internal] load .dockerignore                                           0.0s
 => => transferring context: 2B                                             0.0s
 => resolve image config for quay.io/vdemeest/buildkit-tekton:v0.0.1        0.0s
 => CACHED docker-image://quay.io/vdemeest/buildkit-tekton:v0.0.1           0.0s
 => [internal] load tekton                                                  0.0s
 => => transferring dockerfile: 1.06kB                                      0.0s
 => CACHED docker-image://docker.io/library/bash:latest                     0.0s
 => /usr/local/bin/bash -c date +%s | tee /tekton/results/current-date-uni  0.4s
 => /usr/local/bin/bash -c date | tee /tekton/results/current-date-unix-ti  0.4s
 => /usr/local/bin/bash -c ls -l /; ls -l /tekton/results/; ls -l /tekton-  0.3s
 => exporting to image                                                      0.0s
 => => exporting layers                                                     0.0s
 => => writing image sha256:495bfb85d97b833881ea1823414db581e4b2876edb41e2  0.0s
```
