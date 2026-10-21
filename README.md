# press-start

A morning alarm for one person. It picks the wake time from the first event in
Google Calendar and an alarm setting, plays music on a speaker attached to a
home server, reads the upcoming schedule aloud in Japanese, and keeps ringing,
louder each round, until a Withings Sleep sensor reports that the user is out
of bed.

The project is at an early stage. The Go code is only beginning.

The documents are in Japanese. Start at
[docs/ja/index.md](docs/ja/index.md). They follow the
format of [cumin-trace](https://github.com/cloveclovedev/cumin-trace), and
`cumin-trace check` runs on every pull request.

`go check` also runs on every pull request. It runs gofmt, `go build`,
`go vet`, and `go test` once the repository has a `go.mod`, and passes
without them until then.

## Run with Docker Compose

`compose.yaml` runs the image `ghcr.io/cloveclovedev/press-start`. It holds
no value of the home server. Set these variables before `docker compose up`.

| Variable | Required | Meaning |
|---|---|---|
| `PRESS_START_TAG` | no | Tag of the image. Default: `latest`. |
| `PRESS_START_AUDIO_GID` | yes | Group ID that owns `/dev/snd` on the host. |
| `PRESS_START_CGROUP_PARENT` | no | Parent cgroup (`cgroup_parent`). Empty: the Docker default. |
| `PRESS_START_CONFIG_DIR` | yes | Host directory with `settings.toml`. It is mounted read-only at `/config`. |
| `PRESS_START_ALSA_DEVICE` | yes | ALSA device of `mpv` (`--audio-device=alsa/<device>`). |
| `PRESS_START_ALSA_MIXER_CARD` | yes | ALSA card of `amixer -c`. |
| `PRESS_START_ALSA_MIXER_CONTROL` | yes | Mixer control that `amixer` sets. |
| `TZ` | yes | Time zone of the wake time. |

The service runs as UID 10001 and GID 10001. The host must let this UID read
`settings.toml` and the sound file in `PRESS_START_CONFIG_DIR`, and enter that
directory (the `x` permission), for example with mode `0755` on the directory
and `0644` on the files.

The project name is fixed to `press-start` in `compose.yaml`, so every copy
of the file runs the same project.

`docker compose config` stops with an error when a required variable is not
set.
