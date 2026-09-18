"""Scores recordings against hey_model.onnx the way the bot does: streaming,
80ms at a time, reporting the peak. Takes any files ffmpeg can read.

    source .venv/bin/activate
    python score.py ../data/voice-dumps/*.wav
"""

import subprocess
import sys

import numpy as np
from openwakeword.model import Model


def load16k(path: str) -> np.ndarray:
    raw = subprocess.run(
        ["ffmpeg", "-v", "error", "-i", path, "-f", "s16le", "-ac", "1", "-ar", "16000", "pipe:1"],
        check=True, capture_output=True).stdout
    # a second of silence either side, so a phrase at the very start fits a window
    return np.concatenate([np.zeros(16000, np.int16), np.frombuffer(raw, np.int16), np.zeros(16000, np.int16)])


if __name__ == "__main__":
    model = Model(wakeword_models=["hey_model.onnx"], inference_framework="onnx")
    for path in sys.argv[1:]:
        model.reset()
        pcm = load16k(path)
        scores = [float(model.predict(pcm[i:i + 1280])["hey_model"]) for i in range(0, len(pcm) - 1279, 1280)]
        peak = int(np.argmax(scores))
        print(f"{path}: peak {max(scores):.3f} at {peak * 0.08:.2f}s of {len(pcm) / 16000 - 2:.2f}s")
