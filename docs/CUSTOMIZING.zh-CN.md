[English](./CUSTOMIZING.md) | 中文

# 定制 ifx-orchestrator

## 接入 venue

实现 `hop.ExactInHop`：

1. `VenueID`、`InputMint`、`OutputMint`、`OutputMeasureAccount`
2. `BuildBlueprint` → amount 置 0 的模板 ix + `PatchSite` 偏移

参考 `venue/raydiumcpmm`、`venue/meteoradammv2`。

## Feature

用 `.Feature(...)` 注册。**BeforeRoute** 按注册顺序执行；**AfterRoute** **逆序**（夹道）。

典型租金峰值组合：

```go
orchestrator.New(scratch, user).
    AmountIn(amount).
    UserLamports(userSOL).
    Feature(flashrent.Auto()).                 // 先 borrow
    AtaPolicy(feature.AtaCreateAndCloseCreated). // 再 create ATA
    Hop(...).
    Build()
// AfterRoute：先 close ATA，再 repay
```

### FlashRent 后端

- 默认：`flashrent.Auto()` → Jupiter Flash Fill，只借 **一份** TokenAccount rent
- 自定义：`flashrent.AutoWith(flashrent.CustomRentLiquidity{...})`
- 若 `peak - userLamports` 超过后端 `MaxLend()`，编译失败——换能覆盖 peak 的后端

### AtaPolicy

| 策略 | BeforeRoute | AfterRoute |
|--------|-------------|------------|
| `UseOnly` | — | — |
| `UseAndClose` | — | 关闭中间 ATA |
| `CreateAndCloseCreated` | 创建 缺失 ATA | 关闭本笔 create 的（保留终点） |
| `CreateAndCloseAll` | create 缺失 | 关闭 create + 已有 |
| `CreateOnly` | create 缺失 | — |

链上已有 ATA 时设 `RouteNode.Exists = true`，峰值与 create 才准确。
