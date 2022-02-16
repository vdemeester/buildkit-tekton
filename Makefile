VERSION         = latest
IMAGE_REFERENCE = quay.io/vdemeest/buildkit-tekton
RUNTIME         = docker


all: image/push

.PHONY: image/push
image/push: image
	${RUNTIME} push $(IMAGE_REFERENCE):${VERSION}

.PHONE: image
image:
	${RUNTIME} build -f Dockerfile.${RUNTIME} -t ${IMAGE_REFERENCE}:${VERSION} .
