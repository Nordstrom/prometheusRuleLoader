app_name := prometheusRuleLoader
DOCKER_IMAGE_NAME ?= quay.io/nordstrom/prometheusruleloader
DOCKER_IMAGE_TAG  ?= 2.0

.PHONY: build build_image release_image

build: *.go
	docker run --rm \
	  -e CGO_ENABLED=true \
	  -e OUTPUT=$(app_name) \
	  -v $(shell pwd):/src:rw \
	  centurylink/golang-builder

build_image: Dockerfile
	@echo ">> building docker image"
	docker build -t "$(DOCKER_IMAGE_NAME):$(DOCKER_IMAGE_TAG)" .

release_image:
	@echo ">> push docker image"
	@docker push "$(DOCKER_IMAGE_NAME):$(DOCKER_IMAGE_TAG)"
