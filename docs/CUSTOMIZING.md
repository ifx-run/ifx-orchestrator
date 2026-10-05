English | [中文](./CUSTOMIZING.zh-CN.md)

# Customizing ifx-orchestrator

## Add a venue

Implement `hop.ExactInHop`:

1. `VenueID`, `Input() hop.Port`, `Output() hop.Port` (`Port.Native` = user-wallet lamports with Wrapped SOL mint)
2. `BuildBlueprint` → template ix with amount fields zeroed + `PatchSite` offsets

See `venue/raydiumcpmm`, `venue/raydiumammv4`, `venue/raydiumclmm`, `venue/raydiumlaunchpad`, `venue/byrealclmm`, `venue/pancakeswapclmm`, `venue/meteoradammv2`, `venue/meteoradlmm`, `venue/meteoradbc`, `venue/whirlpool`, `venue/lifinityv2`, `venue/pumpfun`, `venue/pumpamm`, `venue/jupiter`, `venue/titan`. Jupiter label hints: `venue/catalog.go`.

`jupiter`: wrap `POST /swap/v1/swap-instructions` **swapInstruction only** (`InstructionFromAPI`). Use `wrapAndUnwrapSol=false` and a user `destinationTokenAccount`; do not use `useTokenLedger`. Amount-in is patched like any other hop, so you can `.Hop(own).Hop(jup)` or `.Hop(jup).Hop(own)`. Native SOL ↔ WSOL between hops is compile (`SolIn` / `SolOut`); ATA create still Features. Watch account count / tx size (Jupiter remaining accounts are large).

`titan`: wrap the T1TAN `swap_route` / `swap_route_v2` instruction from a Titan quote (`SelectSwap`). Amount @8, min_out @16 (official CPI layout). Prefer `titanSwapVersion=3`, `outputWsol=true` when the next hop expects WSOL, and `transactionTemplate` so Titan sizes the route next to ifx ixs. Do not pass wrap/ATA ixs from V2 into the hop.

`pumpfun`: `NewBuyExactSolIn` / `NewSellExactIn` spend/credit **native SOL** (wallet lamports). Compile wraps to the next hop's WSOL ATA automatically (e.g. `.Hop(pumpSell).Hop(raydiumWSOL)`). Last-hop native proceeds stay in the wallet unless `.SolOut(hop.SolWSOL)` (optional `.WSOLAccount(ata)`). Reverse: WSOL hop then inner buy unwraps into the wallet. SPL quote v2 (`BuyExactQuoteInV2` / `SellV2`) is a normal ATA hop.

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
    Feature(solfunding.WrapAndUnwrap(wrapLamports, solfunding.UnwrapLamportsAll)). // optional prelude; hop-to-hop SOL/WSOL is compile
    AtaPolicy(feature.AtaCreateAndCloseCreated).
    SolIn(hop.SolNative).  // first hop WSOL: wrap AmountIn from wallet (omit if ATA already funded)
    SolOut(hop.SolWSOL).   // last hop native SOL → WSOL ATA (omit to leave lamports)
    Feature(hopconserve.New()). // per-hop output ≥ +1 and input debit ≥ amount_in
    Feature(arbcheck.Token(userATA, minProfit)). // first → AfterRoute asserts last
    Feature(mevtip.New(tipTo, tipLamports)).     // or ShareNative(tipTo, 500).WithWSOL(wsol)
    Feature(feehook.AtNode(1, feeTo).WithTokenBPS(50, feeATA)).
    HopWithMinOut(hop0, 0).             // per-hop min_out (0 allowed)
    HopWithMinOut(hop1, minOut1).
    // Hop(hop2) / MinAmountOut(final)  // no min, or last-hop fallback
    Build()
```

### FlashRent backends

- Default: `flashrent.Auto()` → Jupiter Flash Fill (`JUPLdTq…`), lends **one** TokenAccount rent.
- Custom: `flashrent.AutoWith(flashrent.CustomRentLiquidity{Borrow: ix, Repay: ix, MaxLamports: n})`
- If `peak - userLamports` exceeds backend `MaxLend()`, compile fails — supply a backend that can cover the peak.

### SolIn / SolOut (native SOL ↔ WSOL)

Compile adapts lanes between hops when both sides use the Wrapped SOL mint:

- native → WSOL ATA: transfer hop **delta** + `SyncNative` into the next hop's `UserInputATA`
- WSOL ATA → native: `UnwrapLamports(delta)` into the wallet (ATA stays open)
- WSOL → WSOL / native → native: forward the delta (same ATA)

`.SolIn(hop.SolNative)` wraps `AmountIn` into the first hop's WSOL ATA when that hop is not native. `.SolOut(hop.SolWSOL)` wraps the last hop's native proceeds (`.WSOLAccount` or derived ATA). Default `SolAsEmitted` leaves the venue CPI form as-is.

`solfunding.Wrap` remains a **fixed-lamports** BeforeRoute prelude; hop-to-hop conversion is compile.

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
- `hopconserve.New()` — each hop: output after ≥ before+1 (ATA or native lamports); input debit ≥ patched amount_in (`.SkipInput()` for output-only). Same in-out ATA skips output.
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

## Compiler principles (ifx)

Zero-cost ifx — see [design.md](./design.md#compiler-principles-zero-cost-ifx). Short form:

1. No runtime Frame need ⇒ **no ifx ix at all**, including reset. Constants go in templates, not `let`+patch. Hop count is not the test: a single hop plus bps fees / HopConserve **still needs ifx**.
2. Consecutive `IfxLet` merge into **one** ix (`Ctx.Let()` shared batch; flushed on `Emit`).
3. Public API stays short and hard to misuse: constant-only plans do not require a Frame (`New(nil, user)`); scratch is required only when a binding is needed.
