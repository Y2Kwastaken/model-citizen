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
| `MODEL_CONFIG` | Optional path to the bot's config, defaults to `config/model.json` |

plus one key per service the model rosters name in `auth_key`: `MODEL_AUTH_KEY`
(NVIDIA NIM `nvapi-...`), `OLLAMA_MODEL_KEY`, `MISTRAL_KEY`, `DEEPGRAM_KEY`,
`ASSEMBLYAI_KEY` and `GOOGLE_KEY` in the committed rosters.

### Configuration

Everything else lives in `config/model.json`. File paths in it are relative to
the config file itself:

```json
{
    "default_personality": "default",
    "personalities": {
        "default": "personalities/default-personality.md",
        "smug": "personalities/smug-personality.md",
        "degenerate": "personalities/degenerate-personality.md"
    },
    "text-models": "models/text-models.json",
    "tts-models": "models/tts-models.json",
    "stt-models": "models/stt-models.json",
    "history_size": 20,
    "wake_word_threshold": 0.12,
    "wake_word_directory": "wakeword",
    "wake_word_file": "hey_model.onnx",
    "wake_word_runtime": "/usr/local/lib/libonnxruntime.so"
}
```

| Field | Purpose |
| --- | --- |
| `default_personality` | Which entry of `personalities` is the system prompt |
| `personalities` | Name to a markdown prompt file; add a file and an entry to add a personality |
| `text-models`, `stt-models`, `tts-models` | The chat, transcription and speech rosters, see below |
| `history_size` | Messages kept per channel |
| `wake_word_threshold` | Score (0–1) that counts as "hey model"; lower hears more and false-triggers more, and anything under `0.1` is raised to it |
| `wake_word_directory`, `wake_word_file` | Where the wake word model lives |
| `wake_word_runtime` | Path to `libonnxruntime.so`; the default is where the image installs it |

`config/` is copied into the image, so changing it means `docker compose up --build`.

The bot will not start without a chat roster it can load or a wake word model.

### Model rotation

The bot cycles through the chat models in `config/models/text-models.json`, the
transcription models in `config/models/stt-models.json` and the voices in
`config/models/tts-models.json`. All three share one format:

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
never the key itself — the roster is committed, `.env` is not. Each entry
resolves its own variable, so pointing a model at a different service is a new
entry plus a new line in `.env`. A model whose variable is unset is logged
and dropped from the rotation rather than stopping the bot, so you can list a
service before you have credentials for it. A service that has no key at all,
one running alongside the bot rather than over the web, says so with
`"auth_key": "NOP"` and is kept as it is — that way a key which is simply
missing still looks like a mistake. A transcription or speech roster that can't
be loaded turns that feature off; the chat roster is required.

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

`api` may be `openai` (the default), `mistral`, `google`, `deepgram` or `assemblyai`, and
`name` is whatever that service calls its model. Each roster is loaded for one
feature and every service in it is checked against that feature, so a voice
listed among the chat models is an error at startup rather than a request that
fails later. Teaching the bot another service is an entry in `apis` in
`llm/model/apis.go` plus the function it names. Deepgram answers in one round trip;
AssemblyAI queues the clip and is polled until it is done or the model's ten
seconds are up, so it belongs last in the rotation.

### Speech

`config/models/tts-models.json` is the bot's voice:

```json
[
  {
    "name": "en-GB-Chirp3-HD-Charon",
    "base_url": "https://texttospeech.googleapis.com/v1",
    "auth_key": "GOOGLE_KEY",
    "api": "google"
  }
]
```

Google's voices have no model apart from the voice, so `name` is the voice
and there is no `voice` field. `GET /v1/voices?languageCode=en-GB` with the
key in `x-goog-api-key` lists them; the Chirp 3 HD set (Charon, Fenrir,
Puck, Kore, Aoede, ...) exists under every `en-*` locale. The first million
characters a month are free, which is thousands of lines, and Google does
not read the text before saying it.

Mistral's Voxtral (`"api": "mistral"`, with the model in `name` and the
voice in `voice`, e.g. `gb_oliver_neutral`) was the voice before this. It
sounds good and costs nothing, but it runs the text past a content filter
first and refused the persona often enough to matter. Speech is the one
feature where the model and the voice are separate, which is what `voice`
is for on the services that have both; pin the model rather than an alias
like `voxtral-mini-tts-latest`, because an alias moves and the voice is the
bot's whole character to whoever is listening.

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
