# 06 – Gotchas & caveats

Things found while reading the disgo v0.19.6 source that are likely to cause
bugs. Line references are to files under `voice/`.

## Buffer aliasing

`Packet.Opus` and `Packet.Extension` are slices into `udpConnImpl.decryptBuffer`
and `AEADEncrypter.recBuf`. **The next `ReadPacket` call overwrites them.**
If you pass a frame to another goroutine or store it, `slices.Clone` it first.
Otherwise you get corrupted audio that's hard to trace.

## One reader, one writer

`udpConnImpl` has a mutex around the `net.Conn` pointer only. The receive and
decrypt buffers, the RTP header, `sequence`, `timestamp`, and the AEAD nonce
counter have **no locking**.

- Only **one goroutine** may call `ReadPacket`/`Read`. Don't set an
  `OpusFrameReceiver` *and* run your own read loop.
- Only **one goroutine** may call `Write`. Don't set an `OpusFrameProvider`
  *and* call `conn.UDP().Write` yourself.
- Reading on one goroutine and writing on another is fine, because they use
  separate state.

## Unknown SSRCs → user ID 0 and DAVE errors

The SSRC map is only filled by incoming SPEAKING (op 5) events. Until one
arrives, `UserIDBySSRC` returns `0`, and with a real DAVE session the packet
fails to decrypt, so `ReadPacket` returns an error. The default
`AudioReceiver` logs every one of these at **Error** level, which can be noisy.
Filter `userID == 0` in your receiver, or write your own read loop and log them
at debug.

## `ReadPacket` returns errors on bad packets

Short/non-Opus packets are skipped silently, but transport- or DAVE-decrypt
failures are **returned as errors**. Your loop must `continue` on
non-`net.ErrClosed` errors instead of exiting.

## Padding bit mask is wrong

`udp_conn.go` checks padding with `byte[0] & 0x04`. In RTP the padding bit is
`0x20`, and `0x04` is part of the CSRC count. In practice Discord sends
CC=0 and no RTP padding, so this doesn't trigger. But if it did, it would strip
bytes based on the last byte of the *encrypted* packet (the nonce), which is
wrong. Worth knowing if you ever see mysteriously truncated packets.

## Marker bit packets are dropped

The payload type check compares the whole second byte to `0x78`. A packet with
the RTP marker bit set (`0xF8`) would be treated as non-voice and skipped.

## Default `AudioReceiver` shutdown quirks

- `Close()` cancels a context, but the goroutine is blocked inside `conn.Read`,
  so it only notices after the **next packet** arrives or the socket closes.
- `cancelFunc` is assigned *inside* the goroutine. Calling `Close()`
  immediately after `Open()` (e.g. calling `SetOpusFrameReceiver` twice in a
  row) can hit a nil `cancelFunc` and panic.
- There's no read deadline, so a silent channel means the goroutine just sits
  in `Read` until the socket closes.

If any of this matters, write your own `AudioReceiver` via
`WithConnAudioReceiverCreateFunc`, or use your own `ReadPacket` loop and close
it by closing the conn.

## `OpusReader` uses `Read`, not `ReadFull`

`OpusReader.ProvideOpusFrame` does `r.Read(lenBuf)` and `r.Read(buf[:n])`. On
pipes, sockets, or ffmpeg stdout a short read can split a frame and
desync the stream. Wrap the source in something that returns full reads, or
write a provider that uses `io.ReadFull`.

## Timestamp is always +960

`UDPConn.Write` assumes 20 ms frames. Sending 10/40/60 ms Opus frames produces
wrong RTP timestamps.

## External disconnect leaves a dead conn in the manager

See [02](02-connection-lifecycle.md#leaving--closing). After a kick or channel
delete, `GetConn` still returns the old conn. Call
`VoiceManager.RemoveConn(guildID)` before `CreateConn` again.

## Channel moves

Also covered in [02](02-connection-lifecycle.md#leaving--closing). A
`voice.ErrGatewayAlreadyConnected` vs `discord.ErrGatewayAlreadyConnected`
mismatch makes re-opening on a live gateway retry until timeout. Close and
reopen instead.

## Package README is slightly out of date

It shows `client.VoiceManager().CreateConn(...)` and
`conn.Close()`. In v0.19.6 `VoiceManager` is a **field**, and `Close` takes a
`context.Context`.

---

## What model-citizen needs before using voice

Current state of `discord-bot/`, and what has to change:

1. **Intents.** `main.go` only requests `gateway.IntentGuilds`. Voice needs
   **`gateway.IntentGuildVoiceStates`**. Without it the bot never receives
   its own VOICE_STATE_UPDATE, the session ID is never set, and `conn.Open`
   times out.
2. **DAVE.** The default noop DAVE session is past Discord's 1 Mar 2026
   cutoff. Configure a real session with
   `bot.WithVoiceManagerConfigOpts(voice.WithDaveSessionCreateFunc(...))`:
   - `golibdave.NewSession` (`github.com/disgoorg/godave/golibdave`, v0.3.0
     in the module cache) is **CGO**.
   - `thomas-vilte/dave-go` is pure Go (experimental).
   - `go.mod` currently pins `godave v0.1.0`. Check compatibility with
     disgo before bumping godave to v0.3.0.
3. **Dockerfile.** The build uses `CGO_ENABLED=0` and a `FROM scratch` runtime
   image. That's fine with a pure-Go DAVE implementation and Opus decoder. If
   you use `golibdave` or a libopus wrapper, you need `CGO_ENABLED=1`, the
   native libraries in the build stage, and a runtime image that has them
   (e.g. `debian:trixie-slim`), or static linking.
4. **Opus codec.** Pick an Opus decoder/encoder if the bot needs PCM
   (see [04](04-receiving-audio.md#decoding-to-pcm)). The same CGO
   considerations apply.
