// Package flashrent borrows temporary SOL for ATA rent peaks (Feature).
package flashrent

import (
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/feature"
	"github.com/ifx-run/ifx-orchestrator/rentpeak"
)

// JupiterFlashFillProgramID is the mainnet Jupiter Flash Fill program.
var JupiterFlashFillProgramID = solana.MustPublicKeyFromBase58("JUPLdTqUdKztWJ1isGMV92W2QvmEmzs9WTJjhZe4QdJ")

var (
	discBorrow = []byte{228, 253, 131, 202, 207, 116, 89, 18} // sha256("global:borrow")[:8]
	discRepay  = []byte{234, 103, 67, 82, 208, 234, 219, 166} // sha256("global:repay")[:8]
)

const authoritySeed = "authority"

// RentLiquidityBackend produces borrow/repay instructions for FlashRent.
type RentLiquidityBackend interface {
	BorrowIx(cx *feature.Ctx) (solana.Instruction, error)
	RepayIx(cx *feature.Ctx) (solana.Instruction, error)
	// MaxLend is the maximum lamports this backend can lend (0 => unlimited / unknown).
	MaxLend() uint64
}

// JupiterFlashFill is the default backend (mainnet program JUPLdTq…).
// It always lends exactly one classic TokenAccount rent (program-enforced).
type JupiterFlashFill struct {
	Borrower solana.PublicKey // zero => Ctx.User
}

func (j JupiterFlashFill) MaxLend() uint64 { return rentpeak.DefaultTokenAccountRent }

func (j JupiterFlashFill) authority() (solana.PublicKey, error) {
	pda, _, err := solana.FindProgramAddress([][]byte{[]byte(authoritySeed)}, JupiterFlashFillProgramID)
	return pda, err
}

func (j JupiterFlashFill) borrower(cx *feature.Ctx) solana.PublicKey {
	if !j.Borrower.IsZero() {
		return j.Borrower
	}
	return cx.User
}

func (j JupiterFlashFill) BorrowIx(cx *feature.Ctx) (solana.Instruction, error) {
	auth, err := j.authority()
	if err != nil {
		return nil, err
	}
	b := j.borrower(cx)
	return solana.NewInstruction(
		JupiterFlashFillProgramID,
		solana.AccountMetaSlice{
			{PublicKey: b, IsSigner: true, IsWritable: true},
			{PublicKey: auth, IsSigner: false, IsWritable: true},
			{PublicKey: solana.SysVarInstructionsPubkey, IsSigner: false, IsWritable: false},
			{PublicKey: solana.SystemProgramID, IsSigner: false, IsWritable: false},
		},
		append([]byte(nil), discBorrow...),
	), nil
}

func (j JupiterFlashFill) RepayIx(cx *feature.Ctx) (solana.Instruction, error) {
	auth, err := j.authority()
	if err != nil {
		return nil, err
	}
	b := j.borrower(cx)
	return solana.NewInstruction(
		JupiterFlashFillProgramID,
		solana.AccountMetaSlice{
			{PublicKey: b, IsSigner: true, IsWritable: true},
			{PublicKey: auth, IsSigner: false, IsWritable: true},
			{PublicKey: solana.SysVarInstructionsPubkey, IsSigner: false, IsWritable: false},
			{PublicKey: solana.SystemProgramID, IsSigner: false, IsWritable: false},
		},
		append([]byte(nil), discRepay...),
	), nil
}

// CustomRentLiquidity uses caller-supplied borrow/repay instructions.
type CustomRentLiquidity struct {
	Borrow   solana.Instruction
	Repay    solana.Instruction
	MaxLamports uint64 // 0 => treat as enough for any peak
}

func (c CustomRentLiquidity) MaxLend() uint64 {
	if c.MaxLamports == 0 {
		return ^uint64(0)
	}
	return c.MaxLamports
}
func (c CustomRentLiquidity) BorrowIx(*feature.Ctx) (solana.Instruction, error) { return c.Borrow, nil }
func (c CustomRentLiquidity) RepayIx(*feature.Ctx) (solana.Instruction, error)  { return c.Repay, nil }

// Feature sandwiches borrow → route → repay when the user cannot cover rent peak.
type Feature struct {
	feature.Base
	Backend RentLiquidityBackend
	Always  bool // skip peak gating
	Policy  feature.AtaPolicy // used to compute peak when Ctx.RentPeakReady is false
	active  bool
}

// Auto returns FlashRent with JupiterFlashFill; enables only when userLamports < peak.
func Auto() *Feature {
	return &Feature{Backend: JupiterFlashFill{}, Policy: feature.AtaCreateAndCloseCreated}
}

// AutoWith returns FlashRent with a custom backend.
func AutoWith(b RentLiquidityBackend) *Feature {
	return &Feature{Backend: b, Policy: feature.AtaCreateAndCloseCreated}
}

func (f *Feature) BeforeRoute(cx *feature.Ctx) error {
	if f.Backend == nil {
		return fmt.Errorf("flashrent: backend is required")
	}
	peak, err := f.peak(cx)
	if err != nil {
		return err
	}
	need := uint64(0)
	if peak > cx.UserLamports {
		need = peak - cx.UserLamports
	}
	if !f.Always && need == 0 {
		f.active = false
		return nil
	}
	max := f.Backend.MaxLend()
	if need > max {
		return fmt.Errorf(
			"flashrent: need %d lamports to clear peak %d but backend max lend is %d; use CustomRentLiquidity",
			need, peak, max,
		)
	}
	ix, err := f.Backend.BorrowIx(cx)
	if err != nil {
		return fmt.Errorf("flashrent borrow: %w", err)
	}
	if ix != nil {
		cx.Emit(ix)
	}
	f.active = true
	return nil
}

func (f *Feature) AfterRoute(cx *feature.Ctx) error {
	if !f.active {
		return nil
	}
	ix, err := f.Backend.RepayIx(cx)
	if err != nil {
		return fmt.Errorf("flashrent repay: %w", err)
	}
	if ix != nil {
		cx.Emit(ix)
	}
	return nil
}

func (f *Feature) peak(cx *feature.Ctx) (uint64, error) {
	if cx.RentPeakReady {
		return cx.RentPeakPeakReserve, nil
	}
	est, err := rentpeak.EstimateForRoute(rentpeak.Params{
		AmountIn:         cx.AmountIn,
		Policy:           f.Policy,
		Route:            cx.Route,
		TokenAccountRent: cx.TokenAccountRent,
	})
	if err != nil {
		return 0, err
	}
	cx.RentPeakPeakReserve = est.PeakReserve
	cx.RentPeakReady = true
	return est.PeakReserve, nil
}
