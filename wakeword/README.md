# Wake word

Trains the local "hey model" detector with [openWakeWord](https://github.com/dscripka/openWakeWord).
The detector runs on the bot's own machine so nothing leaves the call until
someone has actually said its name.

Everything the training needs is here; everything it produces except the final
model is ignored by git.

| file | what |
| --- | --- |
| `hey_model.yaml` | the training config: phrase, negatives, sample counts, paths |
| `setup.sh` | Python 3.10 venv via `uv`, the two upstream repos, the frozen front-end models |
| `fetch_data.sh` | the corpora (~20 GB, resumable) |
| `prepare_data.py` | converts them to the 16 kHz wavs training mixes in |
| `train.sh` | generate, clip onsets, augment, train; copies out `hey_model.onnx` |
| `clip_onsets.py` | adds copies of positives with the first 40–160 ms cut, as voice-activity gating delivers them |
| `score.py` | scores any recordings against the model the way the bot does |
| `hey_model.onnx` | the result, loaded by the bot |
| `melspectrogram.onnx`, `embedding_model.onnx` | upstream's frozen front end, which every head sits on |

## Running it

Needs `uv`, `git`, an NVIDIA GPU, and ~25 GB of SSD.

```
cd wakeword
./setup.sh
./fetch_data.sh        # AUDIOSET_SHARDS=2 FMA_HOURS=2 by default
./train.sh
```

Generation is the GPU-bound step; 30k clips is on the order of half an hour
on a 3060. Training runs to `steps` or until the false-positive target on the
held-out set is met.

## Tuning

- **Misses the phrase**: raise `n_samples` (100k+ is what upstream calls best)
  or `augmentation_rounds`.
- **Triggers on something specific**: add it to `custom_negative_phrases` and
  retrain. That list is the one deliberate departure from the upstream
  defaults.
- **Triggers on noise generally**: lower `target_false_positives_per_hour`, or
  raise `max_negative_weight`.

The score threshold and debounce are runtime settings on the Go side
(`discord-bot/listen.go`), not training ones; tune those first, they're free.

## Running the Go tests

`discord-bot/audio` checks its detector against scores recorded from the
Python implementation (`testdata/`). That needs `libonnxruntime.so` from the
release the Go binding targets (1.29.x), found via `ONNXRUNTIME_LIB` or an
unpacked release under `wakeword/data/`:

```
cd wakeword/data
curl -fsSL https://github.com/microsoft/onnxruntime/releases/download/v1.29.1/onnxruntime-linux-x64-1.29.1.tgz | tar -xz
```

Without it those tests skip.

## Why these pins

- `piper-sample-generator` is checked out at `v2.0.0`: `train.py` imports
  `generate_samples` from the repo root, and later versions moved it into a
  package.
- Python 3.10: `piper-phonemize` and `deep-phonemizer` ship no wheels past
  3.11, and the pinned `speechbrain` predates 3.12.
- No TensorFlow: openWakeWord's `[full]` extra pins `tensorflow-cpu==2.8.1`
  purely for `--convert_to_tflite`. The bot loads ONNX, so it's skipped.
