FROM debian:bullseye-slim

LABEL org.opencontainers.image.source https://github.com/growse/owntracks-pg-recorder

RUN apt-get update && apt-get install -y ca-certificates && apt-get clean && rm -rf /var/lib/apt/lists/*

COPY dist/owntracks_pg_recorder_linux_amd64 /usr/local/bin/owntracks-pg-recorder
COPY databasemigrations /var/lib/owntracks-pg-recorder/databasemigrations
RUN mkdir /etc/owntracks-pg-recorder
VOLUME /etc/owntracks-pg-recorder

ENV OT_PG_RECORDER_DATABASEMIGRATIONSPATH /var/lib/owntracks-pg-recorder/databasemigrations

ENTRYPOINT [ "/usr/local/bin/owntracks-pg-recorder" ]