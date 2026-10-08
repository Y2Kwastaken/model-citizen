# 01 – The API landscape

What the OpenAI API offers, which parts matter for model-citizen, and how it fits
local servers that speak the same protocol.

## The endpoints at a glance

| API | Path | What it's for | Relevant here? |
| --- | --- | --- | --- |
| **Chat Completions** | `POST /v1/chat/completions` | Send a list of messages, get the next assistant message. Tools, streaming, JSON output. | **Yes, this is the main one** |
| Responses | `POST /v1/responses` | OpenAI's newer API. Same job, plus server-side conversation state and built-in tools (web search, file search, code interpreter). | Maybe later; see below |
| Realtime | WebSocket / WebRTC | Low-latency speech-in/speech-out sessions with a voice model. | Interesting for voice, but a different architecture |
| Audio: transcriptions | `POST /v1/audio/transcriptions` | Speech → text (Whisper / `gpt-4o-transcribe`). | Voice input |
| Audio: speech | `POST /v1/audio/speech` | Text → speech (TTS). Returns mp3/opus/wav/**pcm** (24 kHz s16le). | Voice output |
| Embeddings | `POST /v1/embeddings` | Text → vector, for similarity search. | Long-term memory / retrieval |
| Moderation | `POST /v1/moderations` | Flags harmful text and images. Free on OpenAI. | Optional safety filter |
| Models | `GET /v1/models` | Lists the model IDs you can use. | Handy for config validation |

Everything else in the SDK (fine-tuning, batch, files, vector stores, assistants,
admin, containers, video, webhooks) you can ignore for now.

## Chat Completions vs Responses

| | Chat Completions | Responses |
| --- | --- | --- |
| State | **Stateless.** You send the whole history every call. | Can be stateful: `previous_response_id` or a `Conversation` keeps history on OpenAI's side. |
| Output shape | `choices[0].message` (content + `tool_calls`) | A list of typed `output` items (`message`, `function_call`, `reasoning`, ...) |
| Built-in tools | Web search only (via special models) | Web search, file search, code interpreter, MCP, image gen, ... |
| Local servers | **Supported almost everywhere** (Ollama, llama.cpp, vLLM, LM Studio) | Support is partial and varies |
| Status | "Supported indefinitely" (per the SDK README) | OpenAI's recommended API for new projects |

**Recommendation:** stay on Chat Completions. `llm/model/chat.go` already uses it,
stateless fits your own `memory` package (you decide exactly what history the
model sees), and it's what every OpenAI-compatible local server implements. That
matters for the small tool-routing model, which you'll probably run locally.

Migration guide, if you switch later: <https://developers.openai.com/api/docs/guides/migrate-to-responses>

## OpenAI-compatible servers

The SDK is just an HTTP client. Point it at another base URL and it talks to
anything that implements `/v1/chat/completions`:

| Server | Default base URL | Docs |
| --- | --- | --- |
| OpenAI | `https://api.openai.com/v1/` | <https://developers.openai.com/api/docs> |
| Ollama | `http://localhost:11434/v1/` | <https://docs.ollama.com/api/openai-compatibility> |
| llama.cpp `llama-server` | `http://localhost:8080/v1/` | <https://github.com/ggml-org/llama.cpp/blob/master/tools/server/README.md> |
| vLLM | `http://localhost:8000/v1/` | <https://docs.vllm.ai/en/latest/serving/openai_compatible_server.html> |

Things that commonly **differ** on local servers, so check your server's docs:

- The `developer` role. Use `system`; every server understands it.
- `strict` tool schemas and `response_format: json_schema`. llama.cpp and vLLM
  enforce them with grammars. Others may ignore `strict`.
- `parallel_tool_calls`, `stream_options.include_usage`, `prompt_cache_key`,
  `reasoning_effort`, `service_tier`: often ignored.
- Model names are whatever the server calls them (`qwen3:4b`), not the
  `openai.ChatModel...` constants. `Model` is just a string, so pass the name.
- The API key is often ignored, but the SDK still wants one, so pass any string.

## Key concepts in one paragraph each

**Tokens.** Models read and write tokens (roughly ¾ of an English word). Context
windows, prices, and limits are all in tokens. `usage` on every response tells
you how many you spent.

**Context window.** The max tokens of input + output in one call. Since Chat
Completions is stateless, your history grows each turn until you trim it. That's
what your short-term memory's circular queue is for.

**Roles.** `system`/`developer` = instructions, `user` = human input,
`assistant` = model output (including its tool calls), `tool` = a tool result
sent back to the model. See [03 – Chat Completions](03-chat-completions.md).

**Prompt caching.** On OpenAI, if the *start* of your prompt (≥1024 tokens)
matches a recent request, those tokens are cached: cheaper and faster. Keep
static content (system prompt, tool definitions) first and changing content
(history, new message) last. Check `usage.prompt_tokens_details.cached_tokens`.
Local servers have their own equivalent (llama.cpp's KV cache reuse), which also
favours a stable prefix. <https://developers.openai.com/api/docs/guides/prompt-caching>

**Reasoning models.** Some models (GPT-5 family, o-series, Qwen3 with thinking
on) "think" before answering. That adds latency, which matters for voice. On
OpenAI you can turn it down with `reasoning_effort` (`"none"`, `"minimal"`, `"low"`, ...; which values a model accepts varies).
<https://developers.openai.com/api/docs/guides/reasoning>
