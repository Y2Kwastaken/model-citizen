# model-citizen (v2)

Discord AI chatbot with voice, written in Go 1.27. Two services that talk over gRPC, plus a shared module:

| Module | What |
| --- | --- |
| `discord-bot/` | disgo v0.19.6 + golibdave (DAVE). Slash commands and llm tools both come from `actions/`; the music player and hand-written mixer; voice listening, wake word (`wake/`, onnxruntime) and speaking replies (`events/`) |
| `llm/` | openai-go v3. Chat with client rotation, memory, tool calling, personalities, speech to text and text to speech. Serves the gRPC services in `server/` |
| `shared/` | the pull-based mixer (`audio/`), `Rotation`, `CircularQueue`, and the gRPC contract: `network/proto/modelcitizen/v1/*.proto` (chat, memory, tools, speech) generated into `network/gen/` |

Each module has its own `go.mod`; `discord-bot` and `llm` import `shared` through a `replace`.

- **Repo:** github.com/Y2Kwastaken/model-citizen. `main` is v2; v1's history is on the `v1` branch. The old v1 checkout
  sits next to this folder at `../model-citizen` (useful for reference, its `main` no longer matches the remote).
- **Commits:** no Claude `Co-Authored-By` (or any other attribution) lines in commits or PRs.
- **Config:** `config/model-citizen.toml` is the bot's, every value shown is its default. `config/llm.toml` is the
  llm's, no defaults and every key required; `[stt]` and `[tts]` are optional sections. Personality prompts live in
  `config/personalities/`. Secrets stay in `.env` (gitignored), named by `api_key_env` in `llm.toml`.
- **Run:** `docker compose up -d --build`. The bot reaches the llm at `llm:50051` (`LLM_ADDRESS`); the llm's port isn't
  published to the host.
- **Build and test:** build or test the bot with `-tags nolibopusfile` (as the Dockerfile does); outside Docker its
  `main` package also needs `libdave` on `PKG_CONFIG_PATH`. Wake word and listener tests skip unless `ONNXRUNTIME_LIB`
  points at `libonnxruntime.so` 1.29.x. After editing a `.proto`, run `go generate ./network` from `shared/`.
- **Who wrote the audio code:** the owner hand-wrote the mixer (`shared/audio/mixing.go`, `editors.go`, `math.go`,
  `steams.go`) and `discord-bot/audio/opus.go`. The owner handed the wake word, listening and STT/TTS to Claude; those
  files start with `// claude authored, kept separate from the hand written audio code`. Keep that split, see Audio
  below.
- Internal notes: `_docs/disgo-voice/` (disgo voice internals) and `_docs/openai/` (openai-go and the chat API).

## Working with the owner

- The owner wrote this by hand to understand it. Code you add has to be **obvious**: plain control flow, small
  functions, no cleverness, no abstractions that aren't paying for themselves. Repeated logic is worth an abstraction.
- **Ask early.** If a styling, design or architecture choice might not be what the owner wants, ask before writing it,
  not after. Offer a recommended option.
- **Match the surrounding file** over anything below: naming, comment density, ordering, error handling.
- **Keep answers short.** Lead with the result, then what changed and what to watch for.
- Report honestly: what was tested and how, what wasn't, and anything you changed that wasn't asked for.

## Coding guide

### Style

- Comments only where something isn't obvious, short and lowercase, starting with what it does:
  `// fetches the message history for the source's channel`. No package doc comments, no comments restating the code.
- Struct fields that need explaining get a one-line comment above them. A mutex sits next to what it guards and is named
  for it: `lock`, `glock`, `slock`.
- Constructors are `NewX(...)`. Related constants go in one `const ( ... )` block, each with a short comment if it's a
  tuning value.
- Errors: return them, wrapping with `fmt.Errorf("doing x: %w", err)` when the context helps. Collect validation errors
  with `errors.Join`.
- Logging is `log/slog` with typed attributes and snake_case keys: `slog.Error("sending chat reply",
  slog.String("channel_id", channel.String()), slog.Any("error", err))`. Messages are lowercase and say what was being
  done. Log timings as `slog.Duration`.
- `gofmt` and `go vet` must be clean. Run tests with `-race`.

### Architecture

- **gRPC contract first.** A new capability between bot and llm starts in a `.proto` (one service per area, a
  `XRequest`/`XResponse` pair per rpc so it passes buf lint), then `go generate ./network`, then the llm server in
  `llm/server/` and the bot client in `discord-bot/network/`.
- gRPC errors are status codes, not fields: `InvalidArgument` for bad input, `Unavailable` when every client failed,
  `Unimplemented` for a feature that's switched off, `FailedPrecondition` for "can't in this state". Pass a caller's
  deadline through with `status.FromContextError`.
- **Rotations.** Anything backed by outside services (chat, STT, TTS) is a list of clients behind
  `shared.Rotation`: a failing client is benched with `Fail`, a working one is `Judge`d on latency, and the next client
  is tried. The `failover` helper in `llm/server/speech.go` is the pattern.
- **Config.** `config/model-citizen.toml` (bot) has a default for every key and the file shows them all.
  `config/llm.toml` has no defaults: every key is required, optional sections are all-or-nothing once present, unknown
  keys and unset `api_key_env` variables stop startup. Add a key to the struct, `Default()` or the required list,
  validation, and the toml with a comment, together. Settings are read once and never change at runtime.
- **Bot features are actions.** Write it once in `discord-bot/actions/`; it becomes both a slash command
  (`router.CommandsFrom`) and an llm tool (`tools.ToolsFrom`). A long action checks `Request.Background` and finishes
  its slow work in a goroutine when the llm is waiting.
- Never block a disgo callback or the 20 ms audio tick: hand work to a goroutine or a buffered channel with a
  non-blocking send.

### Testing

- Test against fakes, not paid services: `httptest` servers for HTTP APIs, an in-memory or local gRPC server for rpcs.
  Real API calls only to diagnose something, and say so.
- The owner removed tests from `llm/config`; don't add permanent tests there without asking. Throwaway tests used to
  check something are deleted afterwards.

## Audio

The owner is learning audio encoding, decoding and mixing through this project.

- **Their audio code is theirs to write.** In the mixer and other unmarked audio files, don't write or rewrite code
  unasked: explain the problem, point at the line and the concept, and let them fix it. Write audio code only when
  explicitly asked, and explain it.
- Claude-authored audio files (marked at the top) are fine to change, but keep them separate and don't edit the
  owner's audio files to fit them.
- Libraries are fine for resampling and Opus. The mixer is hand-written and pull-based: each 20 ms tick pulls one
  frame (960 samples per channel, 48 kHz stereo) from every layer, pads underruns with silence, sums in float32 and
  clamps. Scope is programmatic mixing (sum, gain, dB, ramps, ducking, clipping), not EQ or compression.
