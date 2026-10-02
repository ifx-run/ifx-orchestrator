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
    Feature(gassponsored.New(sponsor, fixedCost)). // 或 .WithWSOL(userWSOLATA)
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

### GasSponsored + RepayMode

`BeforeRoute` baseline；`AfterRoute` 断言 proceeds ≥ settle，再按模式还款：

| Mode | 行为 |
|------|------|
| `InterceptSOL`（默认） | patched System transfer user → sponsor |
| `InterceptWSOL` | `SyncNative` + patched `UnwrapLamports` 从用户 WSOL ATA **直接解到 sponsor**（ATA 不关） |
| `TokenTransfer` | patched SPL transfer user ATA → sponsor ATA（`FixedCost` 为 token raw） |

`.WithWSOL(ata)` / `.WithToken(userATA, sponsorATA)`。`.WithATARent(ata)` 仅配合 `InterceptSOL`。

### MevTip / FeeHook

- `mevtip.New` — `AfterRoute` 字面 tip
- `feehook.Fixed` / `ProceedsBPS` — 路线结束时的 SOL 费
- `feehook.AtNode(node, recipient).WithFixed(...).WithTokenBPS(...)` — Exact **fee_node_index**：该节点 in-edges 齐后扣一次（Fixed 在 AfterEdge；TokenBps 经 MapForwardAmount，下游 hop 看到净额）

### AtaPolicy

| 策略 | BeforeRoute | AfterRoute |
|--------|-------------|------------|
| `UseOnly` | — | — |
| `UseAndClose` | — | 关闭中间 ATA |
| `CreateAndCloseCreated` | 创建 缺失 ATA | 关闭本笔 create 的（保留终点） |
| `CreateAndCloseAll` | create 缺失 | 关闭 create + 已有 |
| `CreateOnly` | create 缺失 | — |

链上已有 ATA 时设 `RouteNode.Exists = true`，峰值与 create 才准确。
