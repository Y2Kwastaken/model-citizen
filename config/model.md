# model.json

The brain: who the bot is, how it samples, how long its replies may be, and how
it rotates between models.

```json
{
    "default_personality": "default",
    "personalities": {
        "default": "personalities/default-personality.md",
        "smug": "personalities/smug-personality.md",
        "degenerate": "personalities/degenerate-personality.md"
    },
    "text-models": "models/text-models.json",
    "tts-models": "models/tts-models.json",
    "stt-models": "models/stt-models.json",
    "history_size": 20,
    "temperature": 0.85,
    "top_p": 0.9,
    "wake_word_threshold": 0.12,
    "wake_word_directory": "wakeword",
    "wake_word_file": "hey_model.onnx",
    "wake_word_runtime": "/usr/local/lib/libonnxruntime.so",
    "max_reply_runes": 650,
    "max_reply_tokens": 250,
    "tool_round_bonus": 350,
    "max_redraws": 2,
    "max_tool_rounds": 2,
    "short_term_memory_size": 10,
    "reply_timeout_seconds": 30,
    "per_model_timeout_seconds": 10,
    "rotation": {
        "reward_seconds": 2,
        "punish_seconds": 5,
        "kill_seconds": 10,
        "parole_seconds": 300
    }
}
```

## Personalities

| Key | Meaning |
| --- | --- |
| `default_personality` | The personality a server starts with |
| `personalities` | Name to a markdown prompt file, relative to `model.json` |

To add a personality, write a prompt file in `personalities/` and add an entry.
Keep names **lowercase**: the `switch` tool lowercases whatever name the model
asks for before looking it up.

Each server picks its own personality with the `switch` tool; the choice is
kept in memory and resets to `default_personality` on restart. A prompt file
that can't be read, or a `default_personality` with no entry, logs an error and
falls back to the prompt `NOP`.

## Rosters

| Key | Meaning |
| --- | --- |
| `text-models` | Chat roster, required |
| `stt-models` | Transcription roster; if it can't be loaded, voice commands are off |
| `tts-models` | Speech roster; if it can't be loaded, the bot doesn't speak |

The roster format is in [models/README.md](models/README.md).

## Sampling and history

| Key | Default | Meaning |
| --- | --- | --- |
| `history_size` | 20 | Messages kept per channel and sent with each reply |
| `temperature` | 0.85 | 0 to 2 |
| `top_p` | 0.9 | Above 0, at most 1 |

## Replies

| Key | Default | Meaning |
| --- | --- | --- |
| `max_reply_runes` | 650 | Longest reply in characters. Longer ones are cut at the last sentence end past the halfway mark |
| `max_reply_tokens` | 250 | Token budget for a reply. Enough to reach `max_reply_runes`; more is generated then thrown away |
| `tool_round_bonus` | 350 | Extra tokens while tools are on offer, since deciding on a call takes thinking |
| `max_redraws` | 2 | Times a reply is asked for again when it reads like an assistant, quotes its instructions, or is empty. 0 keeps the first draft |
| `max_tool_rounds` | 2 | Rounds of tool calls in one reply before tools are taken away and the model must answer |
| `reply_timeout_seconds` | 30 | How long a text reply may take, and the chat step of a voice reply |

## Memory

| Key | Default | Meaning |
| --- | --- | --- |
| `short_term_memory_size` | 10 | Memories kept; the oldest drops out when a new one arrives |

Memories go into the prompt as system messages after the personality. They are
shared across every server.

## Rotation

Every model call is timed, and the rotation moves off models that are slow or
down. These apply to all three rosters.

| Key | Default | Meaning |
| --- | --- | --- |
| `per_model_timeout_seconds` | 10 | Deadline for one attempt on one model before failing over to the next |
| `rotation.reward_seconds` | 2 | A call this fast takes 1 off the model's score (never below 0) |
| `rotation.punish_seconds` | 5 | A call this slow adds 2 to the score |
| `rotation.kill_seconds` | 10 | A call this slow benches the model at once |
| `rotation.parole_seconds` | 300 | How long a benched model sits out before it's tried again |

A model is benched when its score reaches 10, when it passes `kill_seconds`, or
when a call errors. If every model is benched, the one benched longest is tried.
The thresholds must satisfy `0 < reward <= punish <= kill`.

## Wake word

| Key | Default | Meaning |
| --- | --- | --- |
| `wake_word_threshold` | 0.12 | Score (0 to 1) that counts as "hey model". Lower hears more and false-triggers more; anything under 0.1 is raised to 0.1 |
| `wake_word_directory`, `wake_word_file` | `wakeword`, `hey_model.onnx` | Where the wake word model lives |
| `wake_word_runtime` | `/usr/local/lib/libonnxruntime.so` | Path to ONNX Runtime; the default is where the image installs it |

The bot won't start without a wake word model. The rest of voice is tuned in
[voice.md](voice.md).
