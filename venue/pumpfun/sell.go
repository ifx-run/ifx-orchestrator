package pumpfun

import (
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/hop"
)

// SellParams configures a native SOL ExactIn sell.
//
// Sell pays native SOL to the user wallet (not a WSOL ATA). Treat as a terminal hop:
// OutputMeasureAccount is the user base ATA (input side) for identity only — chaining
// via SplTokenAmount after sell will not capture SOL proceeds.
type SellParams struct {
	User             solana.PublicKey
	BaseMint         solana.PublicKey
	BaseTokenProgram solana.PublicKey // zero => Tokenkeg
	Curve            CurveMeta
	UserBaseATA      solana.PublicKey // zero => derive
}

// SellExactIn is Pump.fun ExactIn: base token → SOL.
type SellExactIn struct {
	p            SellParams
	userBaseATA  solana.PublicKey
	tokenProgram solana.PublicKey
}

// NewSellExactIn validates and returns a sell hop.
func NewSellExactIn(p SellParams) (*SellExactIn, error) {
	if p.User.IsZero() || p.BaseMint.IsZero() || p.Curve.Creator.IsZero() {
		return nil, fmt.Errorf("pumpfun sell: User, BaseMint, and Curve.Creator are required")
	}
	tp := p.BaseTokenProgram
	if tp.IsZero() {
		tp = solana.TokenProgramID
	}
	ata := p.UserBaseATA
	if ata.IsZero() {
		var err error
		ata, err = ataAddress(p.User, p.BaseMint, tp)
		if err != nil {
			return nil, err
		}
	}
	return &SellExactIn{p: p, userBaseATA: ata, tokenProgram: tp}, nil
}

func (e *SellExactIn) VenueID() string                        { return "pumpfun_sell" }
func (e *SellExactIn) InputMint() solana.PublicKey            { return e.p.BaseMint }
func (e *SellExactIn) OutputMint() solana.PublicKey           { return NativeSOL }
func (e *SellExactIn) OutputMeasureAccount() solana.PublicKey { return e.userBaseATA }

func (e *SellExactIn) BuildBlueprint(_ *hop.HopBuildCtx) (hop.HopBlueprint, error) {
	program := ProgramID
	globalPK, err := GlobalPDA()
	if err != nil {
		return hop.HopBlueprint{}, err
	}
	bcPK, err := BondingCurvePDA(e.p.BaseMint)
	if err != nil {
		return hop.HopBlueprint{}, err
	}
	assocBC, err := ataAddress(bcPK, e.p.BaseMint, e.tokenProgram)
	if err != nil {
		return hop.HopBlueprint{}, err
	}
	creatorVault, err := CreatorVaultPDA(e.p.Curve.Creator)
	if err != nil {
		return hop.HopBlueprint{}, err
	}
	eventAuth, err := EventAuthorityPDA()
	if err != nil {
		return hop.HopBlueprint{}, err
	}
	feeConfig, err := FeeConfigPDA()
	if err != nil {
		return hop.HopBlueprint{}, err
	}
	bcV2, err := BondingCurveV2PDA(e.p.BaseMint)
	if err != nil {
		return hop.HopBlueprint{}, err
	}

	data := make([]byte, 24)
	copy(data[:8], discSell[:])
	putU64LE(data[8:16], 0)
	putU64LE(data[16:24], 0)

	accounts := solana.AccountMetaSlice{
		{PublicKey: globalPK, IsWritable: false, IsSigner: false},
		{PublicKey: PickFeeRecipient(e.p.Curve.IsMayhemMode), IsWritable: true, IsSigner: false},
		{PublicKey: e.p.BaseMint, IsWritable: false, IsSigner: false},
		{PublicKey: bcPK, IsWritable: true, IsSigner: false},
		{PublicKey: assocBC, IsWritable: true, IsSigner: false},
		{PublicKey: e.userBaseATA, IsWritable: true, IsSigner: false},
		{PublicKey: e.p.User, IsWritable: true, IsSigner: true},
		{PublicKey: solana.SystemProgramID, IsWritable: false, IsSigner: false},
		{PublicKey: creatorVault, IsWritable: true, IsSigner: false},
		{PublicKey: e.tokenProgram, IsWritable: false, IsSigner: false},
		{PublicKey: eventAuth, IsWritable: false, IsSigner: false},
		{PublicKey: program, IsWritable: false, IsSigner: false},
		{PublicKey: feeConfig, IsWritable: false, IsSigner: false},
		{PublicKey: FeeProgramID, IsWritable: false, IsSigner: false},
	}
	if e.p.Curve.CashbackEnabled {
		uva, err := UserVolumeAccumulatorPDA(e.p.User)
		if err != nil {
			return hop.HopBlueprint{}, err
		}
		accounts = append(accounts, &solana.AccountMeta{
			PublicKey: uva, IsWritable: true, IsSigner: false,
		})
	}
	accounts = append(accounts,
		&solana.AccountMeta{PublicKey: bcV2, IsWritable: false, IsSigner: false},
		&solana.AccountMeta{PublicKey: PickBuybackFeeRecipient(), IsWritable: true, IsSigner: false},
	)

	minOff := MinOutOffset
	ix := solana.NewInstruction(program, accounts, data)
	return hop.HopBlueprint{
		Template: ix,
		AmountIn: hop.PatchSite{Offset: AmountInOffset},
		MinOut:   &hop.PatchSite{Offset: minOff},
	}, nil
}

var _ hop.ExactInHop = (*SellExactIn)(nil)
