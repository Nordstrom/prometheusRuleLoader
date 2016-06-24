app_name := prometheusRuleLoader
container_name := prometheusruleloader
container_registry := quay.io/nordstrom
container_release := 1.1

.PHONY: build build_image release_image

$(app_name): *.go
	docker run --rm \
	  -e CGO_ENABLED=true \
	  -e OUTPUT=$(app_name) \
	  -v $(shell pwd):/src \
	  centurylink/golang-builder

build/image: $(app_name) Dockerfile
	docker build \
		-t $(container_name) .

tag/image: build/image
	docker tag $(container_name) $(container_registry)/$(container_name):$(container_release)

push/image: tag/image
	docker push $(container_registry)/$(container_name):$(container_release)