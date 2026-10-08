# 05 – Sending audio

## Lowest level: `UDPConn.Write`

```go
conn.SetSpeaking(ctx, voice.SpeakingFlagMicrophone) // op 5, tells Discord you're transmitting
conn.UDP().Write(opusFrame)                         // one 20 ms Opus frame per call
```

Each `Write`:

1. Fills the RTP header: sequence++ and **timestamp += 960** (fixed; disgo
   assumes every frame is 20 ms).
2. DAVE-encrypts the frame with our SSRC.
3. Transport-encrypts and appends the 4-byte nonce.
4. Writes one UDP datagram.

`Write` does **not** pace itself. Calling it faster than once per 20 ms sends
audio faster than real time. Pacing is the caller's job, or the
`AudioSender`'s.

## `OpusFrameProvider` + the default `AudioSender`

```go
type OpusFrameProvider interface {
    ProvideOpusFrame() ([]byte, error) // return one 20 ms frame; nil/empty = nothing to send right now
    Close()
}

conn.SetOpusFrameProvider(provider)
```

`SetOpusFrameProvider` closes any old sender and starts a new
`defaultAudioSender` goroutine:

- Every **20 ms** (with drift correction; it resyncs if it falls more than 3
  frames behind) it calls `ProvideOpusFrame()`.
- **Non-empty frame:**
  - On the first frame after silence, it sends `SetSpeaking(Microphone)`
    (5 s timeout) and resets the silence counter to 5.
  - It writes the frame to UDP.
- **Empty frame** (or `io.EOF` with no data):
  - It first sends up to **5 silence frames** (`0xF8 0xFF 0xFE`), which Discord
    expects so decoders don't interpolate garbage.
  - It then sends `SetSpeaking(None)` once.
  - The loop **keeps running and polling** your provider every 20 ms. It does
    not stop on EOF. Close it with a new provider or `conn.Close`.
- Other errors from `ProvideOpusFrame` are logged and that tick is skipped.
- `net.ErrClosed` or `voice.ErrGatewayNotConnected` from writes/speaking stops the sender.

## Built-in provider: `OpusReader`

`voice.NewOpusReader(r)` reads `[uint32 LE length][opus bytes]` records, the
same format `OpusWriter` writes. It uses plain `r.Read`, not `io.ReadFull`, so
wrap network or pipe readers in `bufio.Reader`, or write your own provider (see
[06](06-gotchas.md#opusreader-uses-read-not-readfull)).

## Speaking flags

| Flag | Value | Use |
| --- | --- | --- |
| `SpeakingFlagNone` | 0 | stopped |
| `SpeakingFlagMicrophone` | 1 | normal voice |
| `SpeakingFlagSoundshare` | 2 | stream/soundshare audio |
| `SpeakingFlagPriority` | 4 | priority speaker |

You must send a speaking flag before audio, or other clients may not play it.
The default sender handles this for you. With raw `Write` you do it yourself.

## Encoding

As with receiving, disgo doesn't encode. Produce Opus at **48 kHz, stereo,
20 ms frames** (960 samples per channel), e.g. with `hraban/opus` in
`AppVoIP` or `AppAudio` mode, or pre-encode with ffmpeg
(`-c:a libopus -ar 48000 -ac 2 -frame_duration 20`) and demux the Ogg pages
into frames.
