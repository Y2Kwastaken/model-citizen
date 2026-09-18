#!/usr/bin/env bash
# One-time environment setup for training the wake word. Needs uv and git.
#
# Python is pinned to 3.10: piper-phonemize and deep-phonemizer have no wheels
# past 3.11, and the pinned speechbrain predates 3.12.
set -euo pipefail
cd "$(dirname "$0")"

OWW_MODELS=https://github.com/dscripka/openWakeWord/releases/download/v0.5.1
PIPER_MODEL=https://github.com/rhasspy/piper-sample-generator/releases/download/v2.0.0/en_US-libritts_r-medium.pt

mkdir -p data
uv python install 3.10
uv venv --python 3.10 .venv
source .venv/bin/activate

# train.py imports generate_samples from the repo root, which only v2.0.0 has.
if [ ! -d data/piper-sample-generator ]; then
    git clone --branch v2.0.0 --depth 1 https://github.com/rhasspy/piper-sample-generator data/piper-sample-generator
fi
curl -fsSL -C - -o data/piper-sample-generator/models/en_US-libritts_r-medium.pt "$PIPER_MODEL"
# torch>=2.6 refuses pickled checkpoints by default; this one is rhasspy's own release
sed -i 's/torch.load(model_path)$/torch.load(model_path, weights_only=False)/' data/piper-sample-generator/generate_samples.py

if [ ! -d data/openwakeword ]; then
    git clone --depth 1 https://github.com/dscripka/openwakeword data/openwakeword
fi
# --convert_to_tflite defaults to the string "False", which is truthy, so the
# tflite conversion (and its TensorFlow dependency) always ran
sed -i 's/default="False"/default=False/' data/openwakeword/openwakeword/train.py
# adversarial negatives are built per word from CMUdict; punctuation in a
# target phrase ("hey... model") makes a word it cannot look up and the
# sampling step fails, so strip it there
sed -i 's/input_text=target_phrase,/input_text=re.sub(r"[^\\w\\s]", "", target_phrase),/' data/openwakeword/openwakeword/train.py
grep -q '^import re$' data/openwakeword/openwakeword/train.py || sed -i 's/^import argparse$/import argparse\nimport re/' data/openwakeword/openwakeword/train.py

# The training half of openwakeword's [full] extra, minus the TensorFlow stack
# that only serves --convert_to_tflite. PyPI torch wheels bundle CUDA.
# torch 2.8 is the last torchaudio with info/load/save built in; 2.9 moved
# them to torchcodec and torch-audiomentations has not followed.
uv pip install \
    "numpy<2" \
    "torch==2.8.*" "torchaudio==2.8.*" \
    "torchinfo>=1.8,<2" "torchmetrics>=0.11.4,<1" \
    "speechbrain>=0.5.14,<1" \
    "audiomentations==0.33.0" "torch-audiomentations>=0.11,<1" \
    "acoustics>=0.2.6,<1" \
    "pyyaml>=6,<7" "pronouncing>=0.2,<1" "deep-phonemizer==0.0.19" \
    "datasets[audio]>=2.14.4,<3" "mutagen>=1.46,<2" \
    "onnx" \
    "piper-phonemize==1.1.0" webrtcvad \
    scipy tqdm \
    "setuptools<81"  # pkg_resources, dropped in 81, for the pinned torchmetrics
uv pip install -e data/openwakeword
# deep-phonemizer's checkpoint is a full pickle too; openwakeword downloads it
# from its own release to generate adversarial negatives
sed -i 's/torch.load(checkpoint_path, map_location=device)$/torch.load(checkpoint_path, map_location=device, weights_only=False)/' \
    .venv/lib/python3.10/site-packages/dp/model/model.py

# The frozen front end every openwakeword model sits on. train.py looks for
# these inside the package.
mkdir -p data/openwakeword/openwakeword/resources/models
for model in melspectrogram.onnx embedding_model.onnx; do
    curl -fsSL -C - -o "data/openwakeword/openwakeword/resources/models/$model" "$OWW_MODELS/$model"
done

echo "ready: source wakeword/.venv/bin/activate, then ./fetch_data.sh"
