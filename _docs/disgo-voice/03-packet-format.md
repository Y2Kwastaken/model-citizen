# 03 – Packet format

Voice audio travels over UDP as RTP packets. Each Opus frame is wrapped in
**three layers**, from innermost to outermost:

```
Opus frame  ──DAVE encrypt (E2EE)──►  DAVE frame  ──transport AEAD──►  RTP payload  ──+ RTP header──►  UDP datagram
```

`UDPConn.Write(opus)` applies the layers in that order, and
`UDPConn.ReadPacket()` removes them in reverse.

## IP discovery (first UDP exchange)

Before any audio, `UDPConn.Open` sends one 74-byte discovery packet so it can
learn its public IP and port (needed for SELECT_PROTOCOL):

```
offset  size  field
0       2     type    = 0x0001 (request)    / 0x0002 (response)
2       2     length  = 70
4       4     SSRC    (ours, from READY)
8       64    address (null-padded ASCII; empty in request, our public IP in response)
72      2     port    (big-endian; our public port in response)
```

The response must have type 2, length 70, and our SSRC, otherwise `Open` errors.
Both the write and the read have a 5 s deadline.

## RTP header (12 bytes, big-endian)

```
 0                   1                   2                   3
 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|V=2|P|X|  CC   |M|     PT      |       sequence number         |   byte 0 = 0x80, byte 1 = 0x78
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                           timestamp                           |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                             SSRC                              |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|            CSRC list (CC × 4 bytes, normally empty)           |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
| ext profile (2) | ext length in 32-bit words (2) |   only if X=1
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
```

| Constant | Value | Meaning |
| --- | --- | --- |
| `RTPHeaderSize` | 12 | fixed header |
| `RTPVersionPadExtend` | `0x80` | version 2, no padding, no extension, CC=0 (what **we send**) |
| `RTPPayloadType` | `0x78` (120) | Discord's Opus payload type |
| `MaxOpusFrameSize` | 1400 | size of the receive buffer |
| `OpusFrameSize` | 960 | samples per channel per 20 ms at 48 kHz; the timestamp step per packet |

**Sequence** is incremented by 1 per packet and **timestamp** by 960 per packet
(48 000 Hz × 0.02 s), both per sender. **SSRC** identifies the sender. It is
*not* a user ID; see [04](04-receiving-audio.md#ssrc--user-id).

Packets from Discord with the extension bit set are common. Discord puts a
header extension on incoming audio.

## Layer 2: transport encryption (`*_rtpsize` AEAD)

Chosen in `ChooseEncryptionMode`: `aead_aes256_gcm_rtpsize` if offered,
otherwise `aead_xchacha20_poly1305_rtpsize`. The 32-byte key comes from
SESSION_DESCRIPTION.

"rtpsize" means the **unencrypted part is the RTP header plus the 4-byte
extension preamble** (profile + length). The extension *body* is encrypted
along with the audio.

```
┌──────────── AAD (authenticated, not encrypted) ─────────────┐┌──────── ciphertext + tag ────────┐┌─ nonce ─┐
│ RTP header (12) │ CSRCs (4×CC) │ ext profile+len (4, if X=1) ││ ext body │ DAVE/Opus payload │tag││ 4 bytes │
└─────────────────────────────────────────────────────────────┘└──────────────────────────────────┘└─────────┘
```

- **Nonce**: a 32-bit counter, written into the first 4 bytes of a zeroed
  nonce (12 bytes for GCM, 24 for XChaCha), appended to the end of the packet.
  disgo writes its outgoing counter **little-endian** (`binary.LittleEndian.PutUint32`).
  On receive it just copies the trailing 4 bytes into the nonce, so byte order
  doesn't matter there.
- **Encrypt**: `cipher.Seal(header, nonce, data, header)`, then append `nonce[:4]`.
  Only the fixed 12-byte header is AAD on send, because disgo never sends CSRCs or extensions.
- **Decrypt**: `cipher.Open(nil, nonce, packet[hdr:len-4], packet[:hdr])`, where
  `hdr` = 12 + 4·CC (+4 if X).

`NoopEncrypter` (`EncryptionModeNone`) exists for tests only. Discord rejects it.

## Layer 3: DAVE end-to-end encryption

DAVE is Discord's E2EE protocol (MLS group keys negotiated over voice-gateway
opcodes 21–31). disgo delegates all of it to a `godave.Session`:

