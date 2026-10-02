# ifx-orchestrator

[English](./README.md) | 中文

Solana **execution orchestrator / venue hub**：把外部给定的 hops 编成可执行的 **ifx** tx。

不是 Jupiter 式最优路径聚合器，也不上新的链上 router program。相对 Jupiter 合约：**开箱即用**（fluent `orchestrator.Builder`）+ **高度可定制**（`ExactInHop` / `Feature` / Flash 后端可插）。

| 层 | 职责 |
|---|---|
| Venue (`ExactInHop`) | 账户布局、ix 模板、ExactIn patch |
| Feature | ATA / FlashRent / GasSponsored / Fee / Tip … |
| Compiler | 图上边循环 → Frame + ifx ix |
| Orchestrator | 开箱 Builder 门面 |

**首期语言：Go**（`ifx/go-sdk`）；Rust / TS 镜像后置。

## 现状（Phase 0–2）

- 框架：`AmountFlow`、`ExactInHop`、`compile`、`orchestrator`
- Venue：`raydiumcpmm`、`meteoradammv2`、`mock`
- Feature：`AtaPolicy`、`rentpeak`、`flashrent`（默认 Jupiter，可插后端）

```bash
go test ./...
go run ./examples/mock_two_hop/
go run ./examples/two_venue_path/
```

详见 [docs/CUSTOMIZING.zh-CN.md](./docs/CUSTOMIZING.zh-CN.md)。设计全文见 [docs/design.zh-CN.md](./docs/design.zh-CN.md)。
