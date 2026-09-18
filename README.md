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
| `SPEECH_MODEL_FILE` | Optional path to the speech rotation file, defaults to `speech-models.json` |
| `ONNXRUNTIME_LIB` | Optional path to `libonnxruntime.so`, defaults to where the image installs it |
| `WAKE_DIR` | Optional directory holding the wake word models, defaults to `wakeword` |
| `WAKE_THRESHOLD` | Optional score (0–1) that counts as the wake word, defaults to `0.2` |

### Model rotation

The bot cycles through the chat models in `data/text-models.json`, the
transcription models in `data/voice-models.json` and the voices in
`data/speech-models.json`, which compose mounts into the container. All three
share one format:

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
service before you have credentials for it. A service that has no key at all,
one running alongside the bot rather than over the web, says so with
`"auth_key": "NOP"` and is kept as it is — that way a key which is simply
missing still looks like a mistake.

Everything is assumed to speak OpenAI's API. The two transcription services
worth using that do not get an `api` field instead:

```json
[
  {
    "name": "nova-3",
    "base_url": "https://api.deepgram.com",
    "auth_key": "DEEPGRAM_KEY",
    "api": "deepgram"
  },
  {
    "name": "universal",
    "base_url": "https://api.assemblyai.com",
    "auth_key": "ASSEMBLYAI_KEY",
    "api": "assemblyai"
  }
]
```

`api` may be `openai` (the default), `mistral`, `deepgram` or `assemblyai`, and
`name` is whatever that service calls its model. Each roster is loaded for one
feature and every service in it is checked against that feature, so a voice
listed among the chat models is an error at startup rather than a request that
fails later. Teaching the bot another service is an entry in `apis` in
`llm/model/apis.go` plus the function it names. Deepgram answers in one round trip;
AssemblyAI queues the clip and is polled until it is done or the model's ten
seconds are up, so it belongs last in the rotation.

### Speech

`data/speech-models.json` is the bot's voice:

```json
[
  {
    "name": "voxtral-mini-tts-2603",
    "base_url": "https://api.mistral.ai/v1",
    "auth_key": "MISTRAL_KEY",
    "api": "mistral",
    "voice": "gb_oliver_neutral"
  }
]
```

Speech is the one feature where the model and the voice are separate, which is
what `voice` is for. `gb_oliver_neutral` is a British man; `GET /v1/audio/voices`
lists the rest, and `en_paul_*` covers eight moods of the same American. The
model is pinned rather than `voxtral-mini-tts-latest`, because an alias moves
and the voice is the bot's whole character to whoever is listening.

Nothing here runs locally. A voice worth listening to wants more compute than
the box has: kokoro took eight to twelve seconds per line on two cores, against
`perModelTimeout`'s ten, and piper is fast enough but sounds it.

A rotation fails over, so treat more than one entry here as an outage plan
rather than a choice -- a swapped voice mid-conversation is worse than a pause.

The bot speaks over music rather than interrupting it. Discord takes one opus
stream per connection, so `audio.Mixer` sits between ffmpeg and the encoder and
sums the two, ducking the music while a line plays; with nothing playing the
same mixer carries the line alone. `/say` is there to try a voice without going
through the wake word.

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
