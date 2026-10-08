# 04 – Streaming

Without streaming, you wait for the whole reply. With streaming, tokens arrive as
the model writes them, over Server-Sent Events (SSE). For voice this is the
difference between waiting ~3 s and starting TTS after ~300 ms.

- Guide: <https://developers.openai.com/api/docs/guides/streaming-responses> (switch the examples to "Chat Completions")
- Latency guide (worth reading for voice): <https://developers.openai.com/api/docs/guides/latency-optimization>

## Basic loop

```go
stream := client.Chat.Completions.NewStreaming(ctx, params)
defer stream.Close()

for stream.Next() {
	chunk := stream.Current()
	if len(chunk.Choices) == 0 {
		continue // the final usage chunk has no choices (see below)
	}
	delta := chunk.Choices[0].Delta
	fmt.Print(delta.Content) // a few characters at a time
}
if err := stream.Err(); err != nil {
	// network error, API error (*openai.Error), or context cancelled
}
```

`NewStreaming` doesn't return an error. Errors (including HTTP errors on the
first request) show up as `stream.Next() == false` and then `stream.Err()`.

## What a chunk looks like

Each chunk is a `ChatCompletionChunk` with `Choices[i].Delta`, which holds only
the **new** part:

```
{delta: {role: "assistant"}}
{delta: {content: "Hel"}}
{delta: {content: "lo the"}}
{delta: {content: "re!"}}
{delta: {}, finish_reason: "stop"}
{choices: [], usage: {...}}          <- only if include_usage is set
```

Tool calls stream too, with the arguments JSON arriving in pieces:

```
{delta: {tool_calls: [{index: 0, id: "call_abc", function: {name: "get_weather", arguments: ""}}]}}
{delta: {tool_calls: [{index: 0, function: {arguments: "{\"ci"}}]}}
{delta: {tool_calls: [{index: 0, function: {arguments: "ty\":\"Paris\"}"}}]}}
{delta: {}, finish_reason: "tool_calls"}
```

Only the first piece has `id` and `name`. Later pieces are matched by `index`.
You don't want to assemble that by hand, so use the accumulator.

## `ChatCompletionAccumulator`

It stitches chunks into a full `ChatCompletion` and tells you when a piece is done:

```go
acc := openai.ChatCompletionAccumulator{}

for stream.Next() {
	chunk := stream.Current()
	acc.AddChunk(chunk)

	if len(chunk.Choices) > 0 {
		speak(chunk.Choices[0].Delta.Content) // live text
	}
	if tc, ok := acc.JustFinishedToolCall(); ok {
		// tc.ID, tc.Name, tc.Arguments (complete JSON), tc.Index
	}
	if text, ok := acc.JustFinishedContent(); ok {
		_ = text // the full text, once text output is done
	}
}

full := acc.ChatCompletion        // same shape as a non-streamed response
history = append(history, full.Choices[0].Message.ToParam())
```

Exported fields and methods: `acc.Choices`, `acc.Usage`, `JustFinishedContent`,
`JustFinishedRefusal`, `JustFinishedToolCall` (source: `streamaccumulator.go`).

## Usage while streaming

Token counts aren't sent by default when streaming. Ask for them:

```go
params.StreamOptions = openai.ChatCompletionStreamOptionsParam{IncludeUsage: openai.Bool(true)}
```

The last chunk then has `Choices == []` and a filled `Usage`. That's why the loop
above checks `len(chunk.Choices)`. Without the check, `chunk.Choices[0]` panics.

## Streaming into TTS: chunk by sentence

TTS needs whole phrases, not 3-character deltas. Buffer the deltas and flush at
sentence boundaries:

```go
var buf strings.Builder
for stream.Next() {
	chunk := stream.Current()
	if len(chunk.Choices) == 0 {
		continue
	}
	buf.WriteString(chunk.Choices[0].Delta.Content)
	if s := buf.String(); endsSentence(s) && len(s) > 20 { // avoid tiny fragments like "Ok."
		ttsQueue <- s
		buf.Reset()
	}
}
if buf.Len() > 0 {
	ttsQueue <- buf.String()
}
```

`endsSentence` can be as simple as "last non-space rune is `.`, `!`, `?` or a
newline". Watch out for "e.g." and "3.14". The first sentence decides how fast
the bot *feels*, so it can be worth flushing the first chunk early, at a comma.

## Cancelling (barge-in, skip, newer message)

```go
ctx, cancel := context.WithCancel(parent)
// store cancel on the turn; call it when the user starts talking again
stream := client.Chat.Completions.NewStreaming(ctx, params)
```

Cancelling closes the HTTP connection. The server stops generating (local
servers free the slot), `Next()` returns false, and `Err()` is
`context.Canceled`. Treat that as a normal outcome, not a failure. Decide whether
the partial reply goes into history. Usually you'd store what was actually
spoken.

## Streaming over Discord text

You can't edit a message 50 times a second (rate limits). Either:

- Don't stream to text at all. Show the "typing" indicator, then post the
  final message. Streaming still helps here, because you can start tool calls as
  soon as `JustFinishedToolCall` fires.
- Or edit the message at most every ~1 s with the accumulated text.
