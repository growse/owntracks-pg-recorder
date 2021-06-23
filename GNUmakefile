VERSION := latest

BUILD_NUMBER ?= 0
TEST_COVERAGE := coverage.txt

LDFLAGS := "-w -s"

export GOPATH := $(shell go env GOPATH)

.PHONY: build test docker clean

build: dist/owntracks_pg_recorder_linux_amd64

dist/owntracks_pg_recorder_linux_amd64: *.go
	go mod vendor -v
	GOOS=linux GOARCH=amd64 go build -ldflags=$(LDFLAGS) -o dist/owntracks_pg_recorder_linux_amd64

test: $(TEST_COVERAGE)

$(TEST_COVERAGE): *.go
	go test -cover -covermode=count -coverprofile=$@ -v

docker: build
	upx dist/owntracks_pg_recorder_linux_amd64 || true
	docker build -t ghcr.io/growse/owntracks-pg-recorder:$(VERSION) .

clean:
	rm -rf dist
	rm -f $(TEST_COVERAGE)
	rm -rf vendor
