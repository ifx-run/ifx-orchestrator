// Command live_fee_plans: fetch a known Meteora DAMM v2 pool, build several fee
// Feature combinations, dump ix sequences, and RPC-simulate (SigVerify=false).
//
//	IFX_LAUNCHPAD_CONFIG=/path/to/config.toml go run ./examples/live_fee_plans/
package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/ifx-run/ifx-orchestrator/compile"
	"github.com/ifx-run/ifx-orchestrator/feature"
	"github.com/ifx-run/ifx-orchestrator/feature/feehook"
	"github.com/ifx-run/ifx-orchestrator/feature/mevtip"
	"github.com/ifx-run/ifx-orchestrator/hop"
	"github.com/ifx-run/ifx-orchestrator/orchestrator"
	"github.com/ifx-run/ifx-orchestrator/venue/meteoradammv2"
	"github.com/ifx-run/ifx/go-sdk/codec"
	"github.com/ifx-run/ifx/go-sdk/constants"
	"github.com/ifx-run/ifx/go-sdk/scratch"
)

var (
	framePK = solana.MustPublicKeyFromBase58("Fr8dvcgrSYKjpvJd471hQD2QuEjF7656WiEuUSb54obu")
	// Known SOL/USDC DAMM v2 pool (same as prior jupiter_simulate success).
	poolPK = solana.MustPublicKeyFromBase58("8Pm2kZpnxD3hoMmt4bjStX2Pw2Z9abpbHzZxMPqxPmie")
	wsol   = hop.WrappedSOLMint
	usdc   = solana.MustPublicKeyFromBase58("EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v")
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	rpcURL := os.Getenv("SOLANA_RPC_URL")
	if rpcURL == "" {
		if p := os.Getenv("IFX_LAUNCHPAD_CONFIG"); p != "" {
			if u := rpcFromToml(p); u != "" {
				rpcURL = u
			}
		}
	}
	if rpcURL == "" {
		log.Fatal("set SOLANA_RPC_URL or IFX_LAUNCHPAD_CONFIG")
	}

	client := rpc.New(rpcURL)
	user := solana.NewWallet().PublicKey()
	if pub := os.Getenv("USER_PUBKEY"); pub != "" {
		pk, err := solana.PublicKeyFromBase58(pub)
		if err != nil {
			log.Fatal(err)
		}
		user = pk
	}
	// Prefer real accounts for System transfers (random empty keys fail rent-exempt).
	feeTo := user
	tipTo := user
	if pub := os.Getenv("FEE_PUBKEY"); pub != "" {
		pk, err := solana.PublicKeyFromBase58(pub)
		if err != nil {
			log.Fatal(err)
		}
		feeTo = pk
	}
	if pub := os.Getenv("TIP_PUBKEY"); pub != "" {
		pk, err := solana.PublicKeyFromBase58(pub)
		if err != nil {
			log.Fatal(err)
		}
		tipTo = pk
	}

	info, err := client.GetAccountInfo(ctx, poolPK)
	if err != nil || info == nil || info.Value == nil {
		log.Fatalf("fetch pool: %v", err)
	}
	ps, err := meteoradammv2.DecodePoolState(info.Value.Data.GetBinary())
	if err != nil {
		log.Fatal(err)
	}
	tpA, err := mintTokenProgram(ctx, client, ps.MintA)
	if err != nil {
		log.Fatal(err)
	}
	tpB, err := mintTokenProgram(ctx, client, ps.MintB)
	if err != nil {
		log.Fatal(err)
	}
	inATA, _, err := solana.FindAssociatedTokenAddress(user, wsol)
	if err != nil {
		log.Fatal(err)
	}
	outATA, _, err := solana.FindAssociatedTokenAddress(user, usdc)
	if err != nil {
		log.Fatal(err)
	}
	hopImpl, err := meteoradammv2.NewExactIn(meteoradammv2.Params{
		User: user, PoolID: poolPK, Pool: ps,
		InputMint: wsol, OutputMint: usdc,
		UserInputATA: inATA, UserOutputATA: outATA,
		TokenProgramA: tpA, TokenProgramB: tpB,
	})
	if err != nil {
		log.Fatal(err)
	}

	amountIn := uint64(10_000_000) // 0.01 SOL
	type sc struct {
		name string
		want string
		feat func(*orchestrator.Builder) *orchestrator.Builder
	}
	scenarios := []sc{
		{
			name: "live_01_bare_wrap",
			want: "SolIn + Meteora ExactIn（无 fee）",
			feat: func(b *orchestrator.Builder) *orchestrator.Builder { return b },
		},
		{
			name: "live_02_after_fixed",
			want: "后扣 Fixed",
			feat: func(b *orchestrator.Builder) *orchestrator.Builder {
				return b.Feature(feehook.Fixed(feeTo, 7_000))
			},
		},
		{
			name: "live_03_after_proceeds",
			want: "后扣 ProceedsBPS(+WSOL)；wrap 路径亏损 ⇒ fee 0",
			feat: func(b *orchestrator.Builder) *orchestrator.Builder {
				return b.Feature(feehook.ProceedsBPS(feeTo, 30).WithWSOL(inATA))
			},
		},
		{
			name: "live_04_fixed_then_proceeds",
			want: "后扣 Fixed→Proceeds(+WSOL)（先注册 bps 再 Fixed）",
			feat: func(b *orchestrator.Builder) *orchestrator.Builder {
				return b.Feature(feehook.ProceedsBPS(feeTo, 30).WithWSOL(inATA)).Feature(feehook.Fixed(feeTo, 7_000))
			},
		},
		{
			name: "live_05_fixed_mevtip",
			want: "后扣 Fixed + MevTip",
			feat: func(b *orchestrator.Builder) *orchestrator.Builder {
				return b.Feature(mevtip.New(tipTo, 1_000)).Feature(feehook.Fixed(feeTo, 5_000))
			},
		},
	}

	fmt.Println("# live_fee_plans — Meteora DAMM v2 SOL→USDC")
	fmt.Printf("user=%s pool=%s\n", user, poolPK)
	fmt.Printf("fee=%s tip=%s\n\n", feeTo, tipTo)

	tape := 4096
	for _, sc := range scenarios {
		s := scratch.ForPublicFrame(framePK, constants.DefaultProgramID, &tape)
		b := orchestrator.New(s, user).
			AmountIn(amountIn).
			MinAmountOut(1).
			AtaPolicy(feature.AtaCreateOnly).
			SolIn(hop.SolNative)
		b = sc.feat(b)
		plan, err := b.Hop(hopImpl).Build()
		fmt.Printf("## %s\nexpect: %s\n", sc.name, sc.want)
		if err != nil {
			fmt.Printf("ERROR: %v\n\n", err)
			continue
		}
		dump(plan)
		ok, units, simErr := simulate(ctx, client, user, plan)
		fmt.Printf("simulate: ok=%v units=%d err=%v\n\n", ok, units, simErr)
	}
}

