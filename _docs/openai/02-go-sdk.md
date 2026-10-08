# 02 – The Go SDK (openai-go v3)

How `github.com/openai/openai-go/v3` is shaped. Written against **v3.57.0**, the
version pinned in `llm/go.mod`. Source: `$(go env GOMODCACHE)/github.com/openai/openai-go/v3@v3.57.0/`.

- Repo + README: <https://github.com/openai/openai-go>
- Every method and type: <https://github.com/openai/openai-go/blob/main/api.md>
- godoc: <https://pkg.go.dev/github.com/openai/openai-go/v3>

## Creating a client

```go
import (
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

client := openai.NewClient(
	option.WithAPIKey(key),                         // default: $OPENAI_API_KEY
	option.WithBaseURL("http://localhost:11434/v1/"), // default: $OPENAI_BASE_URL, else api.openai.com
	option.WithMaxRetries(2),                       // default 2; retries 408/409/429/5xx + connection errors
	option.WithRequestTimeout(30*time.Second),      // per attempt, not total
)
```

`NewClient` returns a value, not a pointer. Your `executeChat` takes
`*openai.Client`, so pass `&client`. The client is safe for concurrent use; make
one per backend (one for the chat model, one for the tool model) and share it.

Any option can also be passed **per request**, overriding the client's:

```go
client.Chat.Completions.New(ctx, params, option.WithRequestTimeout(5*time.Second))
```

Useful options (all in `option/requestoption.go` and `option/middleware.go`):

| Option | Use |
| --- | --- |
| `WithDebugLog(nil)` | Logs every request and response. Use it when a local server does something odd. |
| `WithHeader(k, v)` | Extra headers |
| `WithJSONSet("path", v)` | Add a body field the SDK doesn't know about (server-specific params, e.g. llama.cpp's `"cache_prompt"`) |
| `WithMiddleware(...)` | Wrap every HTTP call (timing, metrics) |
| `WithHTTPClient(c)` | Custom `*http.Client` |

## Request params: `param.Opt[T]` and `omitzero`

Request structs (`ChatCompletionNewParams` etc.) follow one rule: **a zero value
is not sent.** Required fields are plain types. Optional primitives are wrapped
in `param.Opt[T]` so "0" and "not set" are different:

```go
params := openai.ChatCompletionNewParams{
	Model:       "qwen3:4b",          // required; ChatModel is just a string type
	Messages:    msgs,                // required
	Temperature: openai.Float(0.7),   // optional -> openai.Float / Int / String / Bool
	MaxCompletionTokens: openai.Int(300),
}
```

If you leave `Temperature` out, the server's default is used. You couldn't do that
with a plain `float64`, because 0 would be sent.

Fields the SDK doesn't have: `params.SetExtraFields(map[string]any{"top_k": 20})`.

## Unions: `OfXxx` fields

Wherever the API accepts "one of several shapes", the Go type is a struct with
one `OfXxx` pointer or `param.Opt` per shape. **Set exactly one.**

```go
// tool_choice: "auto" | "none" | "required" | {a specific function}
params.ToolChoice = openai.ChatCompletionToolChoiceOptionUnionParam{
	OfAuto: openai.String("required"),
}
```

Most common unions have constructor helpers, so you rarely write `Of...`
by hand: `openai.UserMessage(...)`, `openai.ChatCompletionFunctionTool(...)`,
`openai.ToolChoiceOptionFunctionToolChoice(...)`.

On the **response** side, unions are flattened: one struct has every variant's
fields, plus `.AsAny()` / `.AsFoo()` to get a concrete variant.

## Response structs and `.JSON`

Response fields are plain values (no pointers). To tell "missing/null" from
"zero", each struct has a `JSON` field:

```go
if !resp.Usage.JSON.PromptTokensDetails.Valid() { /* server didn't send it */ }
raw := resp.JSON.ExtraFields["timings"].Raw() // fields the SDK doesn't model (llama.cpp sends "timings")
```

Converting a response message back into a request message, which you need for
history and for the tool loop:

```go
msg := resp.Choices[0].Message
history = append(history, msg.ToParam()) // keeps content AND tool_calls
```

## Errors

Non-2xx responses (after retries) come back as `*openai.Error`:

```go
resp, err := client.Chat.Completions.New(ctx, params)
var apiErr *openai.Error
if errors.As(err, &apiErr) {
	log.Printf("status=%d type=%s code=%s msg=%s", apiErr.StatusCode, apiErr.Type, apiErr.Code, apiErr.Message)
	// apiErr.DumpRequest(true) / DumpResponse(true) for debugging
}
```

Anything else (`context.Canceled`, DNS failures, ...) is a normal error.

| Status | Meaning | What to do |
| --- | --- | --- |
| 400 | Bad request: invalid schema, broken message order, context too long | Fix the request. Not retried. |
| 401 / 403 | Bad key / no access to that model | Config problem |
| 404 | Unknown model (or wrong base URL) | Check `Model` and the trailing `/v1/` |
| 429 | Rate limit **or** out of quota (`code: insufficient_quota`) | SDK retries. Quota errors won't recover. |
| 5xx | Server problem | SDK retries |

<https://developers.openai.com/api/docs/guides/error-codes> ·
<https://developers.openai.com/api/docs/guides/rate-limits>

## Context and cancellation

Every call takes a `context.Context`. Cancelling it aborts the HTTP request
straight away, including a stream in progress. That's how you stop generating
when a user interrupts or a newer message makes the reply pointless. Use one
context per turn and cancel it on barge-in.
