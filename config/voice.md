# voice.json

How the bot hears voice commands in a call. The wake word itself is set in
[model.md](model.md#wake-word).

```json
{
    "language": "en",
    "listen_window_seconds": 30,
    "context_window_seconds": 10,
    "command_quiet_ms": 350,
    "command_max_seconds": 10,
    "min_utterance_ms": 400,
    "wake_debounce_seconds": 3,
    "transcribe_timeout_seconds": 30,
    "speak_timeout_seconds": 60
}
```

A voice command goes: wake word heard, the bot listens until you stop talking,
the speech is transcribed, the bot replies, and the reply is spoken.

| Key | Default | Meaning |
| --- | --- | --- |
| `language` | `en` | Language code passed to the transcriber. Empty lets it guess |
| `listen_window_seconds` | 30 | Audio kept per speaker |
| `context_window_seconds` | 10 | Audio from before the wake word that goes along with the command. At most `listen_window_seconds` |
| `command_quiet_ms` | 350 | Pause that ends a command. Lower answers sooner but cuts people off mid-sentence |
| `command_max_seconds` | 10 | Longest command listened to |
| `min_utterance_ms` | 400 | Shorter sounds are treated as noise and not transcribed |
| `wake_debounce_seconds` | 3 | How soon the wake word can trigger again. 0 turns it off |
| `transcribe_timeout_seconds` | 30 | Deadline for transcribing a command |
| `speak_timeout_seconds` | 60 | Deadline for synthesising and playing one reply |

A whole voice command gets `command_max_seconds` + `transcribe_timeout_seconds`
+ `reply_timeout_seconds` (from `model.json`) before it's abandoned.
