# Configuration

Everything the bot can be tuned with lives in this folder. Each file has its
own page:

| File | What it covers | Docs |
| --- | --- | --- |
| `model.json` | Personalities, sampling, reply limits, memory, model rotation, wake word | [model.md](model.md) |
| `music.json` | Queue, cache, prefetch, volume | [music.md](music.md) |
| `voice.json` | How voice commands are heard and transcribed | [voice.md](voice.md) |
| `models/*.json` | The chat, transcription and speech model rosters | [models/README.md](models/README.md) |
| `personalities/*.md` | The personality prompts, listed in `model.json` | [model.md](model.md#personalities) |

`config/` is copied into the image, so changing any of it means
`docker compose up --build`.

## Where the bot looks

Each top-level file can be pointed elsewhere with an environment variable:

| Variable | Default |
| --- | --- |
| `MODEL_CONFIG` | `config/model.json` |
| `MUSIC_CONFIG` | `config/music.json` |
| `VOICE_CONFIG` | `config/voice.json` |

File paths inside `model.json` are relative to `model.json` itself.

## Validation

Every file is checked at startup, and a value out of range stops the bot with
an error naming the key. There are no built-in defaults: a key left out reads as
`0` or empty, which fails most checks, so keep every key in the file. The
defaults listed in each page are the values the committed files ship with.
