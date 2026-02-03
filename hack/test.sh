#!/usr/bin/env bash
set -eux -o pipefail
timestamp="$(date +%s)"

: "${DOCKER:=docker}"
export DOCKER_BUILDKIT=1

version="$(git describe --match 'v[0-9]*' --dirty='.m' --always --tags)"
td="/tmp/buildkit-tekton-test-${version}-${timestamp}"
mkdir -p "${td}"
trap 'rm -rf ${td}' EXIT
cp -a examples "${td}"

: "${REGISTRY_PORT:=5050}"
"$DOCKER" rm -f reg || true
"$DOCKER" run -d --name reg -p "127.0.0.1:${REGISTRY_PORT}:5000" docker.io/library/registry:2

image="127.0.0.1:${REGISTRY_PORT}/buildkit-tekton:test-${version}-${timestamp}"
"$DOCKER" build -t "$image" -f Dockerfile.docker .
"$DOCKER" push "$image"

# Examples that require external resources (OCI bundles, network git clone, etc.)
SKIP_EXAMPLES="2-taskref-oci 1-pipelinerun-go 3-context-and-ref"

for f in "${td}"/examples/*; do
	name="$(basename "${f}")"
	# Skip examples that require external resources
	if [[ " ${SKIP_EXAMPLES} " == *" ${name} "* ]]; then
		echo "===== ${name} ===== (SKIPPED - requires external resources)"
		continue
	fi
	echo "===== ${name} ====="
	(
		cd "$f"
        for sf in "${f}"/*.yaml; do
            sname="$(basename "${sf}")"
            if [[ ${sname} == *"run"* ]]; then
                echo "---- ${sname} ----"
		        sed -i '1 s/^ *#*syntax*=.*$//' "${sf}"
		        (
			        echo "#syntax=${image}"
			        cat "${sf}"
		        ) | sponge "${sf}"
		        "$DOCKER" build \
                          --build-arg=enable-tekton-oci-bundles=true \
                          -t "${name}" \
                          -f "${sf}" .
            fi
        done
	)
done
