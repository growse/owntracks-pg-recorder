set dotenv-load := true

default:
    just --list

format:
    gofmt -w .

test:
    go test -cover -covermode=count -coverprofile=$@ -v

build:
    go build

build-container:
    docker buildx build -t owntracks-pg-recorder .
