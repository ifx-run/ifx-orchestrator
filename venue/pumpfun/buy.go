package pumpfun

import (
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/hop"
)

// BuyParams configures a native SOL ExactIn buy (buy_exact_sol_in).
type BuyParams struct {
	User             solana.PublicKey
	BaseMint         solana.PublicKey
	BaseTokenProgram solana.PublicKey // zero => Tokenkeg
	Curve            CurveMeta
	UserBaseATA      solana.PublicKey // zero => derive
}

// BuyExactSolIn is Pump.fun ExactIn: SOL → base token.
type BuyExactSolIn struct {
	p            BuyParams
	userBaseATA  solana.PublicKey
	tokenProgram solana.PublicKey
}

// NewBuyExactSolIn validates and returns a buy hop.
func NewBuyExactSolIn(p BuyParams) (*BuyExactSolIn, error) {
	if p.User.IsZero() || p.BaseMint.IsZero() || p.Curve.Creator.IsZero() {
		return nil, fmt.Errorf("pumpfun buy: User, BaseMint, and Curve.Creator are required")
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
	return &BuyExactSolIn{p: p, userBaseATA: ata, tokenProgram: tp}, nil
}

func (e *BuyExactSolIn) VenueID() string                        { return "pumpfun_buy_exact_sol_in" }
func (e *BuyExactSolIn) InputMint() solana.PublicKey            { return NativeSOL }
func (e *BuyExactSolIn) OutputMint() solana.PublicKey           { return e.p.BaseMint }
func (e *BuyExactSolIn) OutputMeasureAccount() solana.PublicKey { return e.userBaseATA }

func (e *BuyExactSolIn) BuildBlueprint(_ *hop.HopBuildCtx) (hop.HopBlueprint, error) {
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
	gva, err := GlobalVolumeAccumulatorPDA()
	if err != nil {
		return hop.HopBlueprint{}, err
	}
	uva, err := UserVolumeAccumulatorPDA(e.p.User)
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
	copy(data[:8], discBuyExactSolIn[:])
	putU64LE(data[8:16], 0)
	putU64LE(data[16:24], 0)

	minOff := MinOutOffset
	ix := solana.NewInstruction(program, solana.AccountMetaSlice{
		{PublicKey: globalPK, IsWritable: false, IsSigner: false},
		{PublicKey: PickFeeRecipient(e.p.Curve.IsMayhemMode), IsWritable: true, IsSigner: false},
		{PublicKey: e.p.BaseMint, IsWritable: false, IsSigner: false},
		{PublicKey: bcPK, IsWritable: true, IsSigner: false},
		{PublicKey: assocBC, IsWritable: true, IsSigner: false},
		{PublicKey: e.userBaseATA, IsWritable: true, IsSigner: false},
		{PublicKey: e.p.User, IsWritable: true, IsSigner: true},
		{PublicKey: solana.SystemProgramID, IsWritable: false, IsSigner: false},
		{PublicKey: e.tokenProgram, IsWritable: false, IsSigner: false},
		{PublicKey: creatorVault, IsWritable: true, IsSigner: false},
		{PublicKey: eventAuth, IsWritable: false, IsSigner: false},
		{PublicKey: program, IsWritable: false, IsSigner: false},
		{PublicKey: gva, IsWritable: false, IsSigner: false},
		{PublicKey: uva, IsWritable: true, IsSigner: false},
		{PublicKey: feeConfig, IsWritable: false, IsSigner: false},
		{PublicKey: FeeProgramID, IsWritable: false, IsSigner: false},
		{PublicKey: bcV2, IsWritable: false, IsSigner: false},
		{PublicKey: PickBuybackFeeRecipient(), IsWritable: true, IsSigner: false},
	}, data)

	return hop.HopBlueprint{
		Template: ix,
		AmountIn: hop.PatchSite{Offset: AmountInOffset},
		MinOut:   &hop.PatchSite{Offset: minOff},
	}, nil
}

var _ hop.ExactInHop = (*BuyExactSolIn)(nil)
