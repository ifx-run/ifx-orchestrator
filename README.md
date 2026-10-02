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

## Status (Phase 0–4)

- Framework: `AmountFlow`, `ExactInHop`, `compile`, `orchestrator`
- Venues: `raydiumcpmm` / `raydiumammv4` / `raydiumclmm` / `raydiumlaunchpad`, `meteoradammv2` / `meteoradlmm` / `meteoradbc`, `whirlpool`, `pumpfun` (bonding + v2), `pumpamm`, `mock`
- Features: `AtaPolicy`, `rentpeak`, `flashrent`, `gassponsored` (SOL/WSOL/Token repay), `solfunding` (wrap + `UnwrapLamports`), `mevtip`, `feehook` (incl. mid-graph `AtNode`)

Path discovery via Jupiter **quote only**; txs are built with our `ExactInHop` venues (`examples/jupiter_simulate`).

```bash
go test ./...
go run ./examples/mock_two_hop/
go run ./examples/two_venue_path/
RPC_URL=https://solana-rpc.publicnode.com go run ./examples/simulate_mainnet/
```

```go
plan, err := orchestrator.New(scratch, user).
    AmountIn(1_000_000).
    UserLamports(userSOL).
    Feature(flashrent.Auto()).
    Feature(gassponsored.FromNative(sponsor, fixedCost, 12_000).WithWSOL(userWSOL)).
    Feature(solfunding.WrapAndUnwrap(wrap, solfunding.UnwrapLamportsAll)).
    Feature(feehook.AtNode(1, feeTo).WithTokenBPS(50, feeATA)).
    AtaPolicy(feature.AtaCreateAndCloseCreated).
    Hop(raydiumcpmm.NewExactIn(...)).
    Build()
```

See [docs/CUSTOMIZING.md](./docs/CUSTOMIZING.md). Full design: [docs/design.md](./docs/design.md).