func dump(plan *compile.Plan) {
	fmt.Printf("ix_count: %d\n", len(plan.Instructions))
	for i, ix := range plan.Instructions {
		pid := ix.ProgramID()
		label := shortProg(pid)
		meta := ""
		data, _ := ix.Data()
		switch {
		case pid.Equals(constants.DefaultProgramID):
			meta = codec.IfxIxHint(data)
			if meta == "" {
				meta = "ifx_?"
			}
		case pid.Equals(solana.SystemProgramID) && len(data) >= 12 && binary.LittleEndian.Uint32(data[0:4]) == 2:
			meta = fmt.Sprintf("transfer lamports=%d", binary.LittleEndian.Uint64(data[4:12]))
		case pid.Equals(meteoradammv2.ProgramID):
			meta = "swap"
		case pid.Equals(solana.SPLAssociatedTokenAccountProgramID):
			meta = "create_ata"
		case pid.Equals(solana.TokenProgramID) && len(data) > 0 && data[0] == 17:
			meta = "sync_native"
		case pid.Equals(solana.TokenProgramID):
			meta = "token"
		}
		fmt.Printf("  [%2d] %-12s %s\n", i, label, meta)
	}
}

func simulate(ctx context.Context, client *rpc.Client, user solana.PublicKey, plan *compile.Plan) (bool, uint64, any) {
	recent, err := client.GetLatestBlockhash(ctx, rpc.CommitmentConfirmed)
	if err != nil {
		return false, 0, err
	}
	tx, err := solana.NewTransaction(plan.Instructions, recent.Value.Blockhash, solana.TransactionPayer(user))
	if err != nil {
		return false, 0, err
	}
	n := int(tx.Message.Header.NumRequiredSignatures)
	tx.Signatures = make([]solana.Signature, n)
	sim, err := client.SimulateTransactionWithOpts(ctx, tx, &rpc.SimulateTransactionOpts{
		ReplaceRecentBlockhash: true,
		SigVerify:              false,
		Commitment:             rpc.CommitmentConfirmed,
	})
	if err != nil {
		return false, 0, err
	}
	var units uint64
	var simErr any
	ok := false
	if sim != nil && sim.Value != nil {
		ok = sim.Value.Err == nil
		simErr = sim.Value.Err
		if sim.Value.UnitsConsumed != nil {
			units = *sim.Value.UnitsConsumed
		}
	}
	return ok, units, simErr
}

func shortProg(pid solana.PublicKey) string {
	switch {
	case pid.Equals(constants.DefaultProgramID):
		return "ifx"
	case pid.Equals(solana.SystemProgramID):
		return "system"
	case pid.Equals(solana.TokenProgramID):
		return "token"
	case pid.Equals(solana.SPLAssociatedTokenAccountProgramID):
		return "ata"
	case pid.Equals(meteoradammv2.ProgramID):
		return "meteora"
	default:
		s := pid.String()
		return s[:4] + "…" + s[len(s)-4:]
	}
}

func mintTokenProgram(ctx context.Context, client *rpc.Client, mint solana.PublicKey) (solana.PublicKey, error) {
	info, err := client.GetAccountInfo(ctx, mint)
	if err != nil || info == nil || info.Value == nil {
		return solana.PublicKey{}, fmt.Errorf("mint %s: %v", mint, err)
	}
	return info.Value.Owner, nil
}

func rpcFromToml(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "rpc_url") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				return strings.Trim(parts[1], ` "`)
			}
		}
	}
	return ""
}
