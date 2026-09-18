"""Adds onset-clipped copies of the generated positives.

Voice-activity gating (Discord's included) opens the stream a beat after
speech starts, so the bot often receives "ey model" or "a model" instead of
"hey model". The TTS clips all start clean; this trims 40-160 ms from the
front of a share of them so the model learns the phrase as it actually
arrives. Idempotent: it does nothing once the copies exist.
"""

import glob
import os
import random

import numpy as np
import scipy.io.wavfile

SHARE = 0.35          # of clips that get a clipped twin
TRIM_MS = (40, 160)   # how much of the onset to drop
SILENCE = 300         # amplitude below which the leading part is not speech yet


def clip_onsets(directory: str, seed: int) -> int:
    clips = [p for p in sorted(glob.glob(os.path.join(directory, "*.wav"))) if not p.endswith("_onset.wav")]
    if glob.glob(os.path.join(directory, "*_onset.wav")):
        return 0
    rng = random.Random(seed)
    made = 0
    for path in rng.sample(clips, int(len(clips) * SHARE)):
        sr, pcm = scipy.io.wavfile.read(path)
        speech = np.argmax(np.abs(pcm) > SILENCE)          # first sample that is speech
        cut = speech + int(sr * rng.uniform(*TRIM_MS) / 1000)
        if cut >= len(pcm) - sr // 4:
            continue
        scipy.io.wavfile.write(path[:-4] + "_onset.wav", sr, pcm[cut:])
        made += 1
    return made


if __name__ == "__main__":
    base = os.path.join(os.path.dirname(__file__), "data", "hey_model", "hey_model")
    for name, seed in (("positive_train", 1), ("positive_test", 2)):
        print(f"{name}: {clip_onsets(os.path.join(base, name), seed)} onset-clipped copies")
