# The image of the press-start service.
#
# The build stage compiles a static binary. The run stage has mpv and amixer
# (alsa-utils), which the service runs to play the alarm sound on ALSA
# (PS-ALARM-005.2). The service runs as a user that is not root (UID and GID
# 10001); Compose adds the group of /dev/snd with group_add, so the image holds
# no group ID of the home server (PS-DEPLOY-003).
FROM golang:1.27.2 AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/press-start ./cmd/press-start

FROM debian:stable-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends mpv alsa-utils tzdata \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 10001 press-start \
    && useradd --uid 10001 --gid 10001 --no-create-home --shell /usr/sbin/nologin press-start

COPY --from=build /out/press-start /usr/local/bin/press-start

# Fixed IDs that do not depend on the base image. 10001 is above the range of
# system accounts and far above the login accounts that a host creates from
# 1000 upwards.
USER 10001:10001
ENTRYPOINT ["/usr/local/bin/press-start"]
