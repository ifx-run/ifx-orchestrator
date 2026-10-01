# ifx-orchestrator

English | [中文](./README.zh-CN.md)

Solana **execution orchestrator / venue hub**: compile externally supplied hops into executable **ifx** transactions.

Not a Jupiter-style best-path aggregator, and not a new on-chain router program. Versus Jupiter’s on-chain router: **batteries-included** (fluent `Router`) **and highly customizable** (pluggable `ExactInHop` / `Feature` / Flash backends).

| Layer | Role |
|---|---|
| Venue (`ExactInHop`) | Account layout, ix templates, ExactIn patches |
| Feature | ATA / FlashRent / GasSponsored / Fee / Tip … |
| Compiler | Edge loop on the route graph → Frame + ifx instructions |
| Router | Batteries-included builder facade |

**First language: Go** (`ifx/go-sdk`); Rust / TS mirrors later.

## Status (Phase 0)

Core framework is in place: `AmountFlow`, `ExactInHop`, `compile`, `router`, `venue/mock`.

```bash
go test ./...
go run ./examples/mock_two_hop/
```

```go
plan, err := router.New(scratch, user).
    AmountIn(1_000_000).
    MinAmountOut(900_000).
    Hop(mock.New(...)).
    Hop(mock.New(...)).
    Build()
```

Full design: [docs/design.md](./docs/design.md).
