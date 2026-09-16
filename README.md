# Model Citizen

This discord bot is truly a model citizen

## Running

```sh
docker compose up --build
```

Requires `data/.env` with:

| Variable | Purpose |
| --- | --- |
| `DISCORD_KEY` | Discord bot token |
| `MODEL_AUTH_KEY` | API key for the chat model (NVIDIA NIM `nvapi-...`) |
| `MODEL_NAME` | Fallback model id, e.g. `nvidia/nemotron-3.5-lightning-30b-a3b` |
| `MODEL_LINK` | Fallback base URL, e.g. `https://integrate.api.nvidia.com/v1` |
| `MODEL_FILE` | Optional path to the text rotation file, defaults to `text-models.json` |
| `VOICE_MODEL_FILE` | Optional path to the voice rotation file, defaults to `voice-models.json` |

### Model rotation

The bot cycles through the chat models in `data/text-models.json` and the
transcription models in `data/voice-models.json`, which compose mounts into the
container. Both share one format:

```json
[
  {
    "name": "nvidia/nemotron-3.5-lightning-30b-a3b",
    "base_url": "https://integrate.api.nvidia.com/v1",
    "auth_key": "MODEL_AUTH_KEY"
  }
]
```

`auth_key` is the *name* of the environment variable holding that model's key,
never the key itself — the roster is committed, `data/.env` is not. Each entry
resolves its own variable, so pointing a model at a different service is a new
entry plus a new line in `data/.env`. A model whose variable is unset is logged
and dropped from the rotation rather than stopping the bot, so you can list a
service before you have credentials for it.

Every reply is timed. A fast model works its score down, a slow one works it up,
and a model that crosses the score or errors outright is benched — the request
is retried on the next model rather than failing. Benched models come back after
five minutes, because an endpoint being down is nearly always temporary.

`MODEL_NAME` and `MODEL_LINK` are only the fallback for when the file is missing
or unreadable, so the bot still starts with a rotation of one.

Chat replies also need the **Message Content** privileged intent enabled in the
Discord developer portal (Bot -> Privileged Gateway Intents). It is declared in
`modelbot.go`, but Discord blanks every message's content until it is toggled on
there too, so the bot silently ignores mentions without it.

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
