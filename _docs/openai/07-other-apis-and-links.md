# 07 – Other APIs and links

Short notes on the rest of the API that's likely to matter later, plus a link
list.

## Speech-to-text (voice input)

```go
f, _ := os.Open("utterance.wav")
tr, err := client.Audio.Transcriptions.New(ctx, openai.AudioTranscriptionNewParams{
	File:  f,
	Model: openai.AudioModelGPT4oMiniTranscribe, // or "whisper-1"
})
fmt.Println(tr.Text)
```

Takes a whole file (wav/mp3/ogg/...), so you need to detect the end of the
utterance yourself (VAD / silence detection). Discord gives you Opus. Decode it to
PCM and wrap it as WAV, or send Ogg/Opus.
Guide: <https://developers.openai.com/api/docs/guides/speech-to-text>.
Local alternative: whisper.cpp.

## Text-to-speech (voice output)

```go
res, err := client.Audio.Speech.New(ctx, openai.AudioSpeechNewParams{
	Model:          openai.SpeechModelGPT4oMiniTTS,
	Voice:          openai.AudioSpeechNewParamsVoiceUnion{OfString: openai.String("alloy")},
	Input:          sentence,
	ResponseFormat: openai.AudioSpeechNewParamsResponseFormatPCM,
})
defer res.Body.Close()
// res.Body streams raw PCM: 24 kHz, 16-bit signed little-endian, mono
```

`pcm` output is **24 kHz mono s16le**. To feed your mixer (48 kHz stereo) you
resample 24 → 48 kHz and duplicate to two channels. This is the resampling case in
the audio lesson plan. `res.Body` is an `io.Reader`, which fits the
`bufferedSource` design.
Guide: <https://developers.openai.com/api/docs/guides/text-to-speech>.

## Realtime API

A WebSocket/WebRTC session where a voice model listens and speaks directly
(speech-to-speech), with server-side VAD and interruptions. It's a whole
different design from STT → chat → TTS, and it's tied to OpenAI's models. Worth
knowing it exists. Guide: <https://developers.openai.com/api/docs/guides/realtime>.

## Embeddings (long-term memory)

```go
emb, err := client.Embeddings.New(ctx, openai.EmbeddingNewParams{
	Model: openai.EmbeddingModelTextEmbedding3Small,
	Input: openai.EmbeddingNewParamsInputUnion{OfString: openai.String(text)},
})
vec := emb.Data[0].Embedding // []float64
```

Store vectors next to memories. At query time, embed the new message and pick the
most similar memories (cosine similarity) to put in the prompt. Ollama and
llama.cpp also serve `/v1/embeddings` with local models (e.g. `nomic-embed-text`).
Guide: <https://developers.openai.com/api/docs/guides/embeddings>.

## Moderation

`client.Moderations.New(...)` returns per-category flags. It's free on OpenAI.
Guide: <https://developers.openai.com/api/docs/guides/moderation>.

## Listing models

```go
page, err := client.Models.List(ctx)
for _, m := range page.Data { fmt.Println(m.ID) }
```

Works on Ollama/llama.cpp too. Good for checking config at startup ("model X
isn't loaded").

---

## Links

### OpenAI docs (developers.openai.com; old platform.openai.com links redirect here)

| Topic | Link |
| --- | --- |
| Docs home | <https://developers.openai.com/api/docs> |
| Text generation | <https://developers.openai.com/api/docs/guides/text> |
| Conversation state | <https://developers.openai.com/api/docs/guides/conversation-state> |
| Function calling | <https://developers.openai.com/api/docs/guides/function-calling> |
| Structured outputs | <https://developers.openai.com/api/docs/guides/structured-outputs> |
| Streaming | <https://developers.openai.com/api/docs/guides/streaming-responses> |
| Prompt caching | <https://developers.openai.com/api/docs/guides/prompt-caching> |
| Latency optimization | <https://developers.openai.com/api/docs/guides/latency-optimization> |
| Prompt engineering | <https://developers.openai.com/api/docs/guides/prompt-engineering> |
| Reasoning models | <https://developers.openai.com/api/docs/guides/reasoning> |
| Speech-to-text | <https://developers.openai.com/api/docs/guides/speech-to-text> |
| Text-to-speech | <https://developers.openai.com/api/docs/guides/text-to-speech> |
| Realtime | <https://developers.openai.com/api/docs/guides/realtime> |
| Embeddings | <https://developers.openai.com/api/docs/guides/embeddings> |
| Moderation | <https://developers.openai.com/api/docs/guides/moderation> |
| Error codes | <https://developers.openai.com/api/docs/guides/error-codes> |
| Rate limits | <https://developers.openai.com/api/docs/guides/rate-limits> |
| Models | <https://developers.openai.com/api/docs/models> |
| Pricing | <https://developers.openai.com/api/docs/pricing> |
| Chat Completions API reference | <https://developers.openai.com/api/reference/resources/chat/subresources/completions/methods/create> |
| Migrating to Responses | <https://developers.openai.com/api/docs/guides/migrate-to-responses> |

### Go SDK

| | Link |
| --- | --- |
| Repo / README | <https://github.com/openai/openai-go> |
| Full method + type list | <https://github.com/openai/openai-go/blob/main/api.md> |
| godoc | <https://pkg.go.dev/github.com/openai/openai-go/v3> |
| Local source (pinned) | `$(go env GOMODCACHE)/github.com/openai/openai-go/v3@v3.57.0/` |

Files in the SDK source worth opening: `chatcompletion.go` (all chat
types, about 4k lines; search for the type name), `streamaccumulator.go`,
`option/requestoption.go`, `README.md`.

### Local servers

| | Link |
| --- | --- |
| Ollama OpenAI compatibility | <https://docs.ollama.com/api/openai-compatibility> |
| llama.cpp server | <https://github.com/ggml-org/llama.cpp/blob/master/tools/server/README.md> |
| vLLM OpenAI server | <https://docs.vllm.ai/en/latest/serving/openai_compatible_server.html> |

### Other

- JSON Schema: <https://json-schema.org/understanding-json-schema>
