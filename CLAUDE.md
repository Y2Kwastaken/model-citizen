# model-citizen (v2)

Discord AI chatbot with voice, written in Go. Modules: `discord-bot/` (disgo v0.19.6 + golibdave for DAVE), `shared/`.
Internal notes on disgo voice internals live in `_docs/disgo-voice/`.

## Learning-first project — read this before writing audio code

v2 is a hand-written rewrite; the owner is using it to **learn** audio encoding, decoding and mixing (see README).

- **Don't generate audio/mixing code unasked.** Tutor instead: explain concepts, review the owner's code, point at
  references, and suggest small exercises. Write audio code only when explicitly asked, and explain it when you do.
- Non-audio plumbing (commands, config, Docker) is fine to help with normally.

## Audio mixing decisions (2026-09-27)

- **Not using go-mix** (github.com/go-mix/mix): it keeps global package-level state (one mixer per process, which breaks
  with one voice conn per guild), schedules file-path sounds at fixed times rather than taking live streams, and pushes
  output instead of letting you pull it. disgo's `OpusFrameProvider` pulls one 20 ms frame per tick.
- **Plan:** a hand-written, per-connection, pull-based mixer modeled on gopxl/beep's `Streamer` interface.
  Each tick: pull 960 samples/channel (48 kHz stereo, 20 ms) from each source, pad short sources with silence, sum in
  float32, clamp or soft-limit, convert to int16, then Opus-encode.
- Libraries are OK for resampling (e.g. 24 kHz TTS → 48 kHz) and Opus encoding (libopus bindings). Don't hand-roll those.
- Scope is programmatic mixing (sum, gain, dB, ramps/fades, ducking, clipping), not music-production mixing (EQ, compression).

## Lesson plan (about 1 week)

When tutoring, find out where the owner is in this plan and build on it.

**Status (2026-10-01):** reading and the audio-processing exercises (1–6) are done. Now on exercise 7: the pull-based
mixer in `shared/audio/mixing.go`, which needs the per-layer buffering in the channels plan below.

### Concepts to master

1. **PCM basics:** samples, sample rate, bit depth, channels, interleaving. Discord: 48 kHz stereo, 20 ms frame =
   960 samples/channel = 1920 `int16` values.
2. **Mixing is addition:** `out[i] = a[i] + b[i]`.
3. **Clipping:** summing `int16` values overflows. Convert to float32, sum, clamp or soft-limit, convert back.
4. **Gain:** multiply each sample. dB to linear: `gain = 10^(dB/20)` (−6 dB ≈ 0.5).
5. **Ramps:** change gain over a few ms, never instantly, or it clicks. Fades, crossfades and ducking are all ramps.
6. **Resampling:** e.g. 24 kHz TTS → 48 kHz. Use a library.
7. **Pull-based mixer loop:** each 20 ms tick, pull 960 frames from each active source, pad with silence, sum,
   clamp, Opus-encode. Never block or allocate carelessly inside the tick.

### Schedule

| Day | Resource | Why |
| --- | --- | --- |
| 1 | Monty Montgomery (xiph.org): "[Digital Show & Tell](https://xiph.org/video/vid2.shtml)" and "[A Digital Media Primer for Geeks](https://xiph.org/video/vid1.shtml)" (free videos) | Sampling, bit depth, and why digital audio works |
| 2 | dspguide.com (Steven W. Smith), chapters 1–3 | Signals and sampling fundamentals; skip heavy math for now |
| 3 | Ross Bencina, "Real-time audio programming 101: time waits for nothing" | Real-time constraints; maps to the 20 ms `ProvideOpusFrame` |
| 4–5 | gopxl/beep source: `mixer.go`, `effects/volume.go`, `resample.go`, `ctrl.go` | The `Streamer` pull model and a ~50-line mixer in Go |
| 6–7 | Build the mixer against `discord-bot/assets/sample-track*.wav` | Hands-on; see exercises below |

### Exercises (in order)

1. Decode one WAV to raw PCM and play it back unchanged:
   `ffplay -f s16le -ar 48000 -ch_layout stereo out.raw`
