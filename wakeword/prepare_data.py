"""Converts the downloaded corpora into the 16 kHz wav files train.py mixes in.

Room impulse responses come straight from Hugging Face; AudioSet and FMA are
converted from the files fetch_data.sh downloaded.
"""

import argparse
import io
import zipfile
from pathlib import Path

import datasets
import librosa
import numpy as np
import pyarrow.parquet as pq
import scipy.io.wavfile
from tqdm import tqdm

DATA = Path(__file__).parent / "data"
RATE = 16000


def save(row, out_dir: Path, ext: str) -> None:
    write(out_dir / Path(row["audio"]["path"]).name.replace(ext, ".wav"), row["audio"]["array"])


def write(path: Path, samples: np.ndarray) -> None:
    scipy.io.wavfile.write(path, RATE, (samples * 32767).astype(np.int16))


def done(out: Path) -> bool:
    if any(out.iterdir()):
        print(f"{out.name}: already prepared")
        return True
    return False


def rirs() -> None:
    out = DATA / "mit_rirs"
    out.mkdir(exist_ok=True)
    if done(out):
        return
    rows = datasets.load_dataset("davidscripka/MIT_environmental_impulse_responses", split="train", streaming=True)
    for row in tqdm(rows, desc="rirs"):
        save(row, out, ".wav")


def audioset() -> None:
    out = DATA / "audioset_16k"
    out.mkdir(exist_ok=True)
    if done(out):
        return
    # Read with pyarrow rather than datasets: the shards embed feature
    # metadata from a newer datasets release than the training pins allow.
    for shard in sorted((DATA / "audioset").glob("*.parquet")):
        table = pq.read_table(shard, columns=["audio"])
        for clip in tqdm(table.column("audio").to_pylist(), desc=shard.name):
            samples, _ = librosa.load(io.BytesIO(clip["bytes"]), sr=RATE, mono=True)
            write(out / Path(clip["path"]).with_suffix(".wav").name, samples)


def fma(hours: float) -> None:
    out = DATA / "fma"
    out.mkdir(exist_ok=True)
    if done(out):
        return
    # every fma clip is 30 seconds
    wanted = int(hours * 3600 // 30)
    with zipfile.ZipFile(DATA / "fma_small.zip") as archive:
        tracks = sorted(name for name in archive.namelist() if name.endswith(".mp3"))[:wanted]
        for name in tqdm(tracks, desc="fma"):
            samples, _ = librosa.load(io.BytesIO(archive.read(name)), sr=RATE, mono=True)
            write(out / Path(name).with_suffix(".wav").name, samples)


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--fma-hours", type=float, default=2)
    args = parser.parse_args()

    rirs()
    audioset()
    fma(args.fma_hours)
