// Command simulate_mainnet builds a hardcoded ExactIn plan and optionally simulates it on RPC.
//
// Usage:
//
//	RPC_URL=https://solana-rpc.publicnode.com go run ./examples/simulate_mainnet/
//
// Without a funded user / matching ATAs the simulate is expected to fail with a program error;
// the point is compile → wire tx → RPC round-trip. Set SKIP_SIMULATE=1 to only print the plan.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/ifx-run/ifx-orchestrator/feature"
	"github.com/ifx-run/ifx-orchestrator/feature/feehook"
	"github.com/ifx-run/ifx-orchestrator/feature/flashrent"
	"github.com/ifx-run/ifx-orchestrator/feature/gassponsored"
	"github.com/ifx-run/ifx-orchestrator/feature/mevtip"
	"github.com/ifx-run/ifx-orchestrator/orchestrator"
	"github.com/ifx-run/ifx-orchestrator/venue/raydiumcpmm"
	"github.com/ifx-run/ifx/go-sdk/constants"
	"github.com/ifx-run/ifx/go-sdk/scratch"
)

// Mainnet public ifx Frame (same as other examples).
var framePK = solana.MustPublicKeyFromBase58("Fr8dvcgrSYKjpvJd471hQD2QuEjF7656WiEuUSb54obu")

// Hardcoded Raydium CPMM pool (SOL/USDC style — fetched at runtime for vaults/mints).
// Fallback synthetic accounts used when RPC fetch fails so compile still demos.
var hardcodedPool = solana.MustPublicKeyFromBase58("7JuwJuNU88gurFnyWeicysseyoDHqcWkY7Vx9FrC2n8")

func main() {
	rpcURL := os.Getenv("RPC_URL")
	if rpcURL == "" {
		rpcURL = "https://solana-rpc.publicnode.com"
	}

	user := solana.NewWallet()
	sponsor := solana.NewWallet().PublicKey()
	tipTo := solana.NewWallet().PublicKey()
	feeTo := solana.NewWallet().PublicKey()

	tape := 2048
	s := scratch.ForPublicFrame(framePK, constants.DefaultProgramID, &tape)

	pool, poolState, err := loadPool(rpcURL, hardcodedPool)
	if err != nil {
		log.Printf("pool fetch failed (%v); using synthetic pool for compile-only demo", err)
		pool, poolState = syntheticPool()
	}

	inMint, outMint := poolState.MintA, poolState.MintB
	inATA, _, err := solana.FindAssociatedTokenAddress(user.PublicKey(), inMint)
	if err != nil {
		log.Fatal(err)
	}
	outATA, _, err := solana.FindAssociatedTokenAddress(user.PublicKey(), outMint)
	if err != nil {
		log.Fatal(err)
	}

	hop, err := raydiumcpmm.NewExactIn(raydiumcpmm.Params{
		User: user.PublicKey(), PoolID: pool, Pool: poolState,
		InputMint: inMint, OutputMint: outMint,
		UserInputATA: inATA, UserOutputATA: outATA,
	})
	if err != nil {
		log.Fatal(err)
	}

	fr := flashrent.Auto()
	fr.Always = true // force borrow/repay into the plan for demo visibility

	plan, err := orchestrator.New(s, user.PublicKey()).
		AmountIn(100_000).
		MinAmountOut(1).
		UserLamports(0).
		Feature(fr).
		Feature(gassponsored.New(sponsor, 5_000)).
		Feature(mevtip.New(tipTo, 1_000)).
		Feature(feehook.Fixed(feeTo, 2_000)).
		AtaPolicy(feature.AtaCreateAndCloseCreated).
		Hop(hop).
		Build()
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("compiled %d instructions (user=%s pool=%s)\n", len(plan.Instructions), user.PublicKey(), pool)
	for i, ix := range plan.Instructions {
		fmt.Printf("  [%d] %s\n", i, ix.ProgramID())
	}

	if os.Getenv("SKIP_SIMULATE") == "1" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	client := rpc.New(rpcURL)

	recent, err := client.GetLatestBlockhash(ctx, rpc.CommitmentConfirmed)
	if err != nil {
		log.Fatalf("blockhash: %v", err)
	}

	tx, err := solana.NewTransaction(
		plan.Instructions,
		recent.Value.Blockhash,
		solana.TransactionPayer(user.PublicKey()),
	)
	if err != nil {
		log.Fatalf("new tx: %v", err)
	}
	_, err = tx.Sign(func(key solana.PublicKey) *solana.PrivateKey {
		if key.Equals(user.PublicKey()) {
			return &user.PrivateKey
		}
		return nil
	})
	if err != nil {
		log.Fatalf("sign: %v", err)
	}

	sim, err := client.SimulateTransactionWithOpts(ctx, tx, &rpc.SimulateTransactionOpts{
		ReplaceRecentBlockhash: true,
		Commitment:             rpc.CommitmentConfirmed,
	})
	if err != nil {
		log.Fatalf("simulate rpc: %v", err)
	}
	if sim == nil || sim.Value == nil {
		log.Fatal("empty simulate response")
	}
	fmt.Printf("simulate ok=%v units=%v err=%v\n", sim.Value.Err == nil, sim.Value.UnitsConsumed, sim.Value.Err)
	for _, line := range sim.Value.Logs {
		fmt.Println(line)
	}
}

func loadPool(rpcURL string, poolID solana.PublicKey) (solana.PublicKey, raydiumcpmm.PoolState, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client := rpc.New(rpcURL)
	info, err := client.GetAccountInfo(ctx, poolID)
	if err != nil {
		return solana.PublicKey{}, raydiumcpmm.PoolState{}, err
	}
	if info == nil || info.Value == nil {
		return solana.PublicKey{}, raydiumcpmm.PoolState{}, fmt.Errorf("account missing")
	}
	state, err := raydiumcpmm.DecodePoolState(info.Value.Data.GetBinary())
	if err != nil {
		return solana.PublicKey{}, raydiumcpmm.PoolState{}, err
	}
	return poolID, state, nil
}

func syntheticPool() (solana.PublicKey, raydiumcpmm.PoolState) {
	tokenProg := solana.TokenProgramID
	return solana.NewWallet().PublicKey(), raydiumcpmm.PoolState{
		ConfigID:      solana.NewWallet().PublicKey(),
		VaultA:        solana.NewWallet().PublicKey(),
		VaultB:        solana.NewWallet().PublicKey(),
		MintA:         solana.NewWallet().PublicKey(),
		MintB:         solana.NewWallet().PublicKey(),
		MintProgramA:  tokenProg,
		MintProgramB:  tokenProg,
		ObservationID: solana.NewWallet().PublicKey(),
	}
}
