#!/usr/bin/env bash
# Downloads the training corpora. ~27 GB, resumable; the features file alone
# is 17 GB and wants an SSD, since training memory-maps it.
#
#   AUDIOSET_SHARDS  balanced-train shards to pull (~0.7 GB each), default 2
#   FMA_HOURS        hours of music to convert from fma_small, default 2
set -euo pipefail
cd "$(dirname "$0")"
source .venv/bin/activate

FEATURES=https://huggingface.co/datasets/davidscripka/openwakeword_features/resolve/main
AUDIOSET=https://huggingface.co/datasets/agkphysics/AudioSet/resolve/main/data/bal_train
FMA=https://os.unil.cloud.switch.ch/fma/fma_small.zip

# Pre-computed negatives: 2000h of speech, noise and music for training, and
# 11h held out for the false-positive rate that drives early stopping.
curl -fL -C - -o data/openwakeword_features_ACAV100M_2000_hrs_16bit.npy "$FEATURES/openwakeword_features_ACAV100M_2000_hrs_16bit.npy"
curl -fL -C - -o data/validation_set_features.npy "$FEATURES/validation_set_features.npy"

mkdir -p data/audioset
for i in $(seq -f "%02g" 0 $(( ${AUDIOSET_SHARDS:-2} - 1 ))); do
    curl -fL -C - -o "data/audioset/$i.parquet" "$AUDIOSET/$i.parquet"
done

# Free Music Archive, 8000 30s clips (7.2 GB). Read in place; never extracted.
curl -fL -C - -o data/fma_small.zip "$FMA"

python prepare_data.py --fma-hours "${FMA_HOURS:-2}"
