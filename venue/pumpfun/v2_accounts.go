package pumpfun

import (
	"fmt"

	"github.com/gagliardetto/solana-go"
)

func resolveQuoteV2Accounts(p BuyQuoteV2Params, userBaseATA, userQuoteATA solana.PublicKey) (solana.AccountMetaSlice, error) {
	baseTP := p.BaseTokenProgram
	if baseTP.IsZero() {
		baseTP = solana.TokenProgramID
	}
	quoteTP := p.QuoteTokenProgram
	if quoteTP.IsZero() {
		quoteTP = solana.TokenProgramID
	}

	program := ProgramID
	globalPK, err := GlobalPDA()
	if err != nil {
		return nil, err
	}
	bcPK, err := BondingCurvePDA(p.BaseMint)
	if err != nil {
		return nil, err
	}
	assocBC, err := ataAddress(bcPK, p.BaseMint, baseTP)
	if err != nil {
		return nil, err
	}
	assocQuoteBC, err := ataAddress(bcPK, p.QuoteMint, quoteTP)
	if err != nil {
		return nil, err
	}
	creatorVault, err := CreatorVaultPDA(p.Curve.Creator)
	if err != nil {
		return nil, err
	}
	assocCreatorVault, err := ataAddress(creatorVault, p.QuoteMint, quoteTP)
	if err != nil {
		return nil, err
	}
	sharingConfig, err := SharingConfigPDA(p.BaseMint)
	if err != nil {
		return nil, err
	}
	gva, err := GlobalVolumeAccumulatorPDA()
	if err != nil {
		return nil, err
	}
	uva, err := UserVolumeAccumulatorPDA(p.User)
	if err != nil {
		return nil, err
	}
	assocUVA, err := ataAddress(uva, p.QuoteMint, quoteTP)
	if err != nil {
		return nil, err
	}
	feeConfig, err := FeeConfigPDA()
	if err != nil {
		return nil, err
	}
	eventAuth, err := EventAuthorityPDA()
	if err != nil {
		return nil, err
	}
	pumpFeeRecipient := PickFeeRecipient(p.Curve.IsMayhemMode)
	assocQuoteFeeRecipient, err := ataAddress(pumpFeeRecipient, p.QuoteMint, quoteTP)
	if err != nil {
		return nil, err
	}
	buybackFeeRecipient := PickBuybackFeeRecipient()
	assocQuoteBuybackFeeRecipient, err := ataAddress(buybackFeeRecipient, p.QuoteMint, quoteTP)
	if err != nil {
		return nil, err
	}

	metas := solana.AccountMetaSlice{
		{PublicKey: globalPK, IsWritable: false, IsSigner: false},
		{PublicKey: p.BaseMint, IsWritable: false, IsSigner: false},
		{PublicKey: p.QuoteMint, IsWritable: false, IsSigner: false},
		{PublicKey: baseTP, IsWritable: false, IsSigner: false},
		{PublicKey: quoteTP, IsWritable: false, IsSigner: false},
		{PublicKey: solana.SPLAssociatedTokenAccountProgramID, IsWritable: false, IsSigner: false},
		{PublicKey: pumpFeeRecipient, IsWritable: true, IsSigner: false},
		{PublicKey: assocQuoteFeeRecipient, IsWritable: true, IsSigner: false},
		{PublicKey: buybackFeeRecipient, IsWritable: true, IsSigner: false},
		{PublicKey: assocQuoteBuybackFeeRecipient, IsWritable: true, IsSigner: false},
		{PublicKey: bcPK, IsWritable: true, IsSigner: false},
		{PublicKey: assocBC, IsWritable: true, IsSigner: false},
		{PublicKey: assocQuoteBC, IsWritable: true, IsSigner: false},
		{PublicKey: p.User, IsWritable: true, IsSigner: true},
		{PublicKey: userBaseATA, IsWritable: true, IsSigner: false},
		{PublicKey: userQuoteATA, IsWritable: true, IsSigner: false},
		{PublicKey: creatorVault, IsWritable: true, IsSigner: false},
		{PublicKey: assocCreatorVault, IsWritable: true, IsSigner: false},
		{PublicKey: sharingConfig, IsWritable: false, IsSigner: false},
		{PublicKey: gva, IsWritable: false, IsSigner: false},
		{PublicKey: uva, IsWritable: true, IsSigner: false},
		{PublicKey: assocUVA, IsWritable: true, IsSigner: false},
		{PublicKey: feeConfig, IsWritable: false, IsSigner: false},
		{PublicKey: FeeProgramID, IsWritable: false, IsSigner: false},
		{PublicKey: solana.SystemProgramID, IsWritable: false, IsSigner: false},
		{PublicKey: eventAuth, IsWritable: false, IsSigner: false},
		{PublicKey: program, IsWritable: false, IsSigner: false},
	}
	// Mainnet upgrade remaining account (required for cashback and non-cashback).
	bcV2, err := BondingCurveV2PDA(p.BaseMint)
	if err != nil {
		return nil, err
	}
	metas = append(metas, &solana.AccountMeta{PublicKey: bcV2, IsWritable: false, IsSigner: false})
	return metas, nil
}

// trimSellV2Accounts drops buy-only global volume accumulator (gva).
func trimSellV2Accounts(accts solana.AccountMetaSlice) solana.AccountMetaSlice {
	const buyV2Count = 28 // IDL 27 + bonding_curve_v2
	const sellV2Count = 27
	const gvaIndex = 19
	if len(accts) != buyV2Count {
		return accts
	}
	out := make(solana.AccountMetaSlice, 0, sellV2Count)
	for i, m := range accts {
		if i == gvaIndex {
			continue
		}
		out = append(out, m)
	}
	if len(out) != sellV2Count {
		panic(fmt.Sprintf("sell v2 account trim: got %d", len(out)))
	}
	return out
}
