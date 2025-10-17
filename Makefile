DOCKER_REPO = stojs/echo-server
DOCKER_PLATFORMS += linux/amd64
DOCKER_PLATFORMS += linux/arm64

GO_EMBEDDED_FILES += cmd/echo-server/html/frontend.tmpl.html

-include .makefiles/Makefile
-include .makefiles/pkg/go/v1/Makefile
-include .makefiles/pkg/docker/v1/Makefile

run: $(GO_DEBUG_DIR)/echo-server
	$< $(RUN_ARGS)

.makefiles/%:
	@curl -sfL https://makefiles.dev/v1 | bash /dev/stdin "$@"
