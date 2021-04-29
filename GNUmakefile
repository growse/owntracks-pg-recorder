ARCH := amd64
VERSION := 1.0.0

BUILD_NUMBER ?= 0
TEST_COVERAGE := coverage.txt

LDFLAGS := "-w -s"

export GOPATH := $(shell go env GOPATH)

.PHONY: build
build: $(addprefix dist/owntracks_pg_recorder_linux_, $(foreach a, $(ARCH), $(a)))

.PHONY: test
test: $(TEST_COVERAGE)

$(TEST_COVERAGE):
	go test -cover -covermode=count -coverprofile=$@ -v

dist/owntracks_pg_recorder_linux_%:
	go mod vendor -v
	GOOS=linux GOARCH=$* go build -ldflags=$(LDFLAGS) -o dist/owntracks_pg_recorder_linux_$*
	upx $@

.PHONY: docker
docker: build
	docker build -t growse/owntracks-pg-recorder:$(VERSION) .

.PHONY: clean
clean:
	rm -rf dist
	rm -f $(TEST_COVERAGE)
	rm -rf *.deb
