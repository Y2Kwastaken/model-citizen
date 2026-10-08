# 02 – Connection lifecycle

What happens between `conn.Open(ctx, channelID, mute, deaf)` and the first
packet you can read.

## Sequence

```
 your code             Conn / Manager               main gateway          voice gateway (ws)          voice UDP
 ─────────             ──────────────               ────────────          ──────────────────          ─────────
 CreateConn(guild) ──► new Conn stored in manager
 Open(ctx, ch) ─────► voiceStateUpdateFunc ───────► op 4 VOICE_STATE_UPDATE
                     (blocks on openedChan)
                                                   ◄── VOICE_STATE_UPDATE (session_id)
                     HandleVoiceStateUpdate: store SessionID, ChannelID
                                                   ◄── VOICE_SERVER_UPDATE (token, endpoint)
                     HandleVoiceServerUpdate: store Token, Endpoint,
                     go gateway.Open() ──────────────────────────────────► dial wss://endpoint?v=8
                                                                          ◄── op 8 HELLO (heartbeat_interval)
                                                     start heartbeat loop
                                                     op 0 IDENTIFY ──────►  (server_id, user_id, session_id,
                                                                             token, max_dave_protocol_version)
                                                                          ◄── op 2 READY (ssrc, ip, port, modes)
                     handleMessage(Ready):
                       udp.Open(ip, port, ssrc) ─────────────────────────────────────────────────────► IP discovery request
                                                                                                   ◄── IP discovery reply
                       ChooseEncryptionMode(modes)
                       op 1 SELECT_PROTOCOL ────────────────────────────►  (our ip, our port, mode)
                                                                          ◄── op 4 SESSION_DESCRIPTION
                                                                              (mode, secret_key, dave_protocol_version)
                     udp.SetSecretKey(mode, key)
                     openedChan <- struct{}{}
 Open() returns ◄─── 
                                                                          ◄── op 11 CLIENTS_CONNECT / op 5 SPEAKING / DAVE ops …
```

Key points:

1. **`Open` blocks until `SESSION_DESCRIPTION` arrives** (or `ctx` expires).
   Give it a real timeout; the echo example uses 5 s.
2. The voice gateway is opened in a goroutine from `HandleVoiceServerUpdate`
   with its own **hard-coded 5 s** timeout. Errors there are only logged, so if
   the gateway fails you'll just see `Open` time out.
3. `Gateway.Send` refuses to send anything until status is `StatusReady`
   (returns `discord.ErrShardNotReady`). Identify/Resume use an internal path
   that bypasses this check.

## Gateway status values

`StatusUnconnected → StatusConnecting → StatusWaitingForHello → StatusIdentifying
(or StatusResuming) → StatusWaitingForReady → StatusReady`, and
`StatusDisconnected` after close. Read it with `conn.Gateway().Status()`.

## Voice gateway opcodes (v8)

| Op | Name | Direction | Handled by disgo |
| --- | --- | --- | --- |
| 0 | Identify | → | sent after Hello if no previous SSRC/seq |
| 1 | Select Protocol | → | sent after Ready |
| 2 | Ready | ← | stores SSRC, opens UDP |
| 3 | Heartbeat | → | every `heartbeat_interval`, includes `seq_ack` |
| 4 | Session Description | ← | sets transport key, tells DAVE the protocol version, unblocks `Open` |
| 5 | Speaking | ↔ | **incoming: fills the SSRC → user map**; outgoing: `SetSpeaking` |
| 6 | Heartbeat ACK | ← | nonce must match, otherwise reconnect |
| 7 | Resume | → | sent after Hello if SSRC and seq are known |
| 8 | Hello | ← | starts the heartbeat loop |
| 9 | Resumed | ← | status → Ready |
| 11 | Clients Connect | ← | `daveSession.AddUser` for each user ID |
| 13 | Client Disconnect | ← | `daveSession.RemoveUser`, removes the user's SSRC, `audioReceiver.CleanupUser` |
| 14 | Guild Sync | ← | parsed, ignored |
| 21–31 | DAVE ops | ↔ | forwarded to the `godave.Session` (see [03](03-packet-format.md#layer-3-dave-end-to-end-encryption)) |

Binary opcodes (21, 26, 27, 28, 29, 30) go over the websocket as binary frames:
`uint8 opcode` followed by the payload. Everything else is JSON
`{"op": n, "d": {...}}`.

Every message, after disgo's internal handling, is passed to your
`EventHandlerFunc` if you set one (`WithConnEventHandlerFunc` or
`conn.SetEventHandlerFunc`). That's the hook for things like "user X started
speaking" (op 5) or "user X left the call" (op 13).

## Heartbeats & reconnects

- The heartbeat loop runs every `heartbeat_interval`. If the previous heartbeat
  was never ACKed, it closes with 1012 (`CloseServiceRestart`) and reconnects.
- Reconnect backoff is `try * min(2^n s, 10 s)`, so the first retry happens
  immediately.
- A reconnect **resumes** (op 7) when the gateway still has an SSRC and a
  sequence number. A clean close (1000/1001) clears both, so the next open
  identifies from scratch.
- Whether a close code allows reconnect is in `voice/gateway_opcodes.go`
  (`GatewayCloseEventCodes`). The ones that don't:

| Code | Meaning |
| --- | --- |
| 4003 | Not authenticated |
| 4004 | Authentication failed |
| 4006 | Session no longer valid |
| 4009 | Session timeout |
| 4011 | Server not found |
| 4012 | Unknown protocol |
| 4014 | Disconnected (kicked, main session dropped, channel moved) |
| 4016 | Unknown encryption mode |
| 4017 | **DAVE protocol required** (the channel requires E2EE and you're on the noop session) |
| 4021 | Rate limited |
| 4022 | Call terminated |

When reconnect isn't possible (or `AutoReconnect` is off), the gateway calls
the conn's close handler, which calls `conn.Close()`. That tears down UDP and
removes the conn from the manager.

## Leaving / closing

- `conn.Close(ctx)` sends a VOICE_STATE_UPDATE with `channel_id: null`, closes
  the websocket and UDP socket, and removes the conn from the manager.
- If the bot gets **disconnected externally** (kicked, channel deleted),
  `HandleVoiceStateUpdate` sees `ChannelID == nil` and closes the
  sender, receiver, UDP, and gateway. **The conn is not removed from the
  manager** in that path, so `GetConn(guildID)` still returns it.
  `CreateConn` returns the same (dead) instance, so call
  `VoiceManager.RemoveConn(guildID)` (or `conn.Close`) before reconnecting.
- If the bot is **moved** to another channel, `HandleVoiceStateUpdate` only
  updates `ChannelID`. If a new VOICE_SERVER_UPDATE arrives while the voice
  websocket is still open, `gateway.Open` fails. `open()` returns
  `voice.ErrGatewayAlreadyConnected`, but `doReconnect` checks for the
  *different* `discord.ErrGatewayAlreadyConnected`, so it keeps retrying until
  its 5 s context runs out and then logs an error. For a clean channel switch,
  `Close` the conn and open a fresh one.
