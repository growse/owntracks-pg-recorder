FROM golang:1.24.4 as builder

LABEL org.opencontainers.image.source https://github.com/growse/owntracks-pg-recorder

ENV CGO_ENABLED=0

COPY go.mod /app/go.mod
COPY go.sum /app/go.sum
WORKDIR /app

RUN --mount=type=cache,target=/root/go/pkg/mod go mod download

COPY . /app

RUN --mount=type=cache,target=/root/.cache/go-build go build -ldflags="-w -s"

FROM alpine:3.22 as cert-fetcher

RUN echo "ot-pg-recorder:x:10001:10001:OwnTracksPgRecorder,,,:/home/ot-pg-recorder:/bin/false" > /passwd

FROM scratch

COPY --from=builder /app/owntracks-pg-recorder /usr/local/bin/owntracks-pg-recorder
COPY --from=cert-fetcher /passwd /etc/passwd
COPY --from=cert-fetcher /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

VOLUME /etc/owntracks-pg-recorder

USER ot-pg-recorder

ENTRYPOINT [ "/usr/local/bin/owntracks-pg-recorder" ]
