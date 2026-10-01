// Command mock_two_hop shows the batteries-included Router surface with mock venues.
package main

import (
	"fmt"
	"log"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/orchestrator"
	"github.com/ifx-run/ifx-orchestrator/venue/mock"
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
	ataB := solana.NewWallet().PublicKey()
	ataC := solana.NewWallet().PublicKey()
	prog1 := solana.NewWallet().PublicKey()
	prog2 := solana.NewWallet().PublicKey()

	acc := func(extra solana.PublicKey) []*solana.AccountMeta {
		return []*solana.AccountMeta{
			{PublicKey: user, IsSigner: true, IsWritable: true},
			{PublicKey: extra, IsWritable: true},
		}
	}

	plan, err := orchestrator.New(s, user).
		AmountIn(1_000_000).
		MinAmountOut(900_000).
		Hop(mock.New("mock_raydium", prog1, mintA, mintB, ataB, acc(ataB))).
		Hop(mock.New("mock_meteora", prog2, mintB, mintC, ataC, acc(ataC))).
		Build()
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("compiled %d instructions (reset + lets + patched CPIs)\n", len(plan.Instructions))
	for i, ix := range plan.Instructions {
		fmt.Printf("  [%d] program=%s accounts=%d\n", i, ix.ProgramID(), len(ix.Accounts()))
	}
}
