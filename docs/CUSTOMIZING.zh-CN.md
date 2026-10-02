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

典型租金峰值 + 赞助组合：

```go
orchestrator.New(scratch, user).
    AmountIn(amount).
    UserLamports(userSOL).
    Feature(flashrent.Auto()).                   // 先 borrow
    Feature(gassponsored.New(sponsor, fixedCost)). // baseline 用户 SOL
    AtaPolicy(feature.AtaCreateAndCloseCreated). // 再 create ATA
    Feature(mevtip.New(tipTo, tipLamports)).
    Feature(feehook.ProceedsBPS(feeTo, 50)).
    Hop(...).
    Build()
// AfterRoute（逆序）：fee → tip → close ATA → sponsor repay → flash repay
```

### FlashRent 后端

- 默认：`flashrent.Auto()` → Jupiter Flash Fill，只借 **一份** TokenAccount rent
- 自定义：`flashrent.AutoWith(flashrent.CustomRentLiquidity{...})`
- 若 `peak - userLamports` 超过后端 `MaxLend()`，编译失败——换能覆盖 peak 的后端

### GasSponsored

`BeforeRoute` 记录用户 lamports；`AfterRoute` 断言 `Δuser ≥ FixedCost（+ 可选 ATA rent）`，再 patched System transfer 还给 `Sponsor`。可选 `.WithATARent(ata)` 在 baseline 与第一条边之间量 sponsor 垫的 ATA 租金（须注册在 `Ata` **之前**）。

与 FlashRent 正交：flash = 峰值流动性；sponsor = 代付 gas + 从成交 SOL 回款。

### MevTip / FeeHook

- `mevtip.New(receiver, lamports)` — `AfterRoute` 字面 System transfer
- `feehook.Fixed(receiver, lamports)` — 固定费
- `feehook.ProceedsBPS(receiver, bps)` — `floor(bps * SOL_delta / 10000)` patched transfer

### AtaPolicy

| 策略 | BeforeRoute | AfterRoute |
|--------|-------------|------------|
| `UseOnly` | — | — |
| `UseAndClose` | — | 关闭中间 ATA |
| `CreateAndCloseCreated` | 创建 缺失 ATA | 关闭本笔 create 的（保留终点） |
| `CreateAndCloseAll` | create 缺失 | 关闭 create + 已有 |
| `CreateOnly` | create 缺失 | — |

链上已有 ATA 时设 `RouteNode.Exists = true`，峰值与 create 才准确。