- The gateway forwards DAVE opcodes to the session (`OnDavePrepareTransition`,
  `OnDaveMLSWelcome`, and so on). The conn implements `godave.Callbacks` so the
  session can send key packages/commits back.
- `UDPConn.Write` calls `daveSession.Encrypt(ourSSRC, opus, buf)` **before**
  transport encryption.
- `UDPConn.ReadPacket` calls `daveSession.Decrypt(userID, payload, buf)`
  **after** transport decryption. It looks up the *user ID* from the SSRC
  because DAVE keys are per user.

Implementations:

| Session | Package | Notes |
| --- | --- | --- |
| `godave.NewNoopSession` | `github.com/disgoorg/godave` | **The default.** Passes bytes through and advertises `max_dave_protocol_version: 0`. It logs: *"Using noop dave session… your audio connections will stop working on 01.03.2026"*. That date (1 Mar 2026) has passed. |
| `golibdave.NewSession` | `github.com/disgoorg/godave/golibdave` | CGO binding to Discord's `libdave`. Needs a C toolchain and the native library at build time, which affects the Dockerfile. |
| `session.NewSession` | `github.com/thomas-vilte/dave-go` | Pure-Go implementation, marked experimental. |

Enable one with `bot.WithVoiceManagerConfigOpts(voice.WithDaveSessionCreateFunc(...))`.

## What `ReadPacket()` does, step by step

From `voice/udp_conn.go`. The loop reads datagrams until it gets one it can
return:

1. `conn.Read(receiveBuffer)` (1400-byte buffer). A read error is returned
   wrapped. `net.ErrClosed` means the socket was closed.
2. `n < 12` → **skip**.
3. `byte[1] != 0x78` → **skip** (drops RTCP and anything that isn't Opus).
4. Padding check (`byte[0] & 0x04`): strip `buf[n-1]` bytes if set.
   *(See [06](06-gotchas.md#padding-bit-mask-is-wrong) — this mask is wrong.)*
5. Parse `Sequence`, `Timestamp`, `SSRC`, `HasExtension` (`byte[0] & 0x10`), and CC (`byte[0] & 0x0F`).
6. Read the CSRCs. If X is set, read the extension profile (`ExtensionID`) and
   length in words, and add 4 to the header size. Short packets → **skip**.
7. Transport-decrypt. **An error is returned, not skipped.**
8. Slice the extension body (`length × 4` bytes) off the front of the plaintext → `Packet.Extension`.
9. `ssrcLookup(SSRC)` → user ID → DAVE-decrypt into `decryptBuffer`.
   **An error is returned.**
10. `Packet.Opus = decryptBuffer[:n]` and return `&Packet`.

`Read(p []byte)` (the `io.Reader` form) calls `ReadPacket` and copies only the
Opus bytes into `p`, so SSRC and the other fields are lost.

## The `Packet` struct

```go
type Packet struct {
    Type         byte     // always 0x78 (RTPPayloadType)
    Sequence     uint16   // per-sender, wraps at 65535
    Timestamp    uint32   // per-sender, +960 per 20 ms frame
    SSRC         uint32   // sender; map to a user with conn.UserIDBySSRC
    HasExtension bool
    ExtensionID  int      // RTP extension profile (e.g. 0xBEDE one-byte header)
    Extension    []byte   // decrypted extension body — aliases an internal buffer
    CSRC         []uint32
    HeaderSize   int      // bytes of unencrypted header incl. CSRCs + ext preamble
    Opus         []byte   // decrypted Opus frame — aliases an internal buffer!
}
```

The `Packet` itself is freshly allocated on every call, but **`Opus` and
`Extension` point into buffers that the next `ReadPacket` overwrites**.
Copy them if you keep them past the next read.
