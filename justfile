set dotenv-load := true

default:
    just --list

test:
    go test -cover -covermode=count -coverprofile=$@ -v

tidy:
    go mod tidy -x

build:
    go build

build-container:
    docker buildx build -t owntracks-pg-recorder .

format:
    @just tidy
    golangci-lint fmt ./...

fix:
    golangci-lint run --fix

lint:
    @just format
    @just fix
    golangci-lint run ./...
