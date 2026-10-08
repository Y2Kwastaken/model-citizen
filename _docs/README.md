# model-citizen docs

Internal notes for the model-citizen bot.

> This folder is named `_docs` rather than `docs` so it sorts to the top of the
> VS Code explorer (`_` sorts before letters, so it lands above `discord-bot/`).

## Contents

### disgo voice / audio

Notes on how [disgo](https://github.com/disgoorg/disgo)'s `voice` package works:
connecting, the packet format, and how audio gets received and sent. They were
written by reading the source of **disgo v0.19.6** (the version pinned in
`discord-bot/go.mod`) and **godave v0.1.0 / golibdave v0.3.0**.

| Doc | What it covers |
| --- | --- |
| [01 – Architecture](disgo-voice/01-architecture.md) | The pieces (`Manager`, `Conn`, `Gateway`, `UDPConn`, sender/receiver) and how they're wired into `bot.Client` |
| [02 – Connection lifecycle](disgo-voice/02-connection-lifecycle.md) | The handshake from `conn.Open()` to the first audio packet, heartbeats, reconnects, close codes |
| [03 – Packet format](disgo-voice/03-packet-format.md) | RTP header layout, transport encryption, DAVE, what `ReadPacket()` does byte by byte |
| [04 – Receiving audio](disgo-voice/04-receiving-audio.md) | Listening for packets: raw `ReadPacket` loops vs `OpusFrameReceiver`, SSRC → user mapping, processing patterns |
| [05 – Sending audio](disgo-voice/05-sending-audio.md) | `OpusFrameProvider`, the 20 ms send loop, speaking flags, silence frames |
| [06 – Gotchas & caveats](disgo-voice/06-gotchas.md) | Buffer aliasing, thread safety, known quirks in the library, and what this project needs to change to use voice |

Source paths in these docs are relative to the disgo module root, i.e.
`$(go env GOMODCACHE)/github.com/disgoorg/disgo@v0.19.6/`.

### OpenAI API (chat, tools)

A practical rundown of the OpenAI API as used from Go, focused on Chat
Completions and tool calling. Written against **openai-go v3.57.0** (pinned in
`llm/go.mod`); every Go snippet was compile-checked against that version.
Also covers OpenAI-compatible local servers (Ollama, llama.cpp, vLLM).

| Doc | What it covers |
| --- | --- |
| [01 – Landscape](openai/01-landscape.md) | Which endpoints exist, Chat Completions vs Responses, local servers, tokens/caching/reasoning |
| [02 – Go SDK](openai/02-go-sdk.md) | Client options, `param.Opt`, unions, `.JSON` metadata, errors, retries, cancellation |
| [03 – Chat Completions](openai/03-chat-completions.md) | Roles, parameters, the response, `finish_reason`, managing history |
| [04 – Streaming](openai/04-streaming.md) | SSE chunks, `ChatCompletionAccumulator`, sentence chunking for TTS, barge-in |
| [05 – Tool calling](openai/05-tool-calling.md) | Defining tools, `tool_choice`, the tool loop, the two-lane (tool model + chat model) design |
| [06 – Structured outputs](openai/06-structured-outputs.md) | JSON mode / JSON schema replies, tools vs schemas for routing |
| [07 – Other APIs & links](openai/07-other-apis-and-links.md) | STT, TTS (24 kHz PCM), Realtime, embeddings, moderation, and a full link list |
