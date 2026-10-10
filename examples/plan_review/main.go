// Command plan_review builds a gallery of ExactIn plans (no RPC simulate)
// and prints a human-readable instruction sequence for manual review.
//
//	go run ./examples/plan_review/
package main

import (
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/compile"
	"github.com/ifx-run/ifx-orchestrator/feature/arbcheck"
	"github.com/ifx-run/ifx-orchestrator/feature/feehook"
	"github.com/ifx-run/ifx-orchestrator/feature/gassponsored"
	"github.com/ifx-run/ifx-orchestrator/feature/hopconserve"
	"github.com/ifx-run/ifx-orchestrator/feature/mevtip"
	"github.com/ifx-run/ifx-orchestrator/feature/solfunding"
	"github.com/ifx-run/ifx-orchestrator/hop"
	"github.com/ifx-run/ifx-orchestrator/orchestrator"
	"github.com/ifx-run/ifx-orchestrator/venue/mock"
	"github.com/ifx-run/ifx/go-sdk/codec"
	"github.com/ifx-run/ifx/go-sdk/constants"
	"github.com/ifx-run/ifx/go-sdk/scratch"
)

func main() {
	w := newWorld()
	scenarios := []scenario{
		{
			Name: "01_single_hop_bare",
			Want: "纯单跳：无 Frame，一条 venue ix，amount/min_out bake 进模板",
			Build: func() (*compile.Plan, error) {
				return orchestrator.New(nil, w.user).
					AmountIn(1_000_000).
					MinAmountOut(900_000).
					Hop(w.hopAB()).
					Build()
			},
		},
		{
			Name: "02_single_hop_fee_after_fixed",
			Want: "单跳 + 路由后固定手续费：venue + System transfer，仍可无 ifx",
			Build: func() (*compile.Plan, error) {
				return orchestrator.New(w.scratch(), w.user).
					AmountIn(1_000_000).
					MinAmountOut(900_000).
					Feature(feehook.Fixed(w.feeTreasury, 42_000)).
					Hop(w.hopAB()).
					Build()
			},
		},
		{
			Name: "03_single_hop_fee_after_proceeds_bps",
			Want: "单跳 + 路由后 SOL 利润 bps：需要 Reset/Let/Assert/patched transfer",
			Build: func() (*compile.Plan, error) {
				return orchestrator.New(w.scratch(), w.user).
					AmountIn(1_000_000).
					Feature(feehook.ProceedsBPS(w.feeTreasury, 30)).
					Hop(w.hopAB()).
					Build()
			},
		},
		{
			Name: "04_two_hop_bare",
			Want: "两跳：hop0 bake；measure Δ → ifx CPI hop1",
			Build: func() (*compile.Plan, error) {
				return orchestrator.New(w.scratch(), w.user).
					AmountIn(1_000_000).
					MinAmountOut(800_000).
					Hop(w.hopAB()).
					Hop(w.hopBC()).
					Build()
			},
		},
		{
			Name: "05_two_hop_fee_mid_token_bps",
			Want: "中扣：AtNode(1)+TokenBps，在 hop0→hop1 的 forward 上抽成，下一跳吃净值",
			Build: func() (*compile.Plan, error) {
				return orchestrator.New(w.scratch(), w.user).
					AmountIn(1_000_000).
					Feature(feehook.AtNode(1, w.feeTreasury).WithTokenBPS(50, w.feeATA)).
					Hop(w.hopAB()).
					Hop(w.hopBC()).
					Build()
			},
		},
		{
			Name: "06_two_hop_fee_mid_fixed",
			Want: "中扣固定：AtNode(1)+Fixed，第一跳结算后 System transfer，再 hop1",
			Build: func() (*compile.Plan, error) {
				return orchestrator.New(w.scratch(), w.user).
					AmountIn(1_000_000).
					Feature(feehook.AtNode(1, w.feeTreasury).WithFixed(12_345)).
					Hop(w.hopAB()).
					Hop(w.hopBC()).
					Build()
			},
		},
		{
			Name: "07_two_hop_fee_after_fixed",
			Want: "后扣固定：整条路由结束后 System transfer",
			Build: func() (*compile.Plan, error) {
				return orchestrator.New(w.scratch(), w.user).
					AmountIn(1_000_000).
					Feature(feehook.Fixed(w.feeTreasury, 12_345)).
					Hop(w.hopAB()).
					Hop(w.hopBC()).
					Build()
			},
		},
		{
			Name: "07b_after_fixed_then_proceeds",
			Want: "后扣：先 Fixed 再 Proceeds（AfterRoute 逆序 ⇒ 先注册 bps 再 Fixed）",
			Build: func() (*compile.Plan, error) {
				return orchestrator.New(w.scratch(), w.user).
					AmountIn(1_000_000).
					Feature(feehook.ProceedsBPS(w.feeTreasury, 30)).
					Feature(feehook.Fixed(w.feeTreasury, 7_000)).
					Hop(w.hopAB()).
					Build()
			},
		},
		{
			Name: "07c_mid_fixed_then_token_bps",
			Want: "中扣：先 Fixed 再 TokenBps（AtNode 按注册顺序）",
			Build: func() (*compile.Plan, error) {
				return orchestrator.New(w.scratch(), w.user).
					AmountIn(1_000_000).
					Feature(feehook.AtNode(1, w.feeTreasury).WithFixed(9_999)).
					Feature(feehook.AtNode(1, w.feeTreasury).WithTokenBPS(50, w.feeATA)).
					Hop(w.hopAB()).
					Hop(w.hopBC()).
					Build()
			},
		},
		{
			Name: "08_two_hop_arbcheck_mevtip",
			Want: "两跳环 + arbcheck + 固定 tip：baseline → hops → tip → assert（Settlement 相内按注册序）",
			Build: func() (*compile.Plan, error) {
				return orchestrator.New(w.scratch(), w.user).
					AmountIn(1_000_000).
					Feature(arbcheck.Token(w.ataA, 1)).
					Feature(mevtip.New(w.tipRecv, 1_000)).
					Hop(w.hopAB()).
					Hop(w.hopBA()).
					Build()
			},
		},
		{
			Name: "09_two_hop_gas_sponsored",
			Want: "GasSponsored FromNative：baseline → hops → assert proceeds ≥ settle → patched repay",
			Build: func() (*compile.Plan, error) {
				return orchestrator.New(w.scratch(), w.user).
					AmountIn(1_000_000).
					Feature(gassponsored.FromNative(w.sponsor, 5_000, 12_000)).
					Hop(w.hopAB()).
					Hop(w.hopBC()).
					Build()
			},
		},
		{
			Name: "10_two_hop_combo_kitchen_sink",
			Want: "组合：GasSponsored + 中扣 TokenBps + arbcheck + MevTip + 后扣 Fixed",
			Build: func() (*compile.Plan, error) {
				return orchestrator.New(w.scratch(), w.user).
					AmountIn(1_000_000).
					Feature(gassponsored.FromNative(w.sponsor, 5_000, 11_000)).
					Feature(arbcheck.Token(w.ataC, 0)).
					Feature(mevtip.New(w.tipRecv, 500)).
					Feature(feehook.AtNode(1, w.feeTreasury).WithTokenBPS(25, w.feeATA)).
					Feature(feehook.Fixed(w.feeTreasury, 1_000)).
					Hop(w.hopAB()).
					Hop(w.hopBC()).
					Build()
			},
		},
		{
			Name: "11_single_hop_hopconserve",
			Want: "单跳 + HopConserve：measure in/out + assert（必须 ifx）",
			Build: func() (*compile.Plan, error) {
				return orchestrator.New(w.scratch(), w.user).
					AmountIn(1_000_000).
					Feature(hopconserve.New()).
					Hop(w.hopAB().WithInput(w.ataA)).
					Build()
			},
		},
		{
			Name: "12_single_hop_sol_in_wrap",
			Want: "SolIn(Native)：CreateATA + System transfer(AmountIn) + SyncNative + venue（无 ifx）",
			Build: func() (*compile.Plan, error) {
				return orchestrator.New(nil, w.user).
					AmountIn(10_000_000).
					MinAmountOut(1).
					SolIn(hop.SolNative).
					Hop(w.hopWSOLToB()).
					Build()
			},
		},
	}

	out := os.Stdout
	fmt.Fprintln(out, "# plan_review — instruction gallery (construct only, no simulate)")
	fmt.Fprintln(out)
	w.printLegend(out)

	var failed int
	for _, sc := range scenarios {
		fmt.Fprintf(out, "\n## %s\n", sc.Name)
		fmt.Fprintf(out, "expect: %s\n", sc.Want)
		plan, err := sc.Build()
		if err != nil {
			failed++
			fmt.Fprintf(out, "ERROR: %v\n", err)
			continue
		}
		dumpPlan(out, w, plan)
	}
	if failed > 0 {
		log.Fatalf("%d scenario(s) failed to build", failed)
	}
}

