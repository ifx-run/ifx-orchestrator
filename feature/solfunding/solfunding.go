// Package solfunding wraps native SOL into WSOL and unwraps WSOL back (Feature).
package solfunding

import (
	"encoding/binary"
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/programs/system"
	"github.com/ifx-run/ifx-orchestrator/feature"
)

// WSOLMint is native wrapped SOL.
var WSOLMint = solana.MustPublicKeyFromBase58("So11111111111111111111111111111111111111112")

// UnwrapMode selects how WSOL SPL is converted back to native SOL after the route.
type UnwrapMode uint8

const (
	// UnwrapNone skips unwrap.
	UnwrapNone UnwrapMode = iota
	// UnwrapPartial syncs then UnwrapLamports(amount) — keeps the ATA open.
	UnwrapPartial
	// UnwrapLamportsAll syncs then UnwrapLamports(all) — keeps the ATA open (rent stays).
	UnwrapLamportsAll
	// UnwrapClose syncs then CloseAccount — returns token balance + rent to owner.
	UnwrapClose
)

const tokenIxUnwrapLamports byte = 45

// Feature wraps SOL before the route and/or unwraps WSOL after.
//
// Prefer UnwrapLamports (Partial / LamportsAll) when the WSOL ATA should stay open
// for reuse; use UnwrapClose only when you want to reclaim rent.
type Feature struct {
	feature.Base

	TokenProgram solana.PublicKey // zero => Tokenkeg
	WSOLATA      solana.PublicKey // zero => derive ATA(user, WSOL)
	Payer        solana.PublicKey // zero => Ctx.User (wrap create + transfer)

	// WrapLamports > 0 emits create-ATA (idempotent) + System transfer + SyncNative in BeforeRoute.
	WrapLamports uint64
	// CreateATA controls wrap ATA create; default true when WrapLamports > 0.
	SkipCreateATA bool

	Unwrap UnwrapMode
	// UnwrapLamports is the partial amount when Unwrap == UnwrapPartial (0 => error).
	UnwrapAmount uint64
}

// Wrap returns a Feature that wraps lamports into the user's WSOL ATA before the route.
func Wrap(lamports uint64) *Feature {
	return &Feature{WrapLamports: lamports}
}

// UnwrapWSOL returns a Feature that unwraps after the route.
func UnwrapWSOL(mode UnwrapMode) *Feature {
	return &Feature{Unwrap: mode}
}

// WrapAndUnwrap wraps before and unwraps after.
func WrapAndUnwrap(wrapLamports uint64, mode UnwrapMode) *Feature {
	return &Feature{WrapLamports: wrapLamports, Unwrap: mode}
}

func (f *Feature) tokenProgram() solana.PublicKey {
	if f.TokenProgram.IsZero() {
		return solana.TokenProgramID
	}
	return f.TokenProgram
}

func (f *Feature) ata(user solana.PublicKey) (solana.PublicKey, error) {
	if !f.WSOLATA.IsZero() {
		return f.WSOLATA, nil
	}
	return AssociatedTokenAddress(user, WSOLMint, f.tokenProgram())
}

// Phase wraps before the route and unwraps in settlement/cleanup ordering.
func (f *Feature) Phase() feature.Phase { return feature.PhaseSetup }

func (f *Feature) BeforeRoute(cx *feature.Ctx) error {
	if f.WrapLamports == 0 {
		return nil
	}
	payer := f.Payer
	if payer.IsZero() {
		payer = cx.User
	}
	ata, err := f.ata(cx.User)
	if err != nil {
		return err
	}
	tp := f.tokenProgram()
	if !f.SkipCreateATA {
		create, err := CreateATAIdempotent(payer, cx.User, WSOLMint, tp)
		if err != nil {
			return err
		}
		cx.Emit(create)
	}
	cx.Emit(system.NewTransferInstruction(f.WrapLamports, payer, ata).Build())
	cx.Emit(SyncNativeInstruction(ata, tp))
	return nil
}

