[English](./CUSTOMIZING.md) | 中文

# 定制 ifx-orchestrator

## 接入 venue

实现 `hop.ExactInHop`：

1. `VenueID`、`Input() hop.Port`、`Output() hop.Port`（`Port.Native` = 用户钱包 lamports，mint 为 Wrapped SOL）
2. `BuildBlueprint` → amount 置 0 的模板 ix + `PatchSite` 偏移

参考 `venue/raydiumcpmm`、`venue/raydiumammv4`、`venue/raydiumclmm`、`venue/raydiumlaunchpad`、`venue/byrealclmm`、`venue/pancakeswapclmm`、`venue/meteoradammv2`、`venue/meteoradlmm`、`venue/meteoradbc`、`venue/whirlpool`、`venue/lifinityv2`、`venue/pumpfun`、`venue/pumpamm`、`venue/jupiter`、`venue/titan`。Jupiter 标签 hint 见 `venue/catalog.go`。

`jupiter`：只包 `POST /swap/v1/swap-instructions` 返回的 **swapInstruction**（`InstructionFromAPI`）。请设 `wrapAndUnwrapSol=false` 并指定用户 `destinationTokenAccount`；不要开 `useTokenLedger`。amount-in 与其它 hop 一样会被 patch，因此可以 `.Hop(自家).Hop(jup)` 或 `.Hop(jup).Hop(自家)`。跳与跳之间的 native SOL ↔ WSOL 由 compile（`SolIn` / `SolOut`）处理；ATA create 仍走 Feature。注意账户数量 / 交易体积（Jupiter remaining accounts 很大）。

`titan`：包 Titan quote 里的 T1TAN `swap_route` / `swap_route_v2`（`SelectSwap`）。amount @8、min_out @16（官方 CPI 布局）。建议 `titanSwapVersion=3`，后接 hop 需要 WSOL 时设 `outputWsol=true`，并用 `transactionTemplate` 让 Titan 按 ifx ix 体积选路。不要把 V2 的 wrap/ATA ix 塞进 hop。

`pumpfun`：`NewBuyExactSolIn` / `NewSellExactIn` 吃/吐 **原生 SOL**（钱包 lamports）。下一跳若要 WSOL ATA，compile 会自动 wrap（例如 `.Hop(内盘卖).Hop(Raydium WSOL)`）。最后一跳默认把 SOL 留在钱包；要 WSOL 用 `.SolOut(hop.SolWSOL)`（可选 `.WSOLAccount(ata)`）。反向：WSOL hop 再接内盘买会 unwrap 进钱包。SPL quote v2（`BuyExactQuoteInV2` / `SellV2`）是普通 ATA hop。

CLMM 类（`raydiumclmm` / `byrealclmm` / `pancakeswapclmm` / `whirlpool` / `meteoradlmm`）：tick/bin remaining accounts 由调用方传入 `Params`（框架不做 RPC 发现）。

## Feature

用 `.Feature(...)` 注册。**BeforeRoute** 按注册顺序执行；**AfterRoute** **逆序**（夹道）。

典型租金峰值 + 赞助 + SOL funding：

```go
orchestrator.New(scratch, user).
    AmountIn(amount).
    UserLamports(userSOL).
    Feature(flashrent.Auto()).
    Feature(gassponsored.New(sponsor, fixedCost)). // 或 FromNative(..., 12000).WithWSOL(...) / FromToken(...)
    Feature(solfunding.WrapAndUnwrap(wrapLamports, solfunding.UnwrapLamportsAll)). // 可选 prelude；跳间 SOL/WSOL 走 compile
    AtaPolicy(feature.AtaCreateAndCloseCreated).
    SolIn(hop.SolNative).  // 第一跳要 WSOL：从钱包 wrap AmountIn（ATA 已有余额则可省略）
    SolOut(hop.SolWSOL).   // 最后一跳 native SOL → WSOL ATA（省略则留在钱包）
    Feature(hopconserve.New()). // 每跳输出 ≥ +1，输入扣减 ≥ amount_in
    Feature(arbcheck.Token(userATA, minProfit)). // 先注册 → AfterRoute 最后断言
    Feature(mevtip.New(tipTo, tipLamports)).     // 或 ShareNative(tipTo, 500).WithWSOL(wsol)
    Feature(feehook.AtNode(1, feeTo).WithTokenBPS(50, feeATA)).
    Hop(...).
    Build()
```

### FlashRent 后端

- 默认：`flashrent.Auto()` → Jupiter Flash Fill，只借 **一份** TokenAccount rent
- 自定义：`flashrent.AutoWith(flashrent.CustomRentLiquidity{...})`
- 若 `peak - userLamports` 超过后端 `MaxLend()`，编译失败——换能覆盖 peak 的后端

### SolIn / SolOut（native SOL ↔ WSOL）

mint 都是 Wrapped SOL 时，compile 在 hop 之间换形：

- native → WSOL ATA：把本跳 **增量** transfer + `SyncNative` 进下一跳 `UserInputATA`
- WSOL ATA → native：`UnwrapLamports(delta)` 进钱包（ATA 保持打开）
- WSOL → WSOL / native → native：直接转发 delta（同一 ATA）