2. Apply −6 dB gain; confirm in Audacity that the amplitude halved.
3. Mix both WAVs with a naive `int16` sum; hear or see the clipping. Fix it with a float32 sum and clamp.
4. Add a 200 ms fade-in and fade-out with no clicks.
5. Crossfade from track 1 to track 2 over 200 ms.
6. Duck track 1 by −12 dB while track 2 plays, with ramps on both edges.
7. Wrap the mixer as a `Streamer`-style pull source producing exactly 960 samples/channel per call, padding with silence.
8. Wire it to disgo's `OpusFrameProvider` (Opus encode via libopus bindings) and play it in a voice channel.

Tools: `ffplay` to hear raw PCM, Audacity to see waveforms and clipping, `sox` to convert formats.

Further reference (for specific questions later): Julius O. Smith's CCRMA online books, musicdsp.org.

## Channels plan (about 3–4 days, alongside the mixer)

Same tutoring rules as the audio plan: the owner writes the code, Claude reviews and explains.

**Status (2026-10-01):** exercises 1–3 and 5 done (plus standalone concept steps 1–5 in `laudio/main2.go`). The
per-layer reader is built inline in `MixingLayer.readLayerToBuffer` and was verified (padded partial frame, stall-safe
tick, no leak on `Unregister`). The owner chose to skip writing the stall test and move on to playing static files
(ffmpeg wiring, exercise 6, and audio exercise 8). Exercise 4 (free list) is deferred.

### Why the mixer needs this

Sources like an ffmpeg pipe (`ffmpeg -i <file> -f s16le -ar 48000 -ac 2 pipe:1`) can return short reads or stall.
`io.ReadFull` fixes short reads but blocks, and nothing may block inside the 20 ms tick. So each layer gets its own
jitter buffer: a reader goroutine runs `io.ReadFull` and sends full 3840-byte frames into a buffered channel (10–25
frames), and `NextFrames` does a non-blocking `select`/`default`, padding with silence on underrun. A closed channel
means the layer is finished. In-memory sources don't need the buffer, so the mixer depends on a "give me one frame now,
never block" interface, with `memorySource` and `bufferedSource` behind it. `bufio` doesn't solve this (it's a passive
buffer that still blocks when empty). Apply gain/editors in the tick, after the buffer, so changes take effect at once.

### Schedule

| Day | Resource | Focus |
| --- | --- | --- |
| 1 | [A Tour of Go: Concurrency](https://go.dev/tour/concurrency/1) + [Go by Example](https://gobyexample.com/goroutines) (Goroutines through Closing Channels, Range over Channels, Timers, Tickers) | Syntax: goroutines, buffered vs unbuffered, `close`, `range`, `select`, `default` |
| 2 | Dave Cheney, "[Channel Axioms](https://dave.cheney.net/2014/03/19/channel-axioms)"; Go blog, "[Share Memory by Communicating](https://go.dev/blog/codelab-share)" | Nil/closed channel behaviour; channel vs mutex |
| 2 | Go blog, "[Pipelines and cancellation](https://go.dev/blog/pipelines)" | Producer/consumer stages, who closes, stopping goroutines with `done`/`context` |
| 3 | Rob Pike, "Concurrency is not Parallelism"; Sameer Ajmani, "Advanced Go Concurrency Patterns" (YouTube) | `for`-`select` loops, free lists, timeouts |
| 3–4 | Build `bufferedSource` | Apply it to the mixer |

### Exercises (in order)

1. Drill: buffered queue, producer goroutine, `close`/`ok`, `select`/`default` underruns with a jittery producer.
2. `bufferedInts(r func() (int, error), size int) <-chan int`: the producer closes on error; the consumer runs a 20 ms
   non-blocking tick and stops on `ok == false`.
3. Cancellation: add a `context.Context` so the consumer can stop the producer early (a song skip); verify the
   goroutine exits.
4. Free list: a `full` and a `free` channel with N frames preallocated; `testing.AllocsPerRun` shows 0 allocations in
   steady state.
5. `bufferedSource` wrapping an `io.Reader` with `io.ReadFull`; stall-test with a reader that sleeps and confirm the
   tick never blocks. Treat `io.ErrUnexpectedEOF` as a final partial frame.
6. Wire it up: ffmpeg pipe → `bufferedSource` → mixer layer. Call `cmd.Wait()` when the layer ends, and cancel the
   context to kill ffmpeg on skip.

Always run with `-race`.
