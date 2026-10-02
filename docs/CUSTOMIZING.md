English | [中文](./CUSTOMIZING.zh-CN.md)

# Customizing ifx-orchestrator

## Add a venue

Implement `hop.ExactInHop`:

1. `VenueID`, `InputMint`, `OutputMint`, `OutputMeasureAccount`
2. `BuildBlueprint` → template ix with amount fields zeroed + `PatchSite` offsets

See `venue/raydiumcpmm`, `venue/meteoradammv2`, and `venue/pumpfun` for minimal adapters.

`pumpfun`: `NewBuyExactSolIn` / `NewSellExactIn` (native SOL). Sell pays SOL to the wallet — use as a **terminal** hop (chaining via `SplTokenAmount` will not capture SOL proceeds).

## Features

Register with `.Feature(...)`. **BeforeRoute** runs in registration order; **AfterRoute** runs in **reverse** (sandwich).

Typical rent-peak + sponsor stack:

```go
orchestrator.New(scratch, user).
    AmountIn(amount).
    UserLamports(userSOL).
    Feature(flashrent.Auto()).                   // borrow first
    Feature(gassponsored.New(sponsor, fixedCost)). // baseline user SOL
    AtaPolicy(feature.AtaCreateAndCloseCreated). // then create ATAs
    Feature(mevtip.New(tipTo, tipLamports)).
    Feature(feehook.ProceedsBPS(feeTo, 50)).
    Hop(...).
    Build()
// AfterRoute (reverse): fee → tip → close ATAs → sponsor repay → flash repay
```

### FlashRent backends

- Default: `flashrent.Auto()` → Jupiter Flash Fill (`JUPLdTq…`), lends **one** TokenAccount rent.
- Custom: `flashrent.AutoWith(flashrent.CustomRentLiquidity{Borrow: ix, Repay: ix, MaxLamports: n})`
- If `peak - userLamports` exceeds backend `MaxLend()`, compile fails — supply a backend that can cover the peak.

### GasSponsored

Baselines user lamports in `BeforeRoute`, then in `AfterRoute`: assert `Δuser ≥ FixedCost (+ optional ATA rent)` and patch a System transfer to `Sponsor`. Optional `.WithATARent(ata)` measures sponsor-paid ATA rent between baseline and first edge (register **before** `Ata`).

Orthogonal to FlashRent: flash = peak liquidity; sponsor = fee-payer front + repay from trade SOL proceeds.

### MevTip / FeeHook

- `mevtip.New(receiver, lamports)` — literal System transfer in `AfterRoute`
- `feehook.Fixed(receiver, lamports)` — literal fee
- `feehook.ProceedsBPS(receiver, bps)` — `floor(bps * SOL_delta / 10000)` patched transfer after route

### AtaPolicy

| Policy | BeforeRoute | AfterRoute |
|--------|-------------|------------|
| `UseOnly` | — | — |
| `UseAndClose` | — | close intermediate ATAs |
| `CreateAndCloseCreated` | create missing ATAs | close ATAs created this tx (keep destination) |
| `CreateAndCloseAll` | create missing | close created + pre-existing |
| `CreateOnly` | create missing | — |

Mark `RouteNode.Exists = true` when the ATA is already on-chain so peak / create stay accurate.
