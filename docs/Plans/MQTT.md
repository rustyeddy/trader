# MQTT Architecture

This document captures the design for distributing broker messages to multiple
trader instances via MQTT. The goal is to support several traders running across
multiple machines, passive consumers (journal, logger, UI) that cannot interfere
with the money loop, and the ability to swap brokers without touching trader
instances.

---

## Overview

```
┌─────────────────────────────────────────────────────────┐
│                    MQTT Broker (Mosquitto)               │
└──────┬─────────────┬──────────────┬──────────┬──────────┘
       │             │              │          │
  [Gateway]   [Trader A]      [Trader B]   [Passive Consumers]
  OANDA/IB    EUR_USD          GBP_USD      journal / logger / UI
  adapter     Pi-1             Pi-2         (subscribe only)
```

The gateway is the **only** process that talks to the broker API. Everything
else is MQTT. Swapping OANDA for IB means replacing the gateway binary —
trader instances, journal, and UI are untouched.

---

## Topic Schema

```
trader/{account_id}/tick/{instrument}       # price bar/tick   broker→all
trader/{account_id}/fill/{instrument}       # confirmed fill    broker→all
trader/{account_id}/position/{instrument}   # position state    broker→all  (retained)
trader/{account_id}/equity                  # equity snapshot   broker→all  (retained)
trader/{account_id}/heartbeat               # gateway alive     broker→all
trader/{account_id}/error                   # broker errors     broker→all
trader/{account_id}/order                   # order request     trader→gateway
```

**Retained topics** (`position`, `equity`): a trader or UI that connects
mid-session immediately gets current state without waiting for the next update.

### QoS Levels

| Topic | QoS | Reason |
|---|---|---|
| `tick` | 0 | Stale price is dangerous; drop and use next |
| `order` | 1 | Must reach gateway at least once |
| `fill` | 1 | Must reach journal; broker holds until ACK'd |
| `position`, `equity` | 1 + retained | Durable state, new subscribers get current state immediately |
| `heartbeat`, `error` | 0 | Best-effort |

---

## Message Payloads (JSON)

**tick**
```json
{ "ts": 1748000000, "account": "001", "instrument": "EUR_USD",
  "bid": 108500, "ask": 108520, "spread": 20 }
```

**order** (trader → gateway)
```json
{ "ts": 1748000001, "account": "001", "instrument": "EUR_USD",
  "side": "long", "units": 10000, "stop": 108300, "take": 108800,
  "order_id": "01J..." }
```

**fill** (gateway → all)
```json
{ "ts": 1748000002, "account": "001", "instrument": "EUR_USD",
  "order_id": "01J...", "side": "long", "units": 10000,
  "price": 108522, "stop": 108300, "take": 108800 }
```

**position** (retained, updated after every fill or close)
```json
{ "ts": 1748000002, "account": "001", "instrument": "EUR_USD",
  "units": 10000, "entry": 108522, "stop": 108300, "take": 108800,
  "unrealized_pl": 0 }
```

**equity** (retained, updated each tick)
```json
{ "ts": 1748000005, "account": "001",
  "balance": 10000000000, "equity": 10000120000,
  "margin_used": 108522, "free_margin": 9891478 }
```

---

## Gateway Design

```
cmd/gateway/
  cmd_run.go     — cobra command, wires MQTT client + broker adapter
  gateway.go     — event loop: broker stream → MQTT publish, order sub → broker
  adapter.go     — BrokerAdapter interface
```

### BrokerAdapter Interface

```go
type BrokerAdapter interface {
    GetAccounts(ctx context.Context) ([]Account, error)
    GetPositions(ctx context.Context, accountID string) ([]Position, error)
    PlaceOrder(ctx context.Context, accountID string, req OrderRequest) (Fill, error)
    CloseTrade(ctx context.Context, accountID string, tradeID string, units int64) error
    StreamPrices(ctx context.Context, accountID string, instruments []string) (<-chan Tick, error)
    StreamEvents(ctx context.Context, accountID string) (<-chan BrokerEvent, error)
}
```

`OANDAAdapter` wraps `oanda.Client` and implements `BrokerAdapter`. `IBAdapter`
replaces it with no other changes to the codebase.

### Gateway Event Loop

1. On startup: call `GetPositions`, publish retained `position` messages for any
   open trades (handles restarts cleanly).
2. Run `StreamPrices` and `StreamEvents` concurrently.
3. Each tick → publish `tick`, recompute equity → publish `equity` (retained).
4. Each broker event (fill, close) → publish `fill` + update retained `position`.
5. Subscribe to `trader/{account_id}/order`, forward each message to `PlaceOrder`.

### Config

```yaml
gateway:
  broker: oanda          # or "ib", "alpaca"
  account: "001"
  mqtt: "tcp://pi3.local:1883"
```

---

## Passive Consumers Don't Interfere

Journal and logger subscribe to `fill` and `equity` with QoS 1 using MQTT
**shared subscriptions**:

```
$share/journal/trader/+/fill/+
$share/journal/trader/+/equity
```

- Mosquitto holds `fill` messages until the journal ACKs them — the trading
  loop never waits.
- If two journal instances run, each fill goes to exactly one of them (no
  duplicates).
- A crashed journal catches up on reconnect (QoS 1 + persistent session).
- The money loop (trader → order → fill) is unaffected whether journal is up
  or not.

The UI uses QoS 0 for ticks (fine to drop) and QoS 1 for fills/positions.

---

## Service Layer Refactor Required

The current `Service` struct has `OANDA *oanda.Client` baked in. For the
gateway to be broker-agnostic, this must become a `BrokerAdapter` interface.

```
Current:   Service → oanda.Client → OANDA API
With MQTT: Service → BrokerAdapter interface
                       ↑
            Gateway wires in OANDAAdapter or IBAdapter
```

### Layer Responsibilities

```
Gateway process                 Trader process
───────────────                 ──────────────
BrokerAdapter (OANDA/IB)        Strategy
    ↕                               ↕
Service.PlaceOrder          MQTTBroker (implements trader.Broker)
Service.GetPositions                ↕
    ↕                           MQTT
  MQTT ──────────────────────────────
    ↕
Journal / Logger / UI (passive subscribers)
```

The gateway uses the order and account methods from `Service` (`PlaceOrder`,
`GetPositions`, `CloseTrade`). Trader instances use `RunLiveStrategy` but with
`MQTTBroker` instead of `oanda.Client`. The journal subscribes to MQTT fills
independently and never calls service methods.

### Service Struct After Refactor

```go
type Service struct {
    Broker    BrokerAdapter   // was: OANDA *oanda.Client
    Log       *slog.Logger
    AccountID string
}
```

All existing service methods compile against the interface unchanged. The
refactor is self-contained and does not touch strategies, journal, or the
trading loop.

---

## Open Questions

1. **Mosquitto location** — one of the Pis, or a cloud broker (HiveMQ free
   tier)? Affects the HA story.
2. **Auth** — TLS + username/password per trader instance, or mTLS? Matters
   if the broker is reachable from the internet.
3. **Gateway HA** — two gateway instances with leader election, or accept that
   one crash means no trading until restart?
4. **Netting enforcement** — does the gateway reject a `long` order when a
   `short` position already exists on the same instrument, or does the trader
   own that logic?
