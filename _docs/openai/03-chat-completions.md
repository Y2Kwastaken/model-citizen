# 03 – Chat Completions

`POST /v1/chat/completions`: you send a list of messages, and you get back the
next assistant message.

- Guide: <https://developers.openai.com/api/docs/guides/text>
- API reference: <https://developers.openai.com/api/reference/resources/chat/subresources/completions/methods/create>
- Conversation state (how history works): <https://developers.openai.com/api/docs/guides/conversation-state>

## The model is stateless

The model remembers nothing between calls. A "conversation" is just you sending
a longer `Messages` slice each time:

```
call 1: [system, user1]                                  -> assistant1
call 2: [system, user1, assistant1, user2]               -> assistant2
call 3: [system, user1, assistant1, user2, assistant2, user3] -> ...
```

So your `memory` package *is* the conversation. Whatever `Ordered()` returns,
turned into messages, is everything the model knows.

## Message roles

| Role | Constructor | Purpose |
| --- | --- | --- |
| `system` | `openai.SystemMessage(s)` | Instructions, persona, tool descriptions. Put it first. |
| `developer` | `openai.DeveloperMessage(s)` | OpenAI's newer name for system (used by reasoning models). OpenAI treats them alike. **Local servers may not know it, so use `system`.** |
| `user` | `openai.UserMessage(s)` | Human input |
| `assistant` | `openai.AssistantMessage(s)` or `msg.ToParam()` | Previous model output. Use `ToParam()` so tool calls are kept. |
| `tool` | `openai.ToolMessage(result, toolCallID)` | Result of a tool call. Must come after the assistant message that requested it. |

### Multi-user chat (Discord)

The API only knows one "user". For a channel with several people, put the
speaker into the content:

```go
openai.UserMessage("[miles]: anyone up for a game?")
```

There is also an optional `name` field on user messages
(`openai.ChatCompletionUserMessageParam{Name: openai.String("miles")}`), but
many local chat templates drop it. Putting the speaker in the content always works.

Text + image in one message (vision models only):

```go
openai.UserMessage([]openai.ChatCompletionContentPartUnionParam{
	openai.TextContentPart("what's in this?"),
	openai.ImageContentPart(openai.ChatCompletionContentPartImageImageURLParam{URL: attachmentURL}),
})
```

## Request parameters worth knowing

| Field | Type in Go | Notes |
| --- | --- | --- |
| `Model` | `string` (`shared.ChatModel`) | Required |
| `Messages` | `[]ChatCompletionMessageParamUnion` | Required |
| `MaxCompletionTokens` | `openai.Int(n)` | Caps the output length. Includes hidden reasoning tokens on reasoning models. (`MaxTokens` is the old name; some local servers only read that one.) |
| `Temperature` | `openai.Float(x)` | 0 = predictable, ~0.7–1.0 = chatty. Use low for the tool model and higher for the chat persona. Some OpenAI reasoning models reject it. |
| `TopP` | `openai.Float(x)` | Alternative to temperature. Change one, not both. |
| `Stop` | `ChatCompletionNewParamsStopUnion{OfStringArray: ...}` | Stop generating at these strings |
| `FrequencyPenalty` / `PresencePenalty` | `openai.Float(x)` | −2..2. Discourages repetition. |
| `Seed` | `openai.Int(n)` | Best-effort reproducibility |
| `Tools`, `ToolChoice`, `ParallelToolCalls` | | See [05 – Tool calling](05-tool-calling.md) |
| `ResponseFormat` | | JSON mode / JSON schema. See [06 – Structured outputs](06-structured-outputs.md) |
| `ReasoningEffort` | `shared.ReasoningEffortLow` etc. | Reasoning models only. Lower = faster. |
| `StreamOptions` | `{IncludeUsage: openai.Bool(true)}` | Streaming only. See [04 – Streaming](04-streaming.md) |
| `PromptCacheKey` | `openai.String(k)` | OpenAI only. Groups requests that share a prefix (e.g. per guild) to improve cache hits. |
| `User` / `SafetyIdentifier` | `openai.String(id)` | OpenAI only. A stable per-user ID (hash it) for abuse tracking. |
| `N` | | Several alternative replies. You won't need it. |

Server-specific fields (llama.cpp `top_k`, `min_p`, `repeat_penalty`, ...):
`params.SetExtraFields(map[string]any{"top_k": 20})`.

## The response

```go
resp, err := client.Chat.Completions.New(ctx, params)
choice := resp.Choices[0]

choice.Message.Content    // the text ("" if the model only called tools)
choice.Message.ToolCalls  // []ChatCompletionMessageToolCallUnion
choice.Message.Refusal    // set if the model refused (OpenAI)
choice.FinishReason       // why it stopped, see below

resp.Usage.PromptTokens
resp.Usage.CompletionTokens
resp.Usage.PromptTokensDetails.CachedTokens      // prompt-cache hits
resp.Usage.CompletionTokensDetails.ReasoningTokens
```

### `finish_reason`: always check it

| Value | Meaning | Handle it by |
| --- | --- | --- |
| `stop` | Natural end or a `Stop` string | Normal case |
| `length` | Hit `MaxCompletionTokens` or the context limit | The reply is **cut off**. Trim history, or raise the limit. |
| `tool_calls` | The model wants tools run | Run them and call again. See [05](05-tool-calling.md). |
| `content_filter` | OpenAI's filter removed content | Don't post an empty reply. |

## Managing history (context window)

Each call resends everything, so cost and latency grow with history. Options,
from simplest to most involved:

1. **Sliding window:** keep the last N messages (your `NewShortTermMemory`).
   Always keep the system prompt.
2. **Token budget:** trim oldest-first until estimated tokens < budget. A rough
   estimate of `len(text)/4` is fine for a Discord bot.
3. **Summarize:** when history gets long, ask the model to summarize the old part
   and replace it with one message.

Trimming rules that avoid 400 errors:

- Never leave a `tool` message without the `assistant` message that has the
  matching `tool_call_id` before it. Drop them together.
- Don't start the history with an `assistant` message right after `system`.
  Some local chat templates reject it.

## Prompt caching tips (OpenAI, and local KV caches)

Order messages so the start is the same across calls:

```
[system prompt + tool list]  <- static, cached
[older history]               <- grows, mostly cached
[newest user message]          <- new
```

Don't put timestamps or "current time" in the system prompt. Doing so changes the
start of every request and breaks the cache. Put the time in the newest message
instead.
