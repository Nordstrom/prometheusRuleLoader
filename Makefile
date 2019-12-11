app_name := prometheusRuleLoader
container_name := prometheusruleloader
container_registry := gitlab-registry.nordstrom.com/k8s/platform-bootstrap
container_release := 5.1

.PHONY: build tag/image push/image clean

build/linux/$(app_name): *.go | build
	GOOS=linux GOARCH=amd64 go build -o $@ .

build/darwin/$(app_name): *.go | build
	GOOS=darwin GOARCH=amd64 go build -o $@ .

build/image: build/linux/$(app_name) Dockerfile
	docker build \
		-t $(container_name) .

tag/image: build/image
	docker tag $(container_name) $(container_registry)/$(container_name):$(container_release)

push/image: tag/image
	docker push $(container_registry)/$(container_name):$(container_release)

build:
	mkdir -p build/linux build/darwin

clean:
	rm -rf build