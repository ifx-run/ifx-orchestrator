[English](./CUSTOMIZING.md) | 中文

# 定制 ifx-orchestrator

## 接入 venue

实现 `hop.ExactInHop`：

1. `VenueID`、`InputMint`、`OutputMint`、`OutputMeasureAccount`
2. `BuildBlueprint` → amount 置 0 的模板 ix + `PatchSite` 偏移

参考 `venue/raydiumcpmm`、`venue/meteoradammv2`、`venue/pumpfun`。

`pumpfun`：`NewBuyExactSolIn` / `NewSellExactIn`（原生 SOL）。Sell 把 SOL 打到钱包——请作**终点 hop**（`SplTokenAmount` 链式测不到 SOL 收益）。

## Feature

用 `.Feature(...)` 注册。**BeforeRoute** 按注册顺序执行；**AfterRoute** **逆序**（夹道）。

典型租金峰值 + 赞助 + SOL funding：

```go
orchestrator.New(scratch, user).
    AmountIn(amount).
    UserLamports(userSOL).
    Feature(flashrent.Auto()).
    Feature(gassponsored.New(sponsor, fixedCost)). // 或 FromNative(..., 12000).WithWSOL(...) / FromToken(...)
    Feature(solfunding.WrapAndUnwrap(wrapLamports, solfunding.UnwrapLamportsAll)).
    AtaPolicy(feature.AtaCreateAndCloseCreated).
    Feature(mevtip.New(tipTo, tipLamports)).
    Feature(feehook.AtNode(1, feeTo).WithTokenBPS(50, feeATA)).
    Hop(...).
    Build()
```

### FlashRent 后端

- 默认：`flashrent.Auto()` → Jupiter Flash Fill，只借 **一份** TokenAccount rent
- 自定义：`flashrent.AutoWith(flashrent.CustomRentLiquidity{...})`
- 若 `peak - userLamports` 超过后端 `MaxLend()`，编译失败——换能覆盖 peak 的后端

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

### MevTip / FeeHook

- `mevtip.New` — `AfterRoute` 字面 tip
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
