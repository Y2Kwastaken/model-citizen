# 05 – Tool calling

The model can't run anything itself. It can only **ask** you to run a function by
replying with a tool call (a name plus JSON arguments). You run it, send the result
back as a `tool` message, and call the model again so it can use the result.

- Guide: <https://developers.openai.com/api/docs/guides/function-calling>
- JSON Schema primer (for `parameters`): <https://json-schema.org/understanding-json-schema>

## Defining a tool

```go
import "github.com/openai/openai-go/v3/shared"

var getWeather = openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
	Name:        "get_weather",
	Description: openai.String("Get the current weather for a city. Use when the user asks about weather."),
	Parameters: shared.FunctionParameters{
		"type": "object",
		"properties": map[string]any{
			"city":  map[string]any{"type": "string", "description": "City name, e.g. Paris"},
			"units": map[string]any{"type": "string", "enum": []string{"c", "f"}},
		},
		"required":             []string{"city", "units"},
		"additionalProperties": false,
	},
	Strict: openai.Bool(true),
})

params.Tools = []openai.ChatCompletionToolUnionParam{getWeather}
```

- `Name`: `a-z A-Z 0-9 _ -`, at most 64 characters.
- `Description` is what the model reads to decide **when** to call the tool.
  Write it like a prompt.
- `Parameters` is a JSON Schema object. `shared.FunctionParameters` is a
  `map[string]any`, so you can also generate it from a struct (e.g.
  `github.com/invopop/jsonschema`, as the SDK README shows).
- `Strict: true` makes OpenAI guarantee that the arguments match the schema.
  Requirements: `additionalProperties: false` on every object, and **every**
  property listed in `required` (make a field optional with
  `"type": ["string", "null"]`). Local servers may ignore `strict`.

## `tool_choice`

| Value | Go | Effect |
| --- | --- | --- |
| `auto` (default when tools are set) | `{OfAuto: openai.String("auto")}` | The model decides: text, tools, or both |
| `none` | `{OfAuto: openai.String("none")}` | Tools are visible but won't be called |
| `required` | `{OfAuto: openai.String("required")}` | Must call at least one tool |
| specific function | `openai.ToolChoiceOptionFunctionToolChoice(openai.ChatCompletionNamedToolChoiceFunctionParam{Name: "x"})` | Must call exactly that one |

`ParallelToolCalls: openai.Bool(false)` limits the model to at most one tool call
per turn. That's easier to reason about, at the cost of extra round trips.

## The tool loop

```go
func runWithTools(ctx context.Context, client *openai.Client, params openai.ChatCompletionNewParams) (string, error) {
	for range 5 { // hard cap: a confused model can loop forever
		resp, err := client.Chat.Completions.New(ctx, params)
		if err != nil {
			return "", err
		}
		msg := resp.Choices[0].Message

		if len(msg.ToolCalls) == 0 {
			return msg.Content, nil // done
		}

		// 1. the assistant message with its tool_calls goes into history FIRST
		params.Messages = append(params.Messages, msg.ToParam())

		// 2. then one tool message per call, matched by ID
		for _, tc := range msg.ToolCalls {
			result, err := dispatch(ctx, tc.Function.Name, tc.Function.Arguments)
			if err != nil {
				result = "error: " + err.Error() // tell the model; don't abort
			}
			params.Messages = append(params.Messages, openai.ToolMessage(result, tc.ID))
		}
	}
	return "", errors.New("too many tool rounds")
}
```

The rules the server enforces (breaking them gives a 400):

1. A `tool` message must follow an `assistant` message containing a tool call
   with that `tool_call_id`.
2. **Every** tool call in that assistant message needs a `tool` reply before
   the next `user`/`assistant` message.
3. Keep them in the history together. If you trim one, trim both.

### Dispatching

`tc.Function.Arguments` is a **string** of JSON. Unmarshal it into a struct per
tool:

```go
type weatherArgs struct {
	City  string `json:"city"`
	Units string `json:"units"`
}

func dispatch(ctx context.Context, name, rawArgs string) (string, error) {
	switch name {
	case "get_weather":
		var a weatherArgs
		if err := json.Unmarshal([]byte(rawArgs), &a); err != nil {
			return "", fmt.Errorf("bad arguments: %w", err) // small models do produce broken JSON
		}
		return weather(ctx, a.City, a.Units)
	default:
		return "", fmt.Errorf("unknown tool %q", name)
	}
}
```

A `map[string]Tool` registry (name → schema + handler) scales better than a
switch once there are more than a handful of tools.

Tool results are plain strings. JSON or short prose both work. Keep them small,
because every byte goes back into the context.

## Tips from OpenAI's guide

- Keep the number of tools low (they suggest **under ~20** per request). Small
  models get worse much faster than that, so 3–4B models want ~5–10.
- Use `enum`s and clear names to prevent invalid calls.
- Don't make the model fill in arguments you already know (guild ID, user ID).
  Add those in your dispatcher.
- Merge tools that are always called in sequence into one tool.
- Turn on `strict` wherever the backend supports it.

## The two-lane design (model-citizen)

The planned layout is that input goes to **both** lanes in parallel:

```
              ┌─> tool model (small, local, temp ~0, tools attached)  ─> tool results
input ────────┤
              └─> chat model (persona, streaming, tools only DESCRIBED in system prompt)
```

API-level notes for building that:

**Tool lane.** Give it the real `Tools` and the last few turns of context, not just
the newest message, so it catches "yeah, do that". Use a low `Temperature` and a
short `MaxCompletionTokens`. Leave `ToolChoice` at `auto`, so "no tool needed" is a
plain text reply you throw away. If you want it to *always* answer in a fixed
shape (e.g. `{"tool": null}` vs a call), use a structured output instead. See
[06](06-structured-outputs.md).

**Chat lane.** Send no `Tools` param, so it can't produce tool calls. Its system
prompt lists what the bot can do, in prose. Stream it.

**Joining the lanes.** When a tool result arrives, it has to reach the chat
model somehow. Two common ways:

- Send it as a new turn: append a `system` (or `user`) message like
  `"[tool get_weather result]: 18°C, sunny"` and run the chat model again, so it
  phrases the answer in persona.
- Or, for action tools (play song, set reminder), post a short confirmation
  straight from code and only log it into history.

Use `system`/`user` messages, not `tool`, to inject results into the chat
lane. A `tool` message needs a matching assistant `tool_calls` entry, and the
chat lane never produced one, so the server would reject it.

## Tool calls while streaming

See [04 – Streaming](04-streaming.md#chatcompletionaccumulator).
`acc.JustFinishedToolCall()` returns each call as soon as its arguments are
complete, so you can start running it before the stream ends.
