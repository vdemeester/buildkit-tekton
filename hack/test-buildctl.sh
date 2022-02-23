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

DOCKER_ARGS=${DOCKER_ARGS:=}

"$DOCKER" rm -f reg || true
"$DOCKER" run ${DOCKER_ARGS} -d --name reg -p 127.0.0.1:5000:5000 docker.io/library/registry:2

image_prefix=${image_prefix:-127.0.0.1:5000}

image="127.0.0.1:5000/buildkit-tekton:test-${version}-${timestamp}"
buildimage="${image_prefix}/buildkit-tekton:test-${version}-${timestamp}"
"$DOCKER" build -t "$image" -f Dockerfile.docker .
"$DOCKER" push "$image"

for f in "${td}"/examples/*; do
	name="$(basename "${f}")"
	echo "===== ${name} ====="
	(
		cd "$f"
        for sf in "${f}"/*.yaml; do
            sname="$(basename "${sf}")"
            echo "---- ${sname} ----"
		    sed -i '1 s/^ *#*syntax*=.*$//' "${sf}"
		    (
			    echo "#syntax=${image}"
			    cat "${sf}"
		    ) | sponge "${sf}"
            pwd
            buildctl build --frontend gateway.v0 --opt source=${buildimage} \
                     --local dockerfile=. --local context=. \
                     --opt filename=$(basename ${sf}) \
                     --opt enable-tekton-oci-bundles=true \
                     --output type=image,name=${name}
        done
	)
done
