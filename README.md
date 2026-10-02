# ifx-orchestrator

English | [中文](./README.zh-CN.md)

Solana **execution orchestrator / venue hub**: compile externally supplied hops into executable **ifx** transactions.

Not a Jupiter-style best-path aggregator, and not a new on-chain router program. Versus Jupiter’s on-chain router: **batteries-included** (fluent `orchestrator.Builder`) **and highly customizable** (pluggable `ExactInHop` / `Feature` / Flash backends).

| Layer | Role |
|---|---|
| Venue (`ExactInHop`) | Account layout, ix templates, ExactIn patches |
| Feature | ATA / FlashRent / GasSponsored / Fee / Tip … |
| Compiler | Edge loop on the route graph → Frame + ifx instructions |
| Orchestrator | Batteries-included builder facade |

**First language: Go** (`ifx/go-sdk`); Rust / TS mirrors later.

## Status (Phase 0–2)

- Framework: `AmountFlow`, `ExactInHop`, `compile`, `orchestrator`
- Venues: `raydiumcpmm`, `meteoradammv2`, `mock`
- Features: `AtaPolicy`, `rentpeak.EstimateForRoute`, `flashrent` (default Jupiter Flash Fill, pluggable backend)

```bash
go test ./...
go run ./examples/mock_two_hop/
go run ./examples/two_venue_path/
```

```go
plan, err := orchestrator.New(scratch, user).
    AmountIn(1_000_000).
    UserLamports(userSOL).
    Feature(flashrent.Auto()).
    AtaPolicy(feature.AtaCreateAndCloseCreated).
    Hop(raydiumcpmm.NewExactIn(...)).
    Hop(meteoradammv2.NewExactIn(...)).
    Build()
```

See [docs/CUSTOMIZING.md](./docs/CUSTOMIZING.md). Full design: [docs/design.md](./docs/design.md).
