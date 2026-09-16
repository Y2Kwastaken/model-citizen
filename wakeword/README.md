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
| `train.sh` | generate, augment, train; copies out `hey_model.onnx` |
| `hey_model.onnx` | the result, loaded by the bot |

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

The score threshold and debounce are runtime settings on the Go side, not
training ones; tune those first, they're free.

## Why these pins

- `piper-sample-generator` is checked out at `v2.0.0`: `train.py` imports
  `generate_samples` from the repo root, and later versions moved it into a
  package.
- Python 3.10: `piper-phonemize` and `deep-phonemizer` ship no wheels past
  3.11, and the pinned `speechbrain` predates 3.12.
- No TensorFlow: openWakeWord's `[full]` extra pins `tensorflow-cpu==2.8.1`
  purely for `--convert_to_tflite`. The bot loads ONNX, so it's skipped.
