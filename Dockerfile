FROM alpine:3

COPY dist/owntracks_pg_recorder_linux_amd64 /usr/local/bin/owntracks-pg-recorder

ENTRYPOINT ["/usr/local/bin/owntracks-pg-recorder"]