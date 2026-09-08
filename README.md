# Model Citizen

This discord bot is truly a model citizen

## Running

```sh
docker compose up --build
```

Requires `data/.env` with `DISCORD_KEY` set.

## Building locally

Voice requires Discord's DAVE (E2EE) protocol, which is implemented through a CGO
binding to the native `libdave` library. The Docker build installs it automatically;
a local `go build` needs it on the host first:

```sh
sh "$(go env GOMODCACHE)/github.com/disgoorg/godave@v0.3.0/scripts/libdave_install.sh" v1.1.0
export PKG_CONFIG_PATH="$HOME/.local/lib/pkgconfig:$PKG_CONFIG_PATH"
```

Without it the build fails with `Package dave was not found in the pkg-config search
path`. The required libdave version is pinned by the `godave/libdave` module — see
`docs/disgo.md` §9 for details.

## External binaries

Voice playback shells out to `ffmpeg` to decode audio into the PCM that gets encoded
to opus. The image copies statically linked `ffmpeg` and `ffprobe` binaries from
`mwader/static-ffmpeg`, pinned **by digest** rather than tag, so every rebuild gets a
byte-identical binary with no runtime library dependencies and no dependence on
Debian's archive retention.

To move to a new ffmpeg release, update both the tag and the digest in the Dockerfile:

```sh
docker pull mwader/static-ffmpeg:<version>
docker image inspect mwader/static-ffmpeg:<version> --format '{{index .RepoDigests 0}}'
```

For local runs, any `ffmpeg` on `PATH` works.

## Docs

- [`docs/disgo.md`](docs/disgo.md) — working reference for the disgo library

Library Credits:
- https://github.com/lrstanley/go-ytdlp
- https://github.com/disgoorg/disgo
- https://github.com/disgoorg/godave
