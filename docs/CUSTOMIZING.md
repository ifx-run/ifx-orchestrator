English | [中文](./CUSTOMIZING.zh-CN.md)

# Customizing ifx-orchestrator

## Add a venue

Implement `hop.ExactInHop`:

1. `VenueID`, `InputMint`, `OutputMint`, `OutputMeasureAccount`
2. `BuildBlueprint` → template ix with amount fields zeroed + `PatchSite` offsets

See `venue/raydiumcpmm`, `venue/raydiumammv4`, `venue/raydiumclmm`, `venue/raydiumlaunchpad`, `venue/byrealclmm`, `venue/pancakeswapclmm`, `venue/meteoradammv2`, `venue/meteoradlmm`, `venue/meteoradbc`, `venue/whirlpool`, `venue/pumpfun`, `venue/pumpamm`, `venue/jupiter`, `venue/titan`. Jupiter label hints: `venue/catalog.go`.

`jupiter`: wrap `POST /swap/v1/swap-instructions` **swapInstruction only** (`InstructionFromAPI`). Use `wrapAndUnwrapSol=false` and a user `destinationTokenAccount`; do not use `useTokenLedger`. Amount-in is patched like any other hop, so you can `.Hop(own).Hop(jup)` or `.Hop(jup).Hop(own)`. Setup/cleanup/compute-budget stay in Features. Watch account count / tx size (Jupiter remaining accounts are large).

`titan`: wrap the T1TAN `swap_route` / `swap_route_v2` instruction from a Titan quote (`SelectSwap`). Amount @8, min_out @16 (official CPI layout). Prefer `titanSwapVersion=3`, `outputWsol=true` when the next hop expects WSOL, and `transactionTemplate` so Titan sizes the route next to ifx ixs. Do not pass wrap/ATA ixs from V2 into the hop — Features own those.

`pumpfun`: `NewBuyExactSolIn` / `NewSellExactIn` (native SOL); plus `BuyExactQuoteInV2` / `SellV2` (SPL quote). Native SOL sell is a **terminal** hop.

CLMM venues (`raydiumclmm`, `byrealclmm`, `pancakeswapclmm`, `whirlpool`, `meteoradlmm`): pass tick/bin remaining accounts in `Params` (no RPC discovery in the framework).

## Features

Register with `.Feature(...)`. **BeforeRoute** runs in registration order; **AfterRoute** runs in **reverse** (sandwich).

Typical rent-peak + sponsor + SOL funding stack:

```go
orchestrator.New(scratch, user).
    AmountIn(amount).
    UserLamports(userSOL).
    Feature(flashrent.Auto()).
    Feature(gassponsored.New(sponsor, fixedCost)). // or FromNative(..., 12000).WithWSOL(...) / FromToken(...)
    Feature(solfunding.WrapAndUnwrap(wrapLamports, solfunding.UnwrapLamportsAll)).
    AtaPolicy(feature.AtaCreateAndCloseCreated).
    Feature(arbcheck.Token(userATA, minProfit)). // first → AfterRoute asserts last
    Feature(mevtip.New(tipTo, tipLamports)).     // or ShareNative(tipTo, 500).WithWSOL(wsol)
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

### GasSponsored

Two default modes:

**1. FromNative** — repay in SOL from mid-route SOL / WSOL proceeds:

```go
gassponsored.FromNative(feePayer, estimatedCostLamports, 12_000).
    WithRepayTo(treasury). // when collection ≠ fee payer
    WithWSOL(userWSOLATA).
    WithATARent(createdATA)
```

`settle = ceil((EstimatedCost + ataRent) * ProtectionBps / 10000)`. Assert `SOL_delta + WSOL_delta ≥ settle`, then take WSOL first (via `UnwrapLamports` → **RepayTo**), remainder from native SOL.

**2. FromToken** — fixed token amount at construction (no gas math):

```go
gassponsored.FromToken(userATA, repayTokenATA, amountRaw).
    WithSponsor(feePayer) // optional when fee payer ≠ token treasury
```

Asserts token proceeds ≥ amount, then patched SPL transfer.

`New(sponsor, cost)` ≡ `FromNative(sponsor, cost, 10000)`.

### ArbCheck / MevTip / FeeHook

Atomic cycle (e.g. two Jupiter legs A→B→A): you still need a **real** off-chain edge (different `dexes`, Titan vs Jupiter, or an own venue). Same-aggregator A→B→A quotes are usually negative EV.

```go
.Feature(arbcheck.Token(userA, minProfit)). // or Native(minLamports).WithWSOL(wsol)
.Feature(mevtip.New(jitoTip, bribe)).       // inclusion; reverts with arbcheck
.Hop(jupAB).Hop(jupBA)
```

- `arbcheck.Token(ata, minProfit)` — `after ≥ before + minProfit` (0 = break-even)
- `arbcheck.Native(minProfit).WithWSOL(wsolATA)` — same for lamports + WSOL
- Register **arbcheck first** so AfterRoute (reversed) asserts after tips/fees
- `mevtip.New(receiver, lamports)` — literal System transfer in `AfterRoute`
- `mevtip.ShareNative(receiver, bps).WithMax(cap).WithWSOL(wsol)` — tip floor(native profit × bps / 10000); loss ⇒ 0 tip (does not revert). Caps at current native lamports. Use `arbcheck` for a hard floor.
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
