# model-citizen

A better version of model-citizen a hand crafted and well understood beauty.

This is a big v2 upgrade of the previous model-citizen repository the rewrite largely contains ground up design choices implemented by hand.
Originally this project was coded by AI in some pretty major areas. That said after getting something working I wanted to get my hands dirty when it
comes to audio encoding and translation it was an area I had very little knowledge or experience in. I'm hope this project can push me much closer to
knowledge in this area.

At it's heart model citizen is a project driven to create an interactive and customziable AI chatbot for your server. Everything from the personality
to the discord bot will provide some level of configuration I hope both you and I as a user will find satisfactory.

v1 is kept on the [`v1` branch](https://github.com/Y2Kwastaken/model-citizen/tree/v1).

## What it does

- **Chats.** @mention the bot in a text channel and it replies, with a short history of each channel and notes it
  chooses to remember.
- **Listens.** While it's in a voice channel, say **"hey model"** and it transcribes what you and the people around you
  just said, then answers in the voice channel's chat and out loud.
- **Personalities with their own voices.** Each personality is a system prompt and a voice. It can switch personality
  when asked.
- **Plays music.** `/join`, `/play <song>`, `/skip`, `/stop`, `/leave`. The model can do the same when you ask it in
  chat or by voice.
- **Fails over.** Chat, speech to text and text to speech each run through a list of providers. A provider that errors,
  answers slowly or returns nothing is benched for a while and the next one is used.

## How it's built

Two services in Go that talk over gRPC:

| | |
| --- | --- |
| `discord-bot/` | The Discord side: commands, the music player and its hand-written mixer, voice listening and the wake word |
| `llm/` | The brain: chat models, memory, tools, personalities, speech to text and text to speech |
| `shared/` | The audio mixer, the provider rotation, and the gRPC contract (`shared/network/proto`) |

## Setup

You need Docker with compose.

1. **Keys.** Copy `.env.example` to `.env` and fill it in: `DISCORD_KEY`, plus a key for every provider listed in
   `config/llm.toml`. The llm won't start while one is missing.
2. **Discord.** In the developer portal, turn on the bot's **Message Content Intent** (Bot → Privileged Gateway
   Intents). Without it the bot can't read chat.
3. **Run it.**

   ```sh
   docker compose up -d --build
   ```

## Configuration

Both files are read once at startup; restart after changing them (`docker compose up -d --force-recreate`). Unknown
keys stop startup, so typos don't go unnoticed. Secrets never go in these files: each provider names the `.env`
variable that holds its key.

### `config/model-citizen.toml`: the Discord bot

Logging, voice, music and listening. Every key is optional, and the file ships with every default written out. To use
a different path, set `MODEL_CITIZEN_CONFIG`.

`[listen]` tunes the wake word and voice commands: `threshold` (how sure the wake word has to be), `quiet_level` (how
loud counts as talking, in dBFS), `command_quiet` (the pause that ends a command) and `context` (how much of the
conversation before the wake word goes along with it). With `level = "debug"`, `voice level` and `wake word near miss`
lines in the logs show the numbers to tune against.

### `config/llm.toml`: the brain

There are no defaults here: every key is required.

- `[chat]`: history size, timeouts, the personalities and the chat providers (`[[chat.clients]]`), tried in order.
- `[stt]`: speech to text providers. Optional; without it the bot doesn't understand voice.
- `[tts]`: voices, each a list of providers. Optional; without it the bot answers voice in text only.

A personality is a prompt file in `config/personalities/` and an optional voice:

```toml
[chat.personalities.cool]
prompt = "personalities/cool.md"
voice = "cool"
```

## Development

Each module (`discord-bot`, `llm`, `shared`) is its own Go module. Run `go vet` and tests with `-race` from inside one.

- Build and test the bot with `-tags nolibopusfile`, as the Dockerfile does.
- The wake word tests need onnxruntime 1.29.x: point `ONNXRUNTIME_LIB` at `libonnxruntime.so`, or they skip.
- After editing a `.proto`, regenerate from `shared/` with `go generate ./network`.
- The wake word model is trained with openWakeWord; the training setup is in v1's `wakeword/` folder.
