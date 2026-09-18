#!/usr/bin/env bash
# Trains hey_model.onnx. Each step resumes if interrupted: generation continues
# until the sample counts in the config are met, and the other two are
# idempotent over their inputs.
set -euo pipefail
cd "$(dirname "$0")"
source .venv/bin/activate

TRAIN=data/openwakeword/openwakeword/train.py
CONFIG=hey_model.yaml

python "$TRAIN" --training_config "$CONFIG" --generate_clips
# the front of the phrase is often lost to transmit gating; see clip_onsets.py
python clip_onsets.py
python "$TRAIN" --training_config "$CONFIG" --augment_clips
python "$TRAIN" --training_config "$CONFIG" --train_model

cp data/hey_model/hey_model.onnx hey_model.onnx
echo "wrote wakeword/hey_model.onnx"
