[![buildkit-tekton in action](https://asciinema.org/a/469475.svg)](https://asciinema.org/a/469475)

# buildkit-tekton

[Buildkit](https://github.com/moby/buildkit) frontend to run
[Tekton](https://tekton.dev) objects locally.

This repository produces two *artifacts*:
- a [Buildkit](https://github.com/moby/buildkit) frontend
- a `tkn-local` command to easily consume this frontend (in most situation)

## `buildkit-tekton` Usage

### With Docker (v20.04+ with `DOCKER_BUILDKIT=1`)

Add `#syntax=ghcr.io/vdemeester/buildkit-tekton/frontend:v0.3.0` as the first
line of you tekton yaml:

```bash
docker build -f taskrun.yaml .
```

### With `buildctl`

```bash
buildctl build --frontend=gateway.v0 --opt source=ghcr.io/vdemeester/buildkit-tekton/frontend:v0.3.0 --local context=. --opt-filename=pipelienrun.yaml --local dockerfile=.
```

### Options

No options yet, but there will be a lot.

## Examples

There is a [examples](./examples) folder to try things out.

```bash
$ docker build -t foo -f examples/0-taskrun-simple/run.yaml .
[+] Building 1.6s (12/12) FINISHED
 => [internal] load build definition from run.yaml                                0.0s
 => => transferring dockerfile: 887B                                              0.0s
 => [internal] load .dockerignore                                                 0.0s
 => => transferring context: 34B                                                  0.0s
 => resolve image config for ghcr.io/vdemeester/buildkit-tekton/frontend:latest   0.0s
 => CACHED docker-image://ghcr.io/vdemeester/buildkit-tekton/frontend:latest      0.0s
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
 => => naming to docker.io/library/foo
 0.0s
 ```

The same `PipelineRun` on `buildkit-tekton` and in a kubernetes
cluster with tekton installed (both without pre-cached images, … and
with approximately the same hardware)
- `buildkit-tekton`: 4m5s
- `tekton` in `k8s`: 7m

## Supported Tekton Features

### TaskRun

| Feature | Status | Notes |
|---------|--------|-------|
| Embedded TaskSpec | ✅ Supported | |
| TaskRef | ✅ Supported | Reference external Task definitions |
| Parameters | ✅ Supported | Default values and overrides |
| Results | ✅ Supported | Via `/tekton/results` directory |
| Scripts | ✅ Supported | With shebang support |
| Commands | ✅ Supported | command + args |
| Step Templates | ✅ Supported | |
| Environment Variables | ✅ Supported | Direct env and EnvFrom |
| EnvFrom (ConfigMap/Secret) | ✅ Supported | Load env vars from ConfigMaps/Secrets |
| Workspaces | ✅ Supported | ConfigMap, Secret, EmptyDir, PVC |
| Volumes (emptyDir) | ✅ Supported | Share data between steps |
| VolumeMounts | ✅ Supported | Mount volumes with subPath, readOnly |
| OnError | ✅ Supported | `continue` and `stopAndFail` |
| Step Timeout | ✅ Supported | Uses shell `timeout` command |
| SecurityContext (runAsUser) | ✅ Supported | |
| Sidecars | ❌ Not Supported | |
| VolumeDevices | ❌ Not Supported | |
| PodTemplate | ❌ Not Supported | |

### PipelineRun

| Feature | Status | Notes |
|---------|--------|-------|
| Embedded PipelineSpec | ✅ Supported | |
| PipelineRef | ✅ Supported | Reference external Pipeline definitions |
| Parameters | ✅ Supported | Pipeline and Task level |
| Workspaces | ✅ Supported | ConfigMap, Secret, EmptyDir, PVC, VolumeClaimTemplate |
| RunAfter | ✅ Supported | Task ordering/dependencies |
| WhenExpressions | ✅ Supported | Conditional task execution (`in`, `notin`) |
| Finally Blocks | ✅ Supported | Tasks that run after all regular tasks |
| Task Timeout | ✅ Supported | Applies to all steps in a task |
| Results Sharing | ✅ Supported | Via `/tekton/from-task/<taskname>` |
| Custom Tasks | ❌ Not Supported | |
| TaskRunSpecs | ❌ Not Supported | |
| Matrix | ❌ Not Supported | |

### Resources

| Resource | Status | Notes |
|----------|--------|-------|
| Task | ✅ Supported | Referenced via TaskRef |
| Pipeline | ✅ Supported | Referenced via PipelineRef |
| ConfigMap | ✅ Supported | For workspaces and EnvFrom |
| Secret | ✅ Supported | For workspaces and EnvFrom |
| PersistentVolumeClaim | ✅ Supported | For workspaces |
| OCI Bundles | ⚠️ Partial | Experimental, requires `enable-tekton-oci-bundles=true` |

## Examples

The [examples](./examples) folder contains working examples for various features:

### TaskRun Examples

- **0-taskrun-simple**: Basic TaskRun with scripts and commands
- **0-taskrun-with-params**: Parameter passing and default values
- **0-taskrun-onerror**: OnError handling (`onError: continue`)
- **0-taskrun-volumes**: EmptyDir volumes shared between steps
- **0-taskrun-timeout**: Step timeout functionality
- **0-taskrun-envfrom**: Environment variables from ConfigMaps/Secrets

### PipelineRun Examples

- **1-pipelinerun-simple**: Basic PipelineRun with runAfter
- **1-pipelinerun-with-params**: Parameter propagation
- **1-pipelinerun-with-workspaces**: ConfigMap, Secret, and PVC workspaces
- **1-pipelinerun-finally**: Finally blocks for cleanup tasks
- **1-pipelinerun-when**: WhenExpressions for conditional execution
- **1-pipelinerun-go**: Real-world Go testing pipeline

### Advanced Examples

- **2-taskref-oci**: OCI bundle references
- **3-context-and-ref**: External task references

## `tkn-local` Usage

```bash
$ tkn local
Local commands

Usage:
  local [command]

Available Commands:
  completion  Generate the autocompletion script for the specified shell
  help        Help about any command
  prune       Run a tekton resource
  run         Run a tekton resource

Flags:
  -h, --help   help for local

Use "local [command] --help" for more information about a command.
```