type scenario struct {
	Name  string
	Want  string
	Build func() (*compile.Plan, error)
}

type world struct {
	user, sponsor, tipRecv, feeTreasury solana.PublicKey
	mintA, mintB, mintC                 solana.PublicKey
	ataA, ataB, ataC, feeATA, wsolATA   solana.PublicKey
	venueAB, venueBC, venueBA           solana.PublicKey
	frame                               solana.PublicKey
	label                               map[string]string
}

func newWorld() *world {
	pk := func() solana.PublicKey { return solana.NewWallet().PublicKey() }
	w := &world{
		user:        pk(),
		sponsor:     pk(),
		tipRecv:     pk(),
		feeTreasury: pk(),
		mintA:       pk(),
		mintB:       pk(),
		mintC:       pk(),
		ataA:        pk(),
		ataB:        pk(),
		ataC:        pk(),
		feeATA:      pk(),
		venueAB:     pk(),
		venueBC:     pk(),
		venueBA:     pk(),
		frame:       solana.MustPublicKeyFromBase58("Fr8dvcgrSYKjpvJd471hQD2QuEjF7656WiEuUSb54obu"),
		label:       map[string]string{},
	}
	wsol, err := solfunding.AssociatedTokenAddress(w.user, hop.WrappedSOLMint, solana.TokenProgramID)
	if err != nil {
		panic(err)
	}
	w.wsolATA = wsol
	w.tag(w.user, "user")
	w.tag(w.sponsor, "sponsor")
	w.tag(w.tipRecv, "tip_recv")
	w.tag(w.feeTreasury, "fee_treasury")
	w.tag(w.mintA, "mintA")
	w.tag(w.mintB, "mintB")
	w.tag(w.mintC, "mintC")
	w.tag(w.ataA, "ataA")
	w.tag(w.ataB, "ataB")
	w.tag(w.ataC, "ataC")
	w.tag(w.feeATA, "fee_ata")
	w.tag(w.wsolATA, "wsol_ata")
	w.tag(w.venueAB, "venue_AB")
	w.tag(w.venueBC, "venue_BC")
	w.tag(w.venueBA, "venue_BA")
	w.tag(w.frame, "frame")
	w.tag(constants.DefaultProgramID, "ifx")
	w.tag(solana.SystemProgramID, "system")
	w.tag(solana.TokenProgramID, "token")
	w.tag(solana.SPLAssociatedTokenAccountProgramID, "ata_prog")
	w.tag(hop.WrappedSOLMint, "wsol_mint")
	return w
}

