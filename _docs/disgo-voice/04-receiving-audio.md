# 04 – Receiving audio (listening & processing)

There are two ways to consume incoming audio. **Pick one per conn.** Both call
`UDPConn.ReadPacket()`, which isn't safe to call concurrently (see
[06](06-gotchas.md#one-reader-one-writer)).

## Option A: your own `ReadPacket` loop

Best when you want full control (per-user routing, jitter handling, your own
shutdown). This is what `_examples/echo` does.

```go
func listen(conn voice.Conn) {
    for {
        pkt, err := conn.UDP().ReadPacket()
        if err != nil {
            if errors.Is(err, net.ErrClosed) {
                return // conn.Close() or external disconnect
            }
            // decryption / DAVE errors land here; usually safe to keep going
            slog.Debug("voice read error", slog.Any("err", err))
            continue
        }

        userID := conn.UserIDBySSRC(pkt.SSRC) // 0 if not known yet
        frame := slices.Clone(pkt.Opus)       // MUST copy, buffer is reused
        handleFrame(userID, pkt.SSRC, pkt.Sequence, pkt.Timestamp, frame)
    }
}
```

Start it after `conn.Open(...)` returns. Before that, the UDP socket isn't
dialed and `ReadPacket` would dereference a nil `net.Conn`.

## Option B: `OpusFrameReceiver` + the default `AudioReceiver`

```go
type OpusFrameReceiver interface {
    ReceiveOpusFrame(userID snowflake.ID, packet *voice.Packet) error
    CleanupUser(userID snowflake.ID) // called on op 13 CLIENT_DISCONNECT
    Close()                          // called when the receiver closes
}

conn.SetOpusFrameReceiver(myReceiver)
```

`SetOpusFrameReceiver` closes any previous receiver, builds a new one with
`AudioReceiverCreateFunc` (default `NewAudioReceiver`), and calls `Open()`,
which starts a goroutine running:

```
loop:
    pkt, err := conn.UDP().ReadPacket()
    net.ErrClosed  → receiver.Close(), stop
    other error    → log at Error level, continue
    ok             → opusReceiver.ReceiveOpusFrame(conn.UserIDBySSRC(pkt.SSRC), pkt)
                     (errors returned are logged at Error level)
```

Notes:

- `ReceiveOpusFrame` runs **on the read goroutine**. If it blocks, you stop
  reading the socket and the kernel buffer drops packets. Keep it fast: copy
  the frame and push it onto a channel.
- `packet.Opus` is still an aliased buffer here too, so copy it.
- The built-in `voice.NewOpusWriter(w, filter)` writes
  `[uint32 LE length][opus bytes]` records to any `io.Writer`, optionally
  filtered by a `UserFilterFunc`. It's handy for dumping raw captures to a file
  and replaying them with `voice.NewOpusReader`.
- On a disconnect from outside (kick or channel delete),
  `HandleVoiceStateUpdate` closes the receiver for you.

### Example: per-user fan-out receiver

```go
type userStreams struct {
    mu      sync.Mutex
    streams map[snowflake.ID]chan []byte
    onNew   func(userID snowflake.ID, frames <-chan []byte)
}

func (u *userStreams) ReceiveOpusFrame(userID snowflake.ID, p *voice.Packet) error {
    if userID == 0 {
        return nil // SSRC not mapped yet, see below
    }
    u.mu.Lock()
    ch, ok := u.streams[userID]
    if !ok {
        ch = make(chan []byte, 64)
        u.streams[userID] = ch
        go u.onNew(userID, ch)
    }
    u.mu.Unlock()

    select {
    case ch <- slices.Clone(p.Opus):
    default: // consumer is behind: drop instead of stalling the socket
    }
    return nil
}

func (u *userStreams) CleanupUser(userID snowflake.ID) {
    u.mu.Lock()
    defer u.mu.Unlock()
    if ch, ok := u.streams[userID]; ok {
        close(ch)
        delete(u.streams, userID)
    }
}

func (u *userStreams) Close() {
    u.mu.Lock()
    defer u.mu.Unlock()
    for id, ch := range u.streams {
        close(ch)
        delete(u.streams, id)
    }
}
```

## SSRC → user ID

Packets carry an **SSRC**, not a user. `Conn` keeps a
`map[uint32]snowflake.ID` that is:

- **filled** only from incoming **op 5 SPEAKING** messages
  (`{ssrc, user_id, speaking}`), which Discord sends when a user starts
  transmitting
- **cleared** for a user on **op 13 CLIENT_DISCONNECT**

So:

- `conn.UserIDBySSRC(ssrc)` returns **`0`** for an SSRC it hasn't seen a
  SPEAKING message for. Early packets from a user can arrive before their
  SPEAKING event.
- With a real DAVE session, the user ID is also used to choose the decryption
  key. An unmapped SSRC → user `"0"` → DAVE decrypt likely fails and
  `ReadPacket` **returns an error** for that packet. That is normal right after
  someone starts talking. Log it at debug level and continue.
- To react to "user started/stopped speaking" yourself, set an
  `EventHandlerFunc` and look for `voice.GatewayMessageDataSpeaking`. It runs
  after disgo has updated the map.

```go
voice.WithConnEventHandlerFunc(func(g voice.Gateway, op voice.Opcode, seq int, data voice.GatewayMessageData) {
    switch d := data.(type) {
    case voice.GatewayMessageDataSpeaking:
        slog.Debug("speaking", "user", d.UserID, "ssrc", d.SSRC, "flags", d.Speaking)
    case voice.GatewayMessageDataClientDisconnect:
        slog.Debug("left", "user", d.UserID)
    }
})
```

## What the incoming stream looks like

- One packet ≈ **one 20 ms Opus frame** (48 kHz, normally stereo).
- Discord **only sends packets while a user is transmitting**. Silence means
  no packets, not silent packets. When someone stops, clients usually send a
  few `0xF8 0xFF 0xFE` silence frames (`voice.SilenceAudioFrame`) first.
- Streams are interleaved across users. Demultiplex by SSRC or user ID.
- UDP means **loss, duplication, and reordering are possible**. disgo does no
  jitter buffering, reordering, or loss concealment. If you need them:
  - order/dedupe by `Sequence` (uint16, wraps, so compare with wraparound)
  - detect gaps with `Timestamp` deltas (960 per frame; a jump of 1920 means
    one lost frame)
  - for missing frames, Opus decoders can do PLC/FEC (e.g. call decode with
    nil data or decode FEC from the next packet)
- A long gap in `Timestamp` after a silence period is normal. Use it to insert
  real silence if you're building a time-aligned recording or mix.

## Decoding to PCM

disgo gives you Opus. To process audio samples (speech-to-text, level meters,
mixing, WAV output) you need a decoder. Common Go choices:

- `gopkg.in/hraban/opus.v2`: CGO wrapper around libopus (encode + decode, PLC/FEC)
- `layeh.com/gopus`: older CGO wrapper
- pure-Go decoders exist (e.g. `github.com/pion/opus`), but they're less complete

Decoder settings for Discord audio: **48 000 Hz, 2 channels, 960 samples per
channel per frame** → 1920 `int16` values (3840 bytes) per 20 ms frame.
`voice.OpusFrameSizeBytes` (= 960·2·2 = 3840) is exactly that PCM size. Keep
**one decoder per SSRC/user**, because Opus decoders are stateful.

## Processing pipeline sketch

```
UDP socket
   │  ReadPacket() ─ one goroutine, never blocks on downstream
   ▼
demux by SSRC → user ID    (drop / buffer while user ID == 0)
   │  clone Opus bytes, non-blocking send
   ▼
per-user goroutine:
   reorder/dedupe by Sequence  →  opus.Decode → []int16 PCM (48k stereo)
   │
   ▼
your processing (VAD, STT, recording, mixing …)
```