`.SolIn(hop.SolNative)`：第一跳要 WSOL 且不是 native 时，把 `AmountIn` wrap 进该 hop 的 WSOL ATA。`.SolOut(hop.SolWSOL)`：把最后一跳的 native 增量 wrap（`.WSOLAccount` 或派生 ATA）。默认 `SolAsEmitted` 保持 venue CPI 的形态。

`solfunding.Wrap` 仍是 **固定 lamports** 的 BeforeRoute prelude；跳间换形归 compile。

### SolFunding（WSOL wrap / unwrap）

- `solfunding.Wrap(lamports)` — BeforeRoute：create ATA + transfer + `SyncNative`
- `solfunding.UnwrapWSOL(mode)` / `WrapAndUnwrap` — AfterRoute unwrap
- **Unwrap 模式**（要保留 ATA 时优先用 `UnwrapLamports*`，不要默认 Close）：
  - `UnwrapPartial` — `UnwrapLamports(amount)`（Token ix 45）
  - `UnwrapLamportsAll` — `UnwrapLamports(all)`，保留 ATA + rent
  - `UnwrapClose` — `CloseAccount`，收回 rent

### GasSponsored

两种默认模式：

**1. FromNative** — 用中间 SOL / WSOL 收益以 **SOL** 还款：

```go
gassponsored.FromNative(feePayer, estimatedCostLamports, 12_000).
    WithRepayTo(treasury). // 收款账户 ≠ 代付账户时
    WithWSOL(userWSOLATA).
    WithATARent(createdATA)
```

`settle = ceil((EstimatedCost + ataRent) * ProtectionBps / 10000)`。断言 `SOL_delta + WSOL_delta ≥ settle`，先从 WSOL 解包到 **RepayTo**（保留 ATA），剩余用原生 SOL。

**2. FromToken** — 构造时写死 token 与额度（不做 gas 推算）：

```go
gassponsored.FromToken(userATA, repayTokenATA, amountRaw).
    WithSponsor(feePayer) // 可选：代付账户 ≠ token 金库时
```

断言 token 收益 ≥ amount，再 patched SPL transfer。

`New(sponsor, cost)` ≡ `FromNative(sponsor, cost, 10000)`。

### ArbCheck / MevTip / FeeHook

原子环路（例如 Jupiter 两腿 A→B→A）：链下仍要有**真价差**（不同 `dexes`、Titan vs Jupiter、或自家 venue）。同一聚合器 A→B→A 的 quote 通常是负 EV。

```go
.Feature(arbcheck.Token(userA, minProfit)). // 或 Native(minLamports).WithWSOL(wsol)
.Feature(mevtip.New(jitoTip, bribe)).
.Hop(jupAB).Hop(jupBA)
```

- `arbcheck.Token(ata, minProfit)` — `after ≥ before + minProfit`（0 = 保本）
- `hopconserve.New()` — 每跳：输出 after ≥ before+1（ATA 或 native lamports）；输入扣减 ≥ patched amount_in（`.SkipInput()` 只检查输出）。同一 ATA 作输入输出时跳过输出侧。
- `arbcheck.Native(minProfit).WithWSOL(wsolATA)` — lamports + WSOL
- **先注册 arbcheck**，AfterRoute 逆序才会在 tip/fee 之后做利润断言
- `mevtip.New(receiver, lamports)` — `AfterRoute` 字面 System transfer
- `mevtip.ShareNative(receiver, bps).WithMax(cap).WithWSOL(wsol)` — tip = floor(native 利润 × bps / 10000)；亏损则 tip 0（不 revert）。不超过当前 native lamports。硬地板用 `arbcheck`。
- `feehook.Fixed` / `ProceedsBPS` — 路线结束时的 SOL 费
- `feehook.AtNode(node, recipient).WithFixed(...).WithTokenBPS(...)` — Exact **fee_node_index**：该节点 in-edges 齐后扣一次（Fixed 在 AfterEdge；TokenBps 经 MapForwardAmount，下游 hop 看到净额）

**自定义 fee program**（不用 System/Token transfer）：

```go
template := solana.NewInstruction(feeProgram, accounts, data) // amount 字段置 0
feehook.ProceedsBPS(treasury, 50).
    WithSolSettler(feehook.CustomIx(template, amountOffset))
```

默认 settler：`SystemTransferSettler`（SOL）/ `TokenTransferSettler`（TokenBps）。用 `.WithSolSettler` / `.WithTokenSettler` 覆盖；也可用 `SettlerFunc` 完全自定义。

### AtaPolicy

| 策略 | BeforeRoute | AfterRoute |
|--------|-------------|------------|
| `UseOnly` | — | — |
| `UseAndClose` | — | 关闭中间 ATA |
| `CreateAndCloseCreated` | 创建 缺失 ATA | 关闭本笔 create 的（保留终点） |
| `CreateAndCloseAll` | create 缺失 | 关闭 create + 已有 |
| `CreateOnly` | create 缺失 | — |

链上已有 ATA 时设 `RouteNode.Exists = true`，峰值与 create 才准确。