func (w *world) tag(pk solana.PublicKey, name string) {
	w.label[pk.String()] = name
}

func (w *world) name(pk solana.PublicKey) string {
	if s, ok := w.label[pk.String()]; ok {
		return s
	}
	s := pk.String()
	if len(s) > 8 {
		return s[:4] + "…" + s[len(s)-4:]
	}
	return s
}

func (w *world) scratch() *scratch.FrameScratch {
	tape := 8192
	return scratch.ForPublicFrame(w.frame, constants.DefaultProgramID, &tape)
}

func (w *world) acc(extra solana.PublicKey) []*solana.AccountMeta {
	return []*solana.AccountMeta{
		{PublicKey: w.user, IsSigner: true, IsWritable: true},
		{PublicKey: extra, IsWritable: true},
	}
}

func (w *world) hopAB() *mock.ExactIn {
	return mock.New("venue_AB", w.venueAB, w.mintA, w.mintB, w.ataB, w.acc(w.ataB)).WithInput(w.ataA)
}
func (w *world) hopBC() *mock.ExactIn {
	return mock.New("venue_BC", w.venueBC, w.mintB, w.mintC, w.ataC, w.acc(w.ataC)).WithInput(w.ataB)
}
func (w *world) hopBA() *mock.ExactIn {
	return mock.New("venue_BA", w.venueBA, w.mintB, w.mintA, w.ataA, w.acc(w.ataA)).WithInput(w.ataB)
}
func (w *world) hopWSOLToB() *mock.ExactIn {
	return mock.New("venue_WSOL_B", w.venueAB, hop.WrappedSOLMint, w.mintB, w.ataB, w.acc(w.ataB)).WithInput(w.wsolATA)
}

