# Model rosters

The bot cycles through the chat models in `text-models.json`, the transcription
models in `stt-models.json` and the voices in `tts-models.json`. Which files are
used is set in [`model.json`](../model.md#rosters), and how the rotation judges
and benches models is in [Rotation](../model.md#rotation).

All three share one format, a list tried in order:

```json
[
  {
    "name": "nvidia/nemotron-3-super-120b-a12b",
    "base_url": "https://integrate.api.nvidia.com/v1",
    "auth_key": "MODEL_AUTH_KEY",
    "chat_template_kwargs": {"enable_thinking": false}
  }
]
```

| Key | Required | Meaning |
| --- | --- | --- |
| `name` | yes | What the service calls the model (or, for Google speech, the voice) |
| `base_url` | yes | The service's API root |
| `auth_key` | yes | The **name** of the environment variable holding the key, never the key itself |
| `api` | no | The API shape the service speaks: `openai` (default), `mistral`, `google`, `deepgram` or `assemblyai` |
| `voice` | no | Speech only: which of the service's voices to use, for services where model and voice are separate |
| `chat_template_kwargs` | no | Chat only: sent with every request. Used to turn thinking off on NIM models, which ignore `reasoning_effort`. Leave it off for services that reject unknown fields, like Groq |

## Keys

The rosters are committed and `.env` is not, which is why `auth_key` names a
variable. Each entry resolves its own variable, so pointing a model at a
different service is a new entry plus a new line in `.env`.

A model whose variable is unset is logged and dropped rather than stopping the
bot, so you can list a service before you have credentials for it. A service
with no key at all, one running alongside the bot, says so with
`"auth_key": "NOP"`, so a key that is simply missing still looks like a mistake.

## APIs

Each roster is loaded for one feature, and every entry is checked against it, so
a voice listed among the chat models is an error at startup rather than a request
that fails later.

| `api` | Chat | Transcription | Speech |
| --- | --- | --- | --- |
| `openai` | yes | yes | yes |
| `mistral` | | | yes |
| `google` | | | yes |
| `deepgram` | | yes | |
| `assemblyai` | | yes | |

Deepgram answers in one round trip. AssemblyAI queues the clip and is polled
until it's done or `per_model_timeout_seconds` runs out, so it belongs last in
the rotation. Teaching the bot another service is an entry in `apis` in
`llm/model/apis.go` plus the function it names.

## Speech

`tts-models.json` is the bot's voice:

```json
[
  {
    "name": "en-GB-Chirp3-HD-Charon",
    "base_url": "https://texttospeech.googleapis.com/v1",
    "auth_key": "GOOGLE_KEY",
    "api": "google"
  }
]
```

Google's voices have no model apart from the voice, so `name` is the voice and
there is no `voice` field. `GET /v1/voices?languageCode=en-GB` with the key in
`x-goog-api-key` lists them; the Chirp 3 HD set (Charon, Fenrir, Puck, Kore,
Aoede, ...) exists under every `en-*` locale.

For Mistral's Voxtral (`"api": "mistral"`), the model goes in `name` and the
voice in `voice`, e.g. `gb_oliver_neutral`. Pin the model rather than an alias
like `voxtral-mini-tts-latest`, because an alias moves and the voice is the
bot's whole character to whoever is listening.

A rotation fails over, so treat more than one voice as an outage plan rather
than a choice: a swapped voice mid-conversation is worse than a pause.
