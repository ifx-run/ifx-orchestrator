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

## 现状（Phase 0–4）

- 框架：`AmountFlow`、`ExactInHop`、`compile`、`orchestrator`
- Venue：`raydiumcpmm` / `raydiumammv4` / `raydiumclmm` / `raydiumlaunchpad` / `byrealclmm` / `pancakeswapclmm`、`meteoradammv2` / `meteoradlmm` / `meteoradbc`、`whirlpool`、`lifinityv2`、`pumpfun`（内盘 + v2）、`pumpamm`、`jupiter`、`titan`、`mock`
- Feature：`AtaPolicy`、`rentpeak`、`flashrent`、`gassponsored`（SOL/WSOL/Token 还款）、`solfunding`（wrap + `UnwrapLamports`）、`SolIn`/`SolOut`（跳间 native SOL ↔ WSOL）、`mevtip`（定额 / native 利润抽成）、`arbcheck`、`hopconserve`、`feehook`（含图中 `AtNode`）

路径发现用 Jupiter **quote**；hop 可以是自建 venue，也可以是聚合腿（`venue/jupiter` JUP6、`venue/titan` T1TAN），方便在聚合器前后再绑一条自家腿（`examples/jupiter_simulate` 仍是 quote → 自建 venue）。

```bash
go test ./...
go run ./examples/mock_two_hop/
go run ./examples/two_venue_path/
RPC_URL=https://solana-rpc.publicnode.com go run ./examples/simulate_mainnet/
```

详见 [docs/CUSTOMIZING.zh-CN.md](./docs/CUSTOMIZING.zh-CN.md)。设计全文见 [docs/design.zh-CN.md](./docs/design.zh-CN.md)。
