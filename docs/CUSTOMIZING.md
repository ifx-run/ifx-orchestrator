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

Typical rent-peak + sponsor + SOL funding stack:

```go
orchestrator.New(scratch, user).
    AmountIn(amount).
    UserLamports(userSOL).
    Feature(flashrent.Auto()).
    Feature(gassponsored.New(sponsor, fixedCost)). // or .WithWSOL(userWSOLATA)
    Feature(solfunding.WrapAndUnwrap(wrapLamports, solfunding.UnwrapLamportsAll)).
    AtaPolicy(feature.AtaCreateAndCloseCreated).
    Feature(mevtip.New(tipTo, tipLamports)).
    Feature(feehook.AtNode(1, feeTo).WithTokenBPS(50, feeATA)).
    Hop(...).
    Build()
```

### FlashRent backends

- Default: `flashrent.Auto()` → Jupiter Flash Fill (`JUPLdTq…`), lends **one** TokenAccount rent.
- Custom: `flashrent.AutoWith(flashrent.CustomRentLiquidity{Borrow: ix, Repay: ix, MaxLamports: n})`
- If `peak - userLamports` exceeds backend `MaxLend()`, compile fails — supply a backend that can cover the peak.

### SolFunding (WSOL wrap / unwrap)

- `solfunding.Wrap(lamports)` — create ATA + transfer + `SyncNative` in BeforeRoute
- `solfunding.UnwrapWSOL(mode)` / `WrapAndUnwrap` — AfterRoute unwrap
- **Unwrap modes** (prefer `UnwrapLamports*` over Close when the ATA should stay open):
  - `UnwrapPartial` — `UnwrapLamports(amount)` (Token ix 45)
  - `UnwrapLamportsAll` — `UnwrapLamports(all)` keep ATA + rent
  - `UnwrapClose` — `CloseAccount` reclaim rent

### GasSponsored + RepayMode

Baselines in `BeforeRoute`, asserts proceed ≥ settle in `AfterRoute`, then repays:

| Mode | Behavior |
|------|----------|
| `InterceptSOL` (default) | patched System transfer user → sponsor |
| `InterceptWSOL` | `SyncNative` + patched `UnwrapLamports` from user WSOL ATA **directly to sponsor** (ATA stays open) |
| `TokenTransfer` | patched SPL transfer user ATA → sponsor ATA (`FixedCost` in token raw units) |

`.WithWSOL(ata)` / `.WithToken(userATA, sponsorATA)`. Optional `.WithATARent(ata)` only with `InterceptSOL`.

### MevTip / FeeHook

- `mevtip.New(receiver, lamports)` — literal System transfer in `AfterRoute`
- `feehook.Fixed` / `ProceedsBPS` — end-of-route SOL fee
- `feehook.AtNode(node, recipient).WithFixed(...).WithTokenBPS(bps, recipientATA)` — Exact **fee_node_index**: charge once after that node's in-edges settle (Fixed in AfterEdge; TokenBps via MapForwardAmount so the next hop sees net)

**Custom fee program** (instead of System/Token transfer):

```go
template := solana.NewInstruction(feeProgram, accounts, data) // amount field zeroed
feehook.ProceedsBPS(treasury, 50).
    WithSolSettler(feehook.CustomIx(template, amountOffset))

// or full control:
feehook.Fixed(treasury, 1_000).WithSolSettler(feehook.SettlerFunc{
    Patched: func(cx *feature.Ctx, amount typed.ScratchValue) error {
        return feature.EmitRawPatchedCPI(cx, template, feature.RawCpiU64Patch(8, amount))
    },
})
```

Default settlers: `SystemTransferSettler` (SOL) / `TokenTransferSettler` (TokenBps). Override with `.WithSolSettler` / `.WithTokenSettler`.

### AtaPolicy

| Policy | BeforeRoute | AfterRoute |
|--------|-------------|------------|
| `UseOnly` | — | — |
| `UseAndClose` | — | close intermediate ATAs |
| `CreateAndCloseCreated` | create missing ATAs | close ATAs created this tx (keep destination) |
| `CreateAndCloseAll` | create missing | close created + pre-existing |
| `CreateOnly` | create missing | — |

Mark `RouteNode.Exists = true` when the ATA is already on-chain so peak / create stay accurate.
