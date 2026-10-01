// Command two_venue_path compiles Raydium CPMM → Meteora DAMM v2 with synthetic pool context.
package main

import (
	"fmt"
	"log"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/orchestrator"
	"github.com/ifx-run/ifx-orchestrator/venue/meteoradammv2"
	"github.com/ifx-run/ifx-orchestrator/venue/raydiumcpmm"
	"github.com/ifx-run/ifx/go-sdk/constants"
	"github.com/ifx-run/ifx/go-sdk/scratch"
)

func main() {
	frame := solana.MustPublicKeyFromBase58("Fr8dvcgrSYKjpvJd471hQD2QuEjF7656WiEuUSb54obu")
	tape := 1024
	s := scratch.ForPublicFrame(frame, constants.DefaultProgramID, &tape)

	user := solana.NewWallet().PublicKey()
	mintA := solana.NewWallet().PublicKey()
	mintB := solana.NewWallet().PublicKey()
	mintC := solana.NewWallet().PublicKey()
	tokenProg := solana.MustPublicKeyFromBase58("TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA")
	ataA := solana.NewWallet().PublicKey()
	ataB := solana.NewWallet().PublicKey()
	ataC := solana.NewWallet().PublicKey()

	ray, err := raydiumcpmm.NewExactIn(raydiumcpmm.Params{
		User: user, PoolID: solana.NewWallet().PublicKey(),
		Pool: raydiumcpmm.PoolState{
			ConfigID: solana.NewWallet().PublicKey(),
			VaultA: solana.NewWallet().PublicKey(), VaultB: solana.NewWallet().PublicKey(),
			MintA: mintA, MintB: mintB, MintProgramA: tokenProg, MintProgramB: tokenProg,
			ObservationID: solana.NewWallet().PublicKey(),
		},
		InputMint: mintA, OutputMint: mintB,
		UserInputATA: ataA, UserOutputATA: ataB,
	})
	if err != nil {
		log.Fatal(err)
	}
	damm, err := meteoradammv2.NewExactIn(meteoradammv2.Params{
		User: user, PoolID: solana.NewWallet().PublicKey(),
		Pool: meteoradammv2.PoolState{
			MintA: mintB, MintB: mintC,
			VaultA: solana.NewWallet().PublicKey(), VaultB: solana.NewWallet().PublicKey(),
		},
		InputMint: mintB, OutputMint: mintC,
		UserInputATA: ataB, UserOutputATA: ataC,
		TokenProgramA: tokenProg, TokenProgramB: tokenProg,
	})
	if err != nil {
		log.Fatal(err)
	}

	plan, err := orchestrator.New(s, user).
		AmountIn(1_000_000).
		MinAmountOut(900_000).
		Hop(ray).
		Hop(damm).
		Build()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("raydium_cpmm → meteora_damm_v2: %d instructions\n", len(plan.Instructions))
	for i, ix := range plan.Instructions {
		fmt.Printf("  [%d] %s\n", i, ix.ProgramID())
	}
}
