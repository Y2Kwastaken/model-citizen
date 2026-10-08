# 01 – Architecture

disgo's voice support lives in the `github.com/disgoorg/disgo/voice` package.
Everything in it is defined as an interface with a default implementation, and
every default can be swapped through functional options (`With...CreateFunc`).

## The layers

```
bot.Client
 └── VoiceManager  (voice.Manager)          one per bot
      └── Conn     (voice.Conn)             one per guild
           ├── Gateway   (voice.Gateway)    websocket to the voice server (control plane)
           ├── UDPConn   (voice.UDPConn)    UDP socket to the voice server (audio plane)
           ├── godave.Session               end-to-end encryption (DAVE / MLS)
           ├── AudioSender   (optional)     goroutine that pulls frames from an OpusFrameProvider
           └── AudioReceiver (optional)     goroutine that pushes frames to an OpusFrameReceiver
```

| Type | File | Job |
| --- | --- | --- |
| `Manager` | `voice/manager.go` | Holds a `map[guildID]Conn`. Forwards main-gateway `VOICE_STATE_UPDATE` / `VOICE_SERVER_UPDATE` events to the right `Conn`. |
| `Conn` | `voice/conn.go` | Glue object. Owns the gateway and UDP socket, runs the handshake, keeps the **SSRC → user ID** map, and hosts the sender/receiver. |
| `Gateway` | `voice/gateway.go` | The voice websocket (`wss://<endpoint>?v=8`). Identify/resume, heartbeats, reconnects, and forwarding DAVE opcodes to the DAVE session. |
| `UDPConn` | `voice/udp_conn.go` | IP discovery, RTP header building/parsing, transport encryption, DAVE encrypt/decrypt. Implements `net.Conn`, `io.Reader`, `io.Writer`. |
| `Encrypter` | `voice/encryption_modes.go` | Transport-layer AEAD (`aead_aes256_gcm_rtpsize` preferred, `aead_xchacha20_poly1305_rtpsize` required fallback). |
| `AudioSender` | `voice/audio_sender.go` | Default: a 20 ms paced loop calling `OpusFrameProvider.ProvideOpusFrame()` and writing to UDP. |
| `AudioReceiver` | `voice/audio_receiver.go` | Default: a tight loop calling `UDPConn.ReadPacket()` and handing each packet to `OpusFrameReceiver.ReceiveOpusFrame()`. |
| `OpusReader` / `OpusWriter` | `voice/opus.go` | Simple provider/receiver that read/write length-prefixed Opus frames from an `io.Reader` / to an `io.Writer`. |

**Important:** disgo only deals in **Opus** frames. There is no Opus encoder or
decoder in disgo. To get PCM you need a separate library (e.g. a libopus
binding); see [04 – Receiving audio](04-receiving-audio.md#decoding-to-pcm).

## How it's wired into `bot.Client`

`bot/config.go` builds a manager automatically when the client is created:

```go
cfg.VoiceManager = voice.NewManager(client.UpdateVoiceState, *id,
    append([]voice.ManagerConfigOpt{voice.WithLogger(cfg.Logger)}, cfg.VoiceManagerConfigOpts...)...)
```

- `client.UpdateVoiceState` is passed in as the `StateUpdateFunc`. It sends
  opcode 4 (`VOICE_STATE_UPDATE`) on the **main** gateway shard for that guild,
  which is how the bot actually joins/leaves a channel.
- `bot/handlers/voice_handlers.go` forwards events into the manager:
  - `VOICE_STATE_UPDATE` → `VoiceManager.HandleVoiceStateUpdate` (only when
    `event.UserID == client.ID()`, i.e. the bot's own state)
  - `VOICE_SERVER_UPDATE` → `VoiceManager.HandleVoiceServerUpdate`
- `client.Close()` calls `VoiceManager.Close()`, which closes every `Conn`.

The manager is reachable as the field `client.VoiceManager` (not a method,
despite what the package README shows).

## Configuration knobs

Options nest: manager options can carry conn options, which can carry gateway
and UDP options.

```go
disgo.New(token,
    bot.WithGatewayConfigOpts(gateway.WithIntents(gateway.IntentGuilds, gateway.IntentGuildVoiceStates)),
    bot.WithVoiceManagerConfigOpts(
        voice.WithDaveSessionCreateFunc(golibdave.NewSession), // real E2EE (see 03)
        voice.WithConnConfigOpts(
            voice.WithConnEventHandlerFunc(onVoiceGatewayEvent), // see every voice-gateway message
            voice.WithConnAudioReceiverCreateFunc(myReceiverFactory),
            voice.WithUDPConnConfigOpts(voice.WithUDPConnDialer(&net.Dialer{Timeout: 10 * time.Second})),
            voice.WithConnGatewayConfigOpts(voice.WithGatewayAutoReconnect(true)),
        ),
    ),
)
```

| Level | Options |
| --- | --- |
| Manager | `WithLogger`, `WithConnCreateFunc`, `WithConnConfigOpts`, `WithDaveSessionCreateFunc`, `WithDaveSessionLogger` |
| Conn | `WithConnLogger`, `WithConnGatewayCreateFunc`, `WithConnGatewayConfigOpts`, `WithUDPConnCreateFunc`, `WithUDPConnConfigOpts`, `WithConnAudioSenderCreateFunc`, `WithConnAudioReceiverCreateFunc`, `WithConnEventHandlerFunc`, `WithConnDaveSessionCreateFunc`, `WithConnDaveSessionLogger` |
| Gateway | `WithGatewayLogger`, `WithGatewayDialer` (gorilla `websocket.Dialer`), `WithGatewayAutoReconnect` (default `true`) |
| UDP | `WithUDPConnLogger`, `WithUDPConnDialer` (default `net.Dialer{Timeout: 30s}`) |

Defaults (`voice/conn_config.go`): `NewGateway`, `NewUDPConn`,
`godave.NewNoopSession`, `NewAudioSender`, `NewAudioReceiver`.