func (w *world) printLegend(out *os.File) {
	fmt.Fprintln(out, "## key legend")
	for _, row := range []struct {
		pk   solana.PublicKey
		note string
	}{
		{w.user, "payer / user"},
		{w.sponsor, "gas sponsor repay dest"},
		{w.tipRecv, "mev tip receiver"},
		{w.feeTreasury, "fee SOL treasury"},
		{w.feeATA, "fee token ATA"},
		{w.venueAB, "mock venue A→B"},
		{w.venueBC, "mock venue B→C"},
		{w.venueBA, "mock venue B→A"},
		{w.frame, "public ifx frame"},
	} {
		fmt.Fprintf(out, "- %-12s %s\n", w.name(row.pk), row.pk)
	}
}

func dumpPlan(out *os.File, w *world, plan *compile.Plan) {
	fmt.Fprintf(out, "ix_count: %d\n", len(plan.Instructions))
	ifxN := 0
	for _, ix := range plan.Instructions {
		if ix.ProgramID().Equals(constants.DefaultProgramID) {
			ifxN++
		}
	}
	fmt.Fprintf(out, "ifx_count: %d\n", ifxN)
	for i, ix := range plan.Instructions {
		fmt.Fprintf(out, "  [%2d] %s\n", i, describeIx(w, ix))
	}
}

func describeIx(w *world, ix solana.Instruction) string {
	prog := ix.ProgramID()
	data, _ := ix.Data()
	var b strings.Builder
	fmt.Fprintf(&b, "%-10s", w.name(prog))

	switch {
	case prog.Equals(constants.DefaultProgramID):
		hint := codec.IfxIxHint(data)
		if hint == "" {
			hint = "ifx_?"
		}
		fmt.Fprintf(&b, " %s", hint)
		if hint == "ifx_patched_cpi" && len(data) > 1 {
			fmt.Fprintf(&b, " (inner accounts=%d data_len=%d)", len(ix.Accounts()), len(data))
		}
	case prog.Equals(solana.SystemProgramID):
		if len(data) >= 12 && binary.LittleEndian.Uint32(data[0:4]) == 2 {
			lamports := binary.LittleEndian.Uint64(data[4:12])
			fmt.Fprintf(&b, " transfer lamports=%d", lamports)
		} else {
			fmt.Fprintf(&b, " system data_len=%d", len(data))
		}
	case prog.Equals(solana.TokenProgramID):
		if len(data) >= 1 {
			switch data[0] {
			case 3:
				amt := uint64(0)
				if len(data) >= 9 {
					amt = binary.LittleEndian.Uint64(data[1:9])
				}
				fmt.Fprintf(&b, " transfer amount=%d", amt)
			case 17:
				fmt.Fprintf(&b, " sync_native")
			case 9:
				fmt.Fprintf(&b, " close_account")
			default:
				fmt.Fprintf(&b, " token disc=%d", data[0])
			}
		}
	case prog.Equals(solana.SPLAssociatedTokenAccountProgramID):
		if len(data) >= 1 && data[0] == 1 {
			fmt.Fprintf(&b, " create_idempotent")
		} else {
			fmt.Fprintf(&b, " ata ix")
		}
	default:
		// mock venue: [amount u64][min_out u64]
		if len(data) >= 16 {
			fmt.Fprintf(&b, " ExactIn amount=%d min_out=%d",
				binary.LittleEndian.Uint64(data[0:8]),
				binary.LittleEndian.Uint64(data[8:16]))
		} else {
			fmt.Fprintf(&b, " data_len=%d", len(data))
		}
	}

	metas := ix.Accounts()
	if len(metas) > 0 && len(metas) <= 6 {
		parts := make([]string, 0, len(metas))
		for _, m := range metas {
			parts = append(parts, w.name(m.PublicKey))
		}
		fmt.Fprintf(&b, "  accounts=[%s]", strings.Join(parts, ","))
	} else if len(metas) > 6 {
		fmt.Fprintf(&b, "  accounts(%d)=[%s,…]", len(metas), w.name(metas[0].PublicKey))
	}
	return b.String()
}
