# 06 – Structured outputs (JSON replies)

Sometimes you don't want prose, you want data: a routing decision, a
classification, extracted fields. `response_format` makes the model reply with
JSON.

- Guide: <https://developers.openai.com/api/docs/guides/structured-outputs>

## Three levels

| `ResponseFormat` | Guarantee |
| --- | --- |
| `OfText` (default) | None |
| `OfJSONObject` ("JSON mode") | Valid JSON, but any shape. You must also say "reply in JSON" in the prompt. |
| `OfJSONSchema` with `Strict: true` | Valid JSON **matching your schema** |

```go
params.ResponseFormat = openai.ChatCompletionNewParamsResponseFormatUnion{
	OfJSONSchema: &shared.ResponseFormatJSONSchemaParam{
		JSONSchema: shared.ResponseFormatJSONSchemaJSONSchemaParam{
			Name:   "route",
			Strict: openai.Bool(true),
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"needs_tool": map[string]any{"type": "boolean"},
					"tool":       map[string]any{"type": []string{"string", "null"}, "enum": []any{"get_weather", "play_song", nil}},
					"reason":     map[string]any{"type": "string"},
				},
				"required":             []string{"needs_tool", "tool", "reason"},
				"additionalProperties": false,
			},
		},
	},
}

resp, _ := client.Chat.Completions.New(ctx, params)
var r struct {
	NeedsTool bool    `json:"needs_tool"`
	Tool      *string `json:"tool"`
	Reason    string  `json:"reason"`
}
json.Unmarshal([]byte(resp.Choices[0].Message.Content), &r)
```

Same strict-schema rules as tools: `additionalProperties: false`, every property
in `required`, and use a `null` type for optional fields.

## Tools vs structured output: which one?

| Use tools when... | Use `response_format` when... |
| --- | --- |
| The model is choosing **an action to take** | You want **the reply itself** in a fixed shape |
| It may call 0, 1, or several | It always answers exactly once |
| Results go back to the model | You consume the JSON in code and you're done |

For the tool lane, both work:

- **Tools + `auto`**: natural, and gives you arguments directly.
- **A routing schema** (like above): always the same shape, easy to log and
  test, and a field order like `reason` before `tool` can improve small models'
  decisions. Then make a second call with the chosen tool forced, or fill in
  the arguments in the same schema.

## On local servers

- **llama.cpp**: supports `response_format` with `json_schema` (turned into a
  grammar, so it's enforced at sampling time).
- **Ollama**: supports JSON schemas via its OpenAI endpoint (`response_format`).
  Check the compatibility page for the current status.
- **vLLM**: guided decoding, so it's enforced.

Grammar-enforced JSON is very reliable even on 3–4B models. Still handle
`json.Unmarshal` errors, because `finish_reason: length` cuts JSON in half.
