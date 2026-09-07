# DisGo Notes (v0.19.6)

Working reference for [disgo](https://github.com/disgoorg/disgo), written against the
exact version this repo pins (`v0.19.6`) and cross-checked against the source in the
module cache. Anything marked **verified** was confirmed by reading or running the
library code, not from memory.

## 1. What official documentation exists

There *is* documentation, it's just scattered and thin in places:

| Source | Link | Usefulness |
| --- | --- | --- |
| GoDoc | <https://pkg.go.dev/github.com/disgoorg/disgo> | Complete API surface. The real reference. |
| Examples | <https://github.com/disgoorg/disgo/tree/master/_examples> | 20+ runnable programs. The best "how do I" resource. |
| Repo README | <https://github.com/disgoorg/disgo> | Setup + feature list. |
| `voice/README.md` | <https://github.com/disgoorg/disgo/blob/master/voice/README.md> | Only real voice docs. Partly stale — see §9. |
| Bot template | <https://github.com/disgoorg/bot-template> | Project layout with commands + db. |
| Discord server | <https://discord.gg/TewhTfDpvW> | Where questions actually get answered. |
| GitHub Wiki | — | Announced as "under construction". Effectively empty. |
| Discord API docs | <https://discord.com/developers/docs> | disgo mirrors Discord's model closely; upstream docs carry over. |

The examples ship inside your module cache, so you can read them offline:

```sh
ls "$(go env GOMODCACHE)/github.com/disgoorg/disgo@v0.19.6/_examples"
```

Notably **there is no voice example** in `_examples` for this version — §9 below fills
that gap, since it's the part this bot needs.

## 2. Package map

| Package | What lives there |
| --- | --- |
| `disgo` | Just `disgo.New()` and `disgo.Version`. Thin entry point. |
| `bot` | `bot.Client` — the central object. Config options (`bot.WithX`), event manager. |
| `discord` | Pure data types: `Message`, `Embed`, `VoiceState`, `SlashCommandCreate`, … No I/O. |
| `gateway` | Websocket connection, intents, presence, raw gateway events. |
| `events` | Typed, dispatched events (`events.GuildReady`, `events.GuildVoiceJoin`, …). |
| `handler` | chi-style router for interactions (slash commands, buttons, modals). |
| `rest` | Every REST endpoint, on `client.Rest`. |
| `cache` | In-memory state (guilds, voice states, …), on `client.Caches`. |
| `voice` | Voice gateway + UDP, opus send/receive. |
| `sharding` | Multi-shard management. |
| `httpserver` | HTTP interactions instead of a gateway connection. |
| `oauth2`, `webhook` | Standalone clients, usable without a bot. |

The `discord` package is data-only. Anything that talks to Discord lives in `rest`,
`gateway`, or `voice`. That split is the main thing to internalise.

## 3. Client lifecycle

```go
client, err := disgo.New(token,
    bot.WithGatewayConfigOpts(gateway.WithIntents(...)),
    bot.WithCacheConfigOpts(cache.WithCaches(...)),
    bot.WithEventListeners(router),
    bot.WithEventListenerFunc(func(e *events.GuildReady) { ... }),
)
if err != nil { ... }

if err = client.OpenGateway(ctx); err != nil { ... }  // nothing connects until this
defer client.Close(ctx)
```

`disgo.New` only builds the client — no network calls. `OpenGateway` connects.
`client.Close` closes voice, gateway, REST, shards and the HTTP server, each guarded
by a nil check (**verified** in `bot/client.go`).

`bot.Client` exposes its subsystems as **struct fields**, not methods:

```go
client.Rest           // rest.Rest
client.Caches         // cache.Caches
client.Gateway        // gateway.Gateway
client.VoiceManager   // voice.Manager   <- field, not VoiceManager()
client.ApplicationID  // snowflake.ID
client.Logger         // *slog.Logger
```

This matters because the upstream `voice/README.md` still writes
`client.VoiceManager().CreateConn(...)`. That's an older API — in v0.19.6 it's a plain
field access (**verified** against `bot/client.go`).

### Logging

disgo logs through `log/slog`. Set the global default and disgo follows it:

```go
slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})))
```

Debug level is where gateway payloads and voice handshake detail show up — the first
thing to reach for when a voice connection silently doesn't work.

## 4. Intents and cache

Two independent switches that are easy to confuse:

- **Intents** decide what Discord *sends you over the gateway*.
- **Cache flags** decide what disgo *keeps in memory* from what arrives.

Caching something you never receive is silently useless, so the pair has to line up.
This repo does that correctly:

```go
gateway.WithIntents(
    gateway.IntentGuilds,
    gateway.IntentGuildMessages,
    gateway.IntentMessageContent,   // privileged: enable in the dev portal
    gateway.IntentGuildVoiceStates, // needed to know who is in which voice channel
)
cache.WithCaches(cache.FlagGuilds, cache.FlagVoiceStates)
```

Privileged intents (`IntentMessageContent`, `IntentGuildMembers`, `IntentGuildPresences`)
must also be toggled on in the Discord developer portal, or the gateway rejects the
connection at identify time.

Available cache flags (**verified**, `cache/cache_flags.go`): `FlagGuilds`,
`FlagGuildScheduledEvents`, `FlagMembers`, `FlagThreadMembers`, `FlagMessages`,
`FlagPresences`, `FlagChannels`, `FlagRoles`, `FlagEmojis`, `FlagStickers`,
`FlagVoiceStates`, `FlagStageInstances`, `FlagGuildSoundboardSounds`, plus `FlagsNone`
and `FlagsAll`.

Reading the cache — every getter returns `(value, ok)`:

```go
state, ok := client.Caches.VoiceState(guildID, userID)
guild, ok := client.Caches.Guild(guildID)
self,  ok := client.Caches.SelfUser()

for state := range client.Caches.VoiceStates(guildID) { // iter.Seq, Go 1.23+ range-over-func
    _ = state
}
```

## 5. Events

Register listeners at build time or later:

```go
bot.WithEventListenerFunc(func(e *events.MessageCreate) { ... })  // generic, type-inferred
bot.WithEventListeners(router)                                    // anything with OnEvent(bot.Event)
client.AddEventListeners(...)                                     // after construction
```

Every event embeds `*events.GenericEvent`, which gives you `e.Client()` — the way back
to REST, caches and the voice manager from inside a handler.

Useful events for this bot:

| Event | Fires when |
| --- | --- |
| `events.Ready` | Gateway identified. |
| `events.GuildReady` | A guild finished loading (on startup, per guild). |
| `events.GuildsReady` | All guilds loaded. |
| `events.GuildJoin` | Bot added to a new guild. |
| `events.GuildVoiceJoin` | A member joined a voice channel. |
| `events.GuildVoiceMove` | A member switched voice channels. |
| `events.GuildVoiceLeave` | A member left. Carries `OldVoiceState`. |
| `events.GuildVoiceStateUpdate` | Any voice state change (mute/deaf/etc). |

The voice member events carry `VoiceState` and `Member` inline via
`*GenericGuildVoiceState`, and the leave/move variants add `OldVoiceState` — that's
how you detect "the last human left, disconnect" (**verified**,
`events/guild_voice_events.go`).

### Dispatch is synchronous by default

**Verified** in `bot/event_manager.go`: listeners run one after another on the dispatch
goroutine unless you pass `bot.WithEventManagerConfigOpts(bot.WithAsyncEventsEnabled())`.
A handler that blocks — a long download, a slow HTTP call — stalls later events.
Do slow work in its own goroutine.

Dispatch also wraps listeners in `recover()`, so a panicking handler is logged and the
bot survives. The interaction it was serving still gets no response.

## 6. Interactions: the handler router

`handler.Mux` is modelled on `go-chi/chi`. Each interaction is routed by a *path*:

- Slash commands: `/name`, `/name/subcommand`, `/group/sub/command`
- Components and modals: their custom ID

```go
router := handler.New()
router.SlashCommand("/ping", handlePing)
router.ButtonComponent("/button/{id}", handleButton)   // {id} is a path variable
router.Modal("/my-modal", handleModal)

router.Route("/music", func(r handler.Router) {
    r.Use(requireVoice)              // middleware for this subtree only
    r.SlashCommand("/play", handlePlay)   // matches /music/play
})

router.Error(func(e *handler.InteractionEvent, err error) { ... })  // central error handling
router.NotFound(func(e *handler.InteractionEvent) error { ... })
```

Register the router as an event listener — it implements `OnEvent`:

```go
bot.WithEventListeners(router)
```

Handler signatures (from `handler/mux.go`):

```go
func(data discord.SlashCommandInteractionData, e *handler.CommandEvent) error
func(data discord.ButtonInteractionData,       e *handler.ComponentEvent) error
func(data discord.ModalSubmitInteractionData,  e *handler.ModalEvent) error
```

Path variables land in `e.Vars`, and `e.Ctx` carries the request context.

### Responding

You have **3 seconds** to make an initial response or Discord marks the interaction
failed. Anything slower must defer first:

```go
// Fast path
return e.CreateMessage(discord.NewMessageCreate().WithContent("pong"))

// Slow path
if err := e.DeferCreateMessage(false); err != nil { return err }
result := doSlowThing()
_, err := e.UpdateInteractionResponse(discord.NewMessageUpdate().WithContent(result))
return err
```

Other responses: `UpdateMessage`, `DeferUpdateMessage`, `Modal`, `AutocompleteResult`,
`CreateFollowupMessage`, `LaunchActivity`.

### Ephemeral responses (only the invoker sees them)

```go
return e.CreateMessage(discord.NewMessageCreate().
    WithContent("only you can see this").
    WithEphemeral(true))
```

`WithEphemeral(true)` sets `discord.MessageFlagEphemeral`, so
`WithFlags(discord.MessageFlagEphemeral)` is equivalent — the helper is preferable
because passing `false` also clears the flag (**verified**, `discord/message_create.go`).

When deferring, ephemerality is fixed at *defer* time, not when the content is sent:
`DeferCreateMessage(true)` sends `MessageCreate{Flags: MessageFlagEphemeral}` as the
deferred response (**verified**, `handler/interaction.go`). `MessageUpdate` has no
ephemeral toggle, and Discord does not allow flipping a message between ephemeral and
public by editing — so choose correctly at the defer:

```go
if err := e.DeferCreateMessage(true); err != nil {
    return err
}
_, err := e.UpdateInteractionResponse(discord.NewMessageUpdate().WithContent("done"))
```

Follow-ups carry their own flag, which is how one command mixes public and private:

```go
e.CreateMessage(discord.NewMessageCreate().WithContent("public result"))
e.CreateFollowupMessage(discord.NewMessageCreate().
    WithContent("only you see this").WithEphemeral(true))
```

Limits worth knowing: ephemeral messages exist only as interaction responses (never via
`client.Rest.CreateMessage`), they don't survive a client restart, and they can't carry
file attachments. Good for validation errors and "you're not in a voice channel" —
which is why the `/join` example in §9 uses them for its failure paths.

### Message construction

v0.19 uses immutable value types with chained `With*` methods, **not** the older
`NewMessageCreateBuilder()` you'll find in blog posts and older examples:

```go
discord.NewMessageCreate().
    WithContent("hello").
    WithEphemeral(true).
    WithEmbeds(discord.NewEmbed().WithTitle("t").WithDescription("d"))
```

Each `With*` returns a new value rather than mutating in place (**verified**,
`discord/message_create.go`, `discord/embed.go`). Embeds work the same way —
`discord.NewEmbed().WithTitle(...)`, and there's no `.Build()` call at the end.

### Reading options

`SlashCommandInteractionData` has two getter families — panicking-ish defaults and
comma-ok variants:

```go
name := data.String("name")            // zero value if absent
n, ok := data.OptInt("count")          // explicit presence check
user  := data.User("target")
ch, ok := data.OptChannel("channel")
```

Also available: `Bool`, `Float`, `Snowflake`, `Role`, `Member`, `Mentionable`,
`Attachment`, each with an `Opt` form.

Interaction context, on any event: `e.GuildID() *snowflake.ID` (nil in DMs),
`e.User()`, `e.Member()`, `e.Channel()`, `e.Client()`.

## 7. Registering commands

Commands must be pushed to Discord; declaring them in code isn't enough.

```go
// Guild commands: instant, ideal for development
client.Rest.SetGuildCommands(client.ApplicationID, guildID, creates)

// Global commands: visible everywhere, propagation can take up to an hour
client.Rest.SetGlobalCommands(client.ApplicationID, creates)

// Convenience wrapper — global when guildIDs is empty
handler.SyncCommands(client, creates, []snowflake.ID{guildID})
```

`Set*Commands` is a full replacement: anything missing from the slice is deleted.
This repo syncs per-guild on `GuildReady` and `GuildJoin`, which is the right pattern
during development.

## 8. REST

`client.Rest` covers the whole API. Rate limiting is handled internally — you don't
need to throttle calls yourself.

```go
msg, err := client.Rest.CreateMessage(channelID, discord.NewMessageCreate().WithContent("hi"))
ch,  err := client.Rest.GetChannel(channelID)
err       = client.Rest.AddMemberRole(guildID, userID, roleID)
```

Every method takes trailing `...rest.RequestOpt`, which is where per-request context,
reason headers and custom retries go:

```go
client.Rest.CreateMessage(channelID, msg,
    rest.WithCtx(ctx),
    rest.WithReason("audit log entry"),
)
```

## 9. Voice

The part with the least upstream documentation, and the part this bot is heading
toward. All of the following is **verified** against `voice/` in v0.19.6.

### DAVE (E2EE) is mandatory

**Discord requires DAVE for voice as of 2026-03-01.** That deadline has passed, so this
is not optional setup — a bot using the default noop session gets its audio dropped.

disgo defaults `DaveSessionCreate` to `godave.NewNoopSession` (**verified**,
`voice/conn_config.go`), which is a plain passthrough that encrypts nothing. It logs
this on every `CreateConn`:

```
WARN Using noop dave session. Please migrate to an implementation of libdave or your
     audio connections will stop working on 01.03.2026
```

Two implementations of the `godave.Session` interface exist:

| Implementation | Trade-off |
| --- | --- |
| [`godave/golibdave`](https://github.com/disgoorg/godave) | Official CGO binding to Discord's libdave. Needs a native shared library. What this repo uses. |
| [`dave-go`](https://github.com/thomas-vilte/dave-go) | Pure Go, no CGO. Upstream labels it experimental. |

Wiring it in:

```go
import (
    "github.com/disgoorg/disgo/voice"
    "github.com/disgoorg/godave/golibdave"
)

disgo.New(token,
    bot.WithVoiceManagerConfigOpts(
        voice.WithDaveSessionCreateFunc(golibdave.NewSession),
    ),
)
```

### Installing libdave

`golibdave` links against libdave through `#cgo pkg-config: dave` (**verified**,
`libdave/lib.go`), so the native library, its header, and a `dave.pc` must be present
at build time, and `CGO_ENABLED=1` is required.

The required native version is pinned in the Go module: check
`release.txt` in `github.com/disgoorg/godave/libdave` — for `v0.3.0` it is **libdave
v1.1.0**. Upstream provides prebuilt binaries for Linux/macOS/Windows on x64 and arm64.

For local development, godave ships an installer that fetches the prebuilt library
into `~/.local`:

```sh
sh "$(go env GOMODCACHE)/github.com/disgoorg/godave@v0.3.0/scripts/libdave_install.sh" v1.1.0
export PKG_CONFIG_PATH="$HOME/.local/lib/pkgconfig:$PKG_CONFIG_PATH"
```

Without it, `go build ./...` fails with `Package dave was not found in the pkg-config
search path` — the build breaks on the host even though the Docker build is fine.

The `Dockerfile` in this repo does the equivalent inline: it downloads the prebuilt zip,
installs the header and `.so` into `/usr/local`, writes a `dave.pc`, and builds with
`CGO_ENABLED=1`.

### Version skew with disgo v0.19.6

disgo v0.19.6 pins `godave v0.1.0`, but `golibdave v0.3.0` requires `godave v0.3.0`, so
module resolution lands on v0.3.0. This works — the build and a live session were both
verified — with one caveat.

godave v0.3.0 added `Ready()` and `Close()` to the `Session` interface, and disgo
v0.19.6 predates both, so it calls neither:

- **`Ready()`** signals whether the MLS handshake has established an epoch. A caller
  that gated on it would hold frames instead of sending them in the clear. disgo doesn't,
  so frames sent in the brief window right after joining go out as passthrough.
  godave documents this as a supported degradation ("callers that don't gate on Ready
  continue to work"), not a failure.
- **`Close()`** is a no-op in golibdave, and the underlying libdave objects register
  `runtime.SetFinalizer` (**verified**, `libdave/session.go`, `encryptor.go`), so native
  memory is still reclaimed by the GC. Not a leak.

`CreateConn` also returns an existing connection for a guild rather than building a
second one (**verified**, `voice/manager.go`), so repeated `/join` calls in one guild
reuse a single DAVE session.

### Required setup

```go
gateway.WithIntents(gateway.IntentGuildVoiceStates)   // to see who's in voice
cache.WithCaches(cache.FlagVoiceStates)               // to look it up later
```

### Joining a channel

```go
conn := client.VoiceManager.CreateConn(guildID)       // field, not a method
err := conn.Open(ctx, channelID, selfMute, selfDeaf)  // blocks until connected
```

`Open` sends the voice state update, waits for Discord's `VoiceStateUpdate` and
`VoiceServerUpdate`, then completes the voice websocket + UDP handshake. Give it a
context with a timeout — without one, a handshake that never completes blocks forever.

**`Open` must not be called on the event goroutine.** This is the single biggest
footgun in disgo's voice API, and it deadlocks:

- disgo reads the gateway on **one** `listen` goroutine and calls the event handler
  synchronously from that loop (**verified**, `gateway/gateway.go`).
- `bot.eventManagerImpl.HandleGatewayEvent` holds a mutex for the whole handler call
  (**verified**, `bot/event_manager.go`).
- `Open` sends the voice state update, then blocks until `HandleVoiceStateUpdate` and
  `HandleVoiceServerUpdate` are fed from *later gateway events*
  (**verified**, `voice/conn.go`, `bot/handlers/voice_handlers.go`).

So blocking in the handler prevents delivery of the very events `Open` waits for. The
symptom is distinctive: **the bot appears in the voice channel instantly** — Discord
processed the outgoing state update — **but the interaction spins until the context
times out**, because the bot never completes its own handshake.

Connect in a goroutine instead, and let the handler return immediately:

```go
func handleJoin(_ discord.SlashCommandInteractionData, event *handler.CommandEvent) error {
    if event.GuildID() == nil {
        return event.CreateMessage(discord.NewMessageCreate().
            WithContent("This command only works in a server.").WithEphemeral(true))
    }
    guildID := *event.GuildID()

    // Cache reads are cheap; do the validation on the event goroutine.
    state, ok := event.Client().Caches.VoiceState(guildID, event.User().ID)
    if !ok || state.ChannelID == nil {
        return event.CreateMessage(discord.NewMessageCreate().
            WithContent("You need to be in a voice channel first.").WithEphemeral(true))
    }
    channelID := *state.ChannelID

    if err := event.DeferCreateMessage(false); err != nil {
        return err
    }

    go func(ctx context.Context) {
        ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
        defer cancel()

        conn := event.Client().VoiceManager.CreateConn(guildID)
        if err := conn.Open(ctx, channelID, false, false); err != nil {
            slog.Error("opening voice connection", slog.Any("err", err))
            event.Client().VoiceManager.RemoveConn(guildID)
            _, _ = event.UpdateInteractionResponse(discord.NewMessageUpdate().
                WithContent("Couldn't join that channel."))
            return
        }
        _, _ = event.UpdateInteractionResponse(discord.NewMessageUpdate().
            WithContent("Joined."))
    }(context.WithoutCancel(event.Ctx))

    return nil
}
```

Two details in there:

- `context.WithoutCancel(event.Ctx)` keeps any context values while detaching from a
  cancellation the handler's return might trigger.
- `RemoveConn` on failure. `CreateConn` registered the conn in the manager's map before
  `Open` ran; without this a failed attempt leaves a dead entry that later `CreateConn`
  calls return instead of building a fresh one.

The alternative global fix is
`bot.WithEventManagerConfigOpts(bot.WithAsyncEventsEnabled())`, which dispatches every
listener on its own goroutine. It resolves the deadlock too, but it changes event
ordering for *all* handlers, so prefer the targeted goroutine.

### Leaving

```go
conn := client.VoiceManager.GetConn(guildID)  // nil if not connected
if conn != nil {
    conn.Close(ctx)
}
client.VoiceManager.RemoveConn(guildID)
```

### Sending audio

Discord wants **opus frames: 48kHz, stereo, 20ms**. disgo does not encode for you —
it moves already-encoded opus frames. Constants (`voice/audio_sender.go`):
`OpusFrameSizeMs = 20`, `OpusFrameSize = 960` samples, `MaxOpusFrameSize = 1400` bytes.

The high-level path — implement `voice.OpusFrameProvider` and let disgo handle pacing,
the speaking flag and silence frames:

```go
type OpusFrameProvider interface {
    ProvideOpusFrame() ([]byte, error)
    Close()
}

conn.SetOpusFrameProvider(myProvider)
```

The built-in sender then, per 20ms tick (**verified**, `voice/audio_sender.go`):

- calls `ProvideOpusFrame()`
- sends `SpeakingFlagMicrophone` before the first frame, `SpeakingFlagNone` when the
  stream runs dry
- emits 5 silence frames on stop, so Discord doesn't leave the last packet ringing
- treats an empty frame or `io.EOF` as "nothing to send" rather than an error

For reading frames straight out of an ogg/opus stream, `voice.NewOpusReader(io.Reader)`
already implements the interface — that's the natural seam for piping `yt-dlp` output.

The low-level path, if you want to own the timing yourself:

```go
conn.SetSpeaking(ctx, voice.SpeakingFlagMicrophone)
conn.UDP().Write(frame)   // you are responsible for 20ms pacing
```

Getting the pacing wrong is the usual cause of choppy or sped-up audio.

### Receiving audio

```go
conn.SetOpusFrameReceiver(myReceiver)  // ReceiveOpusFrame(userID, *voice.Packet) error
conn.UserIDBySSRC(ssrc)                // map an RTP stream back to a user
```

`voice.NewOpusWriter(w, userFilter)` gives you a ready-made receiver with per-user
filtering.

## 10. Gotchas verified in v0.19.6

1. **A nil handler panics.** `router.SlashCommand("/x", nil)` registers fine and only
   fails when someone runs the command: the type switch in `handler/handler.go` matches
   the nil func and calls it. Confirmed by running a minimal reproduction. The event
   manager's `recover()` catches it, so the bot survives, but the user sees
   "application did not respond".

   The `Command` struct in `discord-bot/commands.go` makes this easy to hit: an entry
   with a `Create` but no `Handler` compiles fine and only fails at invocation time.

2. **`VoiceManager` is a field.** `client.VoiceManager.CreateConn(...)`, not
   `client.VoiceManager()`. The upstream voice README shows the old form.

3. **The builder types are gone.** Use `discord.NewMessageCreate().WithX(...)` and
   `discord.NewEmbed().WithX(...)`. `NewMessageCreateBuilder()` and `NewEmbedBuilder()`
   are from older versions and don't exist in v0.19.6 — most tutorials and LLM answers
   still show them, which is the single most common thing to trip over here.

4. **Events dispatch synchronously by default, on one goroutine, under a mutex.** Slow
   handlers delay every later event — and a handler that *waits on another gateway event*
   deadlocks outright. `voice.Conn.Open` is exactly that; see §9.

5. **`Set*Commands` deletes anything you omit.** It's a replace, not a merge.

6. **Intents ≠ cache flags.** You need both, matched, or lookups quietly return `ok == false`.

7. **Cache getters return `(value, ok)`.** Ignoring `ok` gets you a zero-valued struct
   that looks real — a `VoiceState` with a nil `ChannelID` rather than an error.

8. **`Open` on a voice conn can block.** Always pass a context with a timeout.

9. **`e.GuildID()` returns a pointer** and is nil in DMs. Check it before dereferencing.

10. **Privileged intents need portal opt-in.** `IntentMessageContent` (used here) is one;
    without the toggle the gateway refuses to identify.

11. **DAVE is required, and the default is a noop.** See §9. Voice silently degrades
    rather than erroring, so the only signal is a `WARN` line at `CreateConn` time.

12. **Adding golibdave makes the build CGO-dependent.** `go build` fails on any machine
    without libdave + `dave.pc` installed. Cross-compiling with `CGO_ENABLED=0` no
    longer produces a working binary.

## 11. Where to look when stuck

1. `_examples/` in the module cache — closest thing to a cookbook.
2. Read the disgo source. It's small, flat, and heavily commented; jumping to
   definition usually answers the question faster than searching.
3. Discord's own API docs for semantics disgo passes through unchanged.
4. The disgo Discord server for anything version-specific.
