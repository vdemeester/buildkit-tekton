VERSION := v0.0.1
IMAGE_REFERENCE := quay.io/vdemeest/buildkit-tekton

all: image/push

.PHONY: image/push
image/push: image
	docker push $(IMAGE_REFERENCE):${VERSION}

.PHONE: image
image:
	docker build -t ${IMAGE_REFERENCE}:${VERSION} .
