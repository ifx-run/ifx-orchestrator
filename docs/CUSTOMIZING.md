English | [中文](./CUSTOMIZING.zh-CN.md)

# Customizing ifx-orchestrator

## Add a venue

Implement `hop.ExactInHop`:

1. `VenueID`, `InputMint`, `OutputMint`, `OutputMeasureAccount`
2. `BuildBlueprint` → template ix with amount fields zeroed + `PatchSite` offsets

See `venue/raydiumcpmm` and `venue/meteoradammv2` for minimal adapters.

## Features

Register with `.Feature(...)`. **BeforeRoute** runs in registration order; **AfterRoute** runs in **reverse** (sandwich).

Typical rent-peak stack:

```go
orchestrator.New(scratch, user).
    AmountIn(amount).
    UserLamports(userSOL).
    Feature(flashrent.Auto()).                 // borrow first
    AtaPolicy(feature.AtaCreateAndCloseCreated). // then create ATAs
    Hop(...).
    Build()
// AfterRoute: close ATAs, then repay
```

### FlashRent backends

- Default: `flashrent.Auto()` → Jupiter Flash Fill (`JUPLdTq…`), lends **one** TokenAccount rent.
- Custom: `flashrent.AutoWith(flashrent.CustomRentLiquidity{Borrow: ix, Repay: ix, MaxLamports: n})`
- If `peak - userLamports` exceeds backend `MaxLend()`, compile fails — supply a backend that can cover the peak.

### AtaPolicy

| Policy | BeforeRoute | AfterRoute |
|--------|-------------|------------|
| `UseOnly` | — | — |
| `UseAndClose` | — | close intermediate ATAs |
| `CreateAndCloseCreated` | create missing ATAs | close ATAs created this tx (keep destination) |
| `CreateAndCloseAll` | create missing | close created + pre-existing |
| `CreateOnly` | create missing | — |

Mark `RouteNode.Exists = true` when the ATA is already on-chain so peak / create stay accurate.
