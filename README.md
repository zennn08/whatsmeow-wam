# whatsmeow-wam

WhatsApp Web **WAM** (Falco analytics/telemetry) for
[whatsmeow](https://github.com/tulir/whatsmeow) — a Go port of zapo-js's
[`@zapo-js/wam`](https://github.com/vinikjkkj/zapo/tree/main/packages/wam).

WhatsApp Web continuously uploads client-side telemetry batches over the
`w:stats` channel: message send/receive metrics, connection lifecycle, sync
progress, and a stream of UI interactions. A headless client (whatsmeow) that
uploads **none** of these has a conspicuous gap in its event profile. This
package closes that gap: it builds the exact same binary batches WA Web sends and
uploads them, deriving events from real client activity and fabricating plausible
ambient UI activity for the rest.

```
import wam "github.com/zennn08/whatsmeow-wam"
import "github.com/zennn08/whatsmeow-wam/wmtransport"
```

All three phases are implemented:

| Phase | Scope | Events |
| ----- | ----- | ------ |
| 1 | binary TLV encoder, per-channel batching, globals, `w:stats` upload | manual `Commit` |
| 2 | auto-emit protocol + integrator events from typed + raw stanzas | ~36 |
| 3 | synthetic UI telemetry (base stream + weighted ambient table) | 94 |

~130 distinct events — parity with zapo (which reports 131). The wire encoder is
**byte-identical** to zapo's, verified directly against the original TypeScript.

## Requirements: the forked whatsmeow

Phase 2 needs two small, additive patches that this repo's companion fork
([`github.com/zennn08/whatsmeow`](https://github.com/zennn08/whatsmeow)) already
carries:

- **`Client.RawNodeHandler`** (`rawnode.go`) — a hook fired for every raw binary
  node sent/received, giving the transport-level visibility WA Web has. Without
  it, only the ~9 typed-event lifecycle events are reachable.
- **exported `InfoQuery` / `IQSet` + `SendIQ`** (`request.go`) — so the uploader
  can send the `w:stats` IQ and wait for the server ack.

The fork keeps the upstream module path `go.mau.fi/whatsmeow`, so **your own
module** must point Go at the fork with a `replace`. Go ignores `replace`
directives from dependencies, so the one in this repo's `go.mod` does *not* apply
to you — you have to add it yourself, or the build fails on the missing
`RawNodeHandler` / `SendIQ`:

```
go get github.com/zennn08/whatsmeow-wam
go mod edit -replace go.mau.fi/whatsmeow=github.com/zennn08/whatsmeow@wam
go mod tidy
```

Pin to the exact commit instead of the `wam` branch for reproducible builds:
`...=github.com/zennn08/whatsmeow@c6079a2`. (This repo's own `go.mod` already
carries that pin so its tests build against the fork; consumers still need their
own replace as above.)

## Quick start

```go
import (
    "context"

    "go.mau.fi/whatsmeow"
    wam "github.com/zennn08/whatsmeow-wam"
    "github.com/zennn08/whatsmeow-wam/wmtransport"
)

// cli is a connected *whatsmeow.Client.
coord := wmtransport.NewCoordinator(cli, wam.Options{})   // Phase 1: uploader + globals
ae := wmtransport.AttachAutoEmitter(cli, coord)           // Phase 2: typed + raw-node handlers
ae.EmitAppStateActions = true                             // opt-in integrator actions
synth := ae.StartSynthetic(wmtransport.SyntheticOptions{}) // Phase 3: synthetic UI

// Content-level send events (ForwardSend, StickerSend, …) — call BEFORE sending:
ae.OnSendMessage(to, msg)
cli.SendMessage(ctx, to, msg)

// On shutdown: emits WebWamForceFlush and flushes remaining batches.
synth.Stop()
coord.Close(context.Background())
```

A full runnable bot (QR login, `ping`→`pong`, `--debug` upload logging) is in
[`example/`](example/main.go).

## How it works

Everything funnels into one method — `Coordinator.Commit(event, payload)` — and
flows out as the `w:stats` IQ. The event *sources* differ; the *pipeline* is shared.

```
  EVENT SOURCES                         CORE PIPELINE                       WIRE
  ─────────────                         ─────────────                       ────

  whatsmeow typed events ──┐
   Connected / Message /    │  AutoEmitter.Handle
   Receipt / HistorySync /  ├─► maps each to its WAM ──┐
   app-state / …            │   event + honest fields   │
                            │                           │
  cli.RawNodeHandler ───────┤  AutoEmitter.HandleRawNode│
   raw <message>/<iq>/      │   parses enc/ack/iq, ─────┤──► Coordinator.Commit(event, payload)
   <receipt>/<ack> in&out   │   tracks sends & IQs       │         │
                            │                            │    1. sampling gate  rand()*weight > 1 → drop
  your send ───────────────┤  ae.OnSendMessage ─────────┤    2. resolveEventFields (registry):
   OnSendMessage(to, msg)   │   parses the message proto │         enum key → numeric id,
                            │                            │         declaration order = wire order,
  SyntheticUI (3 timers) ───┘  fabricated ambient ───────┘         drop absent/untyped fields
   ambient / memory /           events                            3. append to per-channel WamBatch
   activity loops                                                     (big-endian TLV encoder)
                                                                          │
                                                                  flush on 5s / 50KB / Close
                                                                          ▼
                                                                  Transport.Upload(batch)
                                                                   <iq type="set" xmlns="w:stats">
                                                                     <add t="…">BATCH</add></iq>
                                                                          ▼
                                                        cli.DangerousInternals().SendIQ → WA server
                                                          (waits for ack; retries 5xx w/ backoff)
```

Step by step:

1. **Registry.** `registry.json` (embedded, extracted from `@vinikjkkj/wa-wam`:
   430 events, 879 enums, 46 globals) is the source of truth for every event's id,
   channel, sampling weight, and ordered fields with their wire types and enum
   tables.
2. **Commit.** `Coordinator.Commit(name, payload)` looks the event up, applies the
   same `rand()*weight > 1` sampling gate WA uses, resolves the payload against the
   registry (enum key strings → numeric ids, fields written in declaration order,
   unknown/absent fields dropped), and appends it to the open `WamBatch` for the
   event's channel.
3. **Encode.** `WamBatch` is WA Web's binary TLV format: a `WAM` header
   (version + stream id + sequence + channel), the batch globals (device
   identity), then each event as `commitTime` + header + fields. The encoder is
   byte-identical to zapo's — the same batch produces the same bytes.
4. **Flush.** A batch flushes on the 5s coalesce interval, when it reaches 50 KB,
   or on `Close`. Flushing is gated on `IsConnected`; a batch built while
   disconnected is dropped.
5. **Upload.** `Transport.Upload` wraps the bytes in the `<iq xmlns="w:stats">`
   stanza and sends it via the fork's `SendIQ`, waiting for the server ack.
   Transient 5xx errors retry with exponential backoff; a permanent failure drops
   the batch silently (telemetry is best-effort — it never surfaces to your app).

The three **AutoEmitter** inputs map onto steps 1–2 without ever fabricating a
field a real client couldn't produce; **SyntheticUI** fabricates events, but each
replicates only the field subset WA Web itself sets.

## Coverage

`AttachAutoEmitter` reproduces zapo's full auto-emitter (~36 events) from four
sources. Every auto-emitted field is derived honestly from real activity — a
wrong field is a worse fingerprint than a missing one.

- **Typed lifecycle** (automatic): `WebcSocketConnect`, `WebcStreamModeChange`,
  `WebcPageResume`, `WebcRawPlatforms`, `MessageReceive`, `E2eMessageRecv`,
  `ReceiptStanzaReceive`, `GroupJoinC`, `MdBootstrapHistoryDataReceived`
  (`WebWamForceFlush` from `Close`).
- **Raw-stanza** (automatic, needs the fork's `RawNodeHandler`): `E2eMessageSend`,
  `WebcMessageSend`, `MessageSend`, `EditMessageSend`, `RevokeMessageSend`,
  `MessageDeleteActions`, `SendRevokeMessage`, `WaOldCode`, `ClockSkewDifferenceT`,
  `MessageHighRetryCount`, `UnknownStanza`, `OfflineCountTooHigh`,
  `WaFsGroupJoinRequestAction`, `GroupCreate`, `GroupCreateC`,
  `EphemeralSettingChange`, `DisappearingModeSettingChange` (and the
  `e2eCiphertextType`/`Version` fields on `E2eMessageRecv`).
- **Outbound content** (`ae.OnSendMessage`, you call it): `ForwardSend`,
  `ReactionActions`, `PollsActions`, `SendDocument`, `StickerSend`,
  `PinInChatMessageSend`.
- **Integrator actions** (`ae.EmitAppStateActions`, opt-in): `ChatMute`,
  `ChatAction`, `StatusMute`, `MdSyncdDogfoodingFeatureUsage`.

**Synthetic UI** (`ae.StartSynthetic`, opt-in) adds the full 94-event fabricated
set: a base stream anchored to real messages / the ambient loop (`UiAction`,
`WebcChatOpen`, `MemoryStat`, `UserActivity`/`TsBitArray`, `AttachmentTrayActions`,
`MediaPicker`, `MessageContextMenuActions`, `GifSearch*`, …) plus a 67-entry
weighted ambient table (settings, status, privacy, search, wallpaper, and gated
community/channel/business surfaces). Jittered, rate-limited, and confined to
optional active hours; a test validates every enum key resolves against the
registry.

### `OnSendMessage`: call it *before* `SendMessage`

Content events fire optimistically at send time — WA Web / zapo log the *user
action* the moment you send, not after the ack. The *send outcome* (`MessageSend`
`OK`) is a separate event that fires automatically only on the server ack, so you
never gate `OnSendMessage` on success. If `SendMessage` fails very early the
action event was still emitted; WA Web behaves the same.

## `wam.Options`

All optional (upload needs a `Transport`, wired for you by `NewCoordinator`):

| Option | Default | Description |
| ------ | ------- | ----------- |
| `Globals` | derived from client | OS / browser / app version stamped in every batch |
| `Transport` | whatsmeow socket | Uploads finalized batches; nil builds-and-drops |
| `IsConnected` | `cli.IsConnected` | Gates uploads; a batch built while disconnected is dropped |
| `FlushInterval` | 5s | Coalesce window before a non-empty batch flushes |
| `MaxBufferSize` | 50000 | Byte size that forces an immediate flush |
| `DisableSampling` | `false` | Skip the `rand()*weight > 1` gate (keep every event) |
| `Logf` | none | Best-effort debug lines (drops / failures) |

`SyntheticOptions` tunes fabrication rates, `ActiveHoursStartHour`/`EndHour`, and
the `Channels` / `Communities` / `Business` capability gates.

Globals default from the device identity (`store.DeviceProps.GetOs()`,
`store.GetWAVersion()`); `Browser` defaults to `"Chrome"`. **Keep globals
consistent with your pairing `ClientPayload`** — an inconsistent global is a worse
fingerprint than none.

## Deviations from strict 1:1

- **`OnSendMessage` is manual** — whatsmeow has no outbound-message event; the six
  content events only fire when you call it. The stanza-level send events are
  automatic via the raw-node hook.
- **Integrator actions are opt-in and approximate** — whatsmeow emits app-state
  events on *sync* (from any device), not on this client's own action, so timing
  differs and multi-device accounts can over-report. Full-sync events are skipped;
  `ClearChat` reports the keep-starred variant (whatsmeow drops the starred flag).
- **`UnknownStanza`** fires for nodes whatsmeow doesn't route — close to, not
  identical to, zapo's dispatcher-unhandled set.

## Testing

```
go test ./...
```

Notable tests: byte-identical wire vectors (`wire_test.go`), Go/TS parity on enum
resolution & field order (`crosscheck_test.go`), the raw-node → event mappings,
and `fabrications_test.go` which runs every one of the 67 ambient fabs many times
and asserts no enum field is silently dropped.

## Regenerating the registry

`registry.json` is extracted from `@vinikjkkj/wa-wam` (fields kept in **declaration
order** — that order is the wire order). `npm pack @vinikjkkj/wa-wam` and dump
`WA_WAM_EVENTS` / `WA_WAM_ENUMS` / `WA_WAM_GLOBALS` plus `WA_WAM_WIRE_FORMAT`,
`WA_WAM_PROTOCOL_VERSION`, `WA_WAM_CHANNEL_WIRE_CODES`, and
`WA_WAM_BUFFER_CONSTANTS`.

## Disclaimer

whatsmeow is an unofficial client, and mimicking WA Web's telemetry does not make
it undetectable — WhatsApp can still identify a non-browser client through its
`ClientPayload`, device props, and protocol behaviour. WAM closes one gap (the
missing telemetry stream); it is best-effort anti-fingerprinting with the
documented approximations above, not a guarantee. Using an unofficial client
carries account-ban risk regardless. Use responsibly.
