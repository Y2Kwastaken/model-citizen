# Model Citizen

This discord bot is truly a model citizen

## Running

```sh
docker compose up --build
```

Requires `.env` (copy `.env.example`) with:

| Variable | Purpose |
| --- | --- |
| `DISCORD_KEY` | Discord bot token |
| `MODEL_CONFIG`, `MUSIC_CONFIG`, `VOICE_CONFIG` | Optional paths to the config files, see below |

plus one key per service the model rosters name in `auth_key`: `MODEL_AUTH_KEY`
(NVIDIA NIM `nvapi-...`), `GROQ_KEY`, `OLLAMA_MODEL_KEY`, `MISTRAL_KEY`,
`DEEPGRAM_KEY`, `ASSEMBLYAI_KEY` and `GOOGLE_KEY` in the committed rosters.

### Configuration

Everything else lives in [`config/`](config/config.md), one documented file per
area:

| File | What it covers |
| --- | --- |
| [`config/model.json`](config/model.md) | Personalities, sampling, reply limits, memory, model rotation, wake word |
| [`config/music.json`](config/music.md) | Queue, cache, prefetch, volume |
| [`config/voice.json`](config/voice.md) | How voice commands are heard and transcribed |
| [`config/models/`](config/models/config.md) | The chat, transcription and speech model rosters |

`config/` is copied into the image, so changing it means `docker compose up --build`.

The bot will not start without a chat roster it can load or a wake word model.

### Speech

The voice is Google's Chirp 3 HD, set in `config/models/tts-models.json`. The
first million characters a month are free, which is thousands of lines, and
Google does not read the text before saying it.

Mistral's Voxtral was the voice before this. It sounds good and costs nothing,
but it runs the text past a content filter first and refused the persona often
enough to matter.

Nothing here runs locally. A voice worth listening to wants more compute than
the box has: kokoro took eight to twelve seconds per line on two cores, against
the ten-second `per_model_timeout_seconds`, and piper is fast enough but sounds it.

The bot speaks over music rather than interrupting it. Discord takes one opus
stream per connection, so `audio.Mixer` sits between ffmpeg and the encoder and
sums the two, ducking the music while a line plays; with nothing playing the
same mixer carries the line alone. `/say` is there to try a voice without going
through the wake word.

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

- [`config/config.md`](config/config.md) — every config file and what it tunes
- [`docs/disgo.md`](docs/disgo.md) — working reference for the disgo library

Library Credits:
- https://github.com/lrstanley/go-ytdlp
- https://github.com/disgoorg/disgo
- https://github.com/disgoorg/godave