func (f *Feature) AfterRoute(cx *feature.Ctx) error {
	if f.Unwrap == UnwrapNone {
		return nil
	}
	ata, err := f.ata(cx.User)
	if err != nil {
		return err
	}
	tp := f.tokenProgram()
	cx.Emit(SyncNativeInstruction(ata, tp))
	switch f.Unwrap {
	case UnwrapClose:
		cx.Emit(CloseAccountInstruction(ata, cx.User, cx.User, tp))
	case UnwrapLamportsAll:
		cx.Emit(UnwrapLamportsInstruction(ata, cx.User, cx.User, tp, nil))
	case UnwrapPartial:
		if f.UnwrapAmount == 0 {
			return fmt.Errorf("solfunding: UnwrapAmount required for UnwrapPartial")
		}
		amt := f.UnwrapAmount
		cx.Emit(UnwrapLamportsInstruction(ata, cx.User, cx.User, tp, &amt))
	default:
		return fmt.Errorf("solfunding: unknown UnwrapMode %d", f.Unwrap)
	}
	return nil
}

// AssociatedTokenAddress derives an ATA for owner/mint/tokenProgram.
func AssociatedTokenAddress(owner, mint, tokenProgram solana.PublicKey) (solana.PublicKey, error) {
	pda, _, err := solana.FindProgramAddress(
		[][]byte{owner.Bytes(), tokenProgram.Bytes(), mint.Bytes()},
		solana.SPLAssociatedTokenAccountProgramID,
	)
	return pda, err
}

// CreateATAIdempotent builds Associated Token Program CreateIdempotent.
func CreateATAIdempotent(payer, owner, mint, tokenProgram solana.PublicKey) (solana.Instruction, error) {
	ata, err := AssociatedTokenAddress(owner, mint, tokenProgram)
	if err != nil {
		return nil, err
	}
	return solana.NewInstruction(
		solana.SPLAssociatedTokenAccountProgramID,
		solana.AccountMetaSlice{
			{PublicKey: payer, IsWritable: true, IsSigner: true},
			{PublicKey: ata, IsWritable: true, IsSigner: false},
			{PublicKey: owner, IsWritable: false, IsSigner: false},
			{PublicKey: mint, IsWritable: false, IsSigner: false},
			{PublicKey: solana.SystemProgramID, IsWritable: false, IsSigner: false},
			{PublicKey: tokenProgram, IsWritable: false, IsSigner: false},
		},
		[]byte{1},
	), nil
}

// SyncNativeInstruction marks lamports in a WSOL ATA as SPL balance (discriminator 17).
func SyncNativeInstruction(wsolATA, tokenProgram solana.PublicKey) solana.Instruction {
	return solana.NewInstruction(
		tokenProgram,
		solana.AccountMetaSlice{
			{PublicKey: wsolATA, IsWritable: true, IsSigner: false},
		},
		[]byte{17},
	)
}

// CloseAccountInstruction closes a token account (discriminator 9).
func CloseAccountInstruction(account, destination, owner, tokenProgram solana.PublicKey) solana.Instruction {
	return solana.NewInstruction(
		tokenProgram,
		solana.AccountMetaSlice{
			{PublicKey: account, IsWritable: true, IsSigner: false},
			{PublicKey: destination, IsWritable: true, IsSigner: false},
			{PublicKey: owner, IsWritable: false, IsSigner: true},
		},
		[]byte{9},
	)
}

// UnwrapLamportsInstruction transfers lamports from a native (WSOL) token account to destination.
// amount nil unwraps the entire synced balance; non-nil unwraps a partial amount without closing.
//
// Prefer this over CloseAccount when the ATA should remain open (reuse / keep rent).
func UnwrapLamportsInstruction(
	source, destination, owner, tokenProgram solana.PublicKey,
	amount *uint64,
) solana.Instruction {
	data := []byte{tokenIxUnwrapLamports}
	if amount == nil {
		data = append(data, 0)
	} else {
		data = append(data, 1)
		var buf [8]byte
		binary.LittleEndian.PutUint64(buf[:], *amount)
		data = append(data, buf[:]...)
	}
	return solana.NewInstruction(
		tokenProgram,
		solana.AccountMetaSlice{
			{PublicKey: source, IsWritable: true, IsSigner: false},
			{PublicKey: destination, IsWritable: true, IsSigner: false},
			{PublicKey: owner, IsWritable: false, IsSigner: true},
		},
		data,
	)
}

// UnwrapLamportsAmountOffset is the u64 LE offset when Option::Some is encoded (disc + tag).
const UnwrapLamportsAmountOffset uint16 = 2
