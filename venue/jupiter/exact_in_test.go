package jupiter

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/hop"
	"github.com/ifx-run/ifx-orchestrator/orchestrator"
	"github.com/ifx-run/ifx-orchestrator/venue/mock"
	"github.com/ifx-run/ifx/go-sdk/constants"
	"github.com/ifx-run/ifx/go-sdk/scratch"
)

func routeData(in, quoted uint64) []byte {
	data := make([]byte, 8+4+exactInTail)
	copy(data, discRoute[:])
	binary.LittleEndian.PutUint32(data[8:12], 0)
	binary.LittleEndian.PutUint64(data[12:20], in)
	binary.LittleEndian.PutUint64(data[20:28], quoted)
	binary.LittleEndian.PutUint16(data[28:30], 50)
	data[30] = 0
	return data
}

func sharedData(in, quoted uint64) []byte {
	data := make([]byte, 8+1+4+exactInTail)
	copy(data, discSharedAccountsRoute[:])
	data[8] = 1
	binary.LittleEndian.PutUint32(data[9:13], 0)
	binary.LittleEndian.PutUint64(data[13:21], in)
	binary.LittleEndian.PutUint64(data[21:29], quoted)
	binary.LittleEndian.PutUint16(data[29:31], 50)
	data[31] = 0
	return data
}

func TestExactInOffsetsRouteAndShared(t *testing.T) {
	in, min, err := ExactInOffsets(routeData(111, 222))
	if err != nil {
		t.Fatal(err)
	}
	if in != 12 || min != 20 {
		t.Fatalf("route offsets in=%d min=%d", in, min)
	}
	in, min, err = ExactInOffsets(sharedData(111, 222))
	if err != nil {
		t.Fatal(err)
	}
	if in != 13 || min != 21 {
		t.Fatalf("shared offsets in=%d min=%d", in, min)
	}
}

func TestExactInOffsetsRejectsUnsupported(t *testing.T) {
	ledger := make([]byte, 64)
	copy(ledger, discRouteTokenLedger[:])
	if _, _, err := ExactInOffsets(ledger); err == nil {
		t.Fatal("expected token-ledger error")
	}
	exactOut := make([]byte, 64)
	copy(exactOut, discExactOutRoute[:])
	if _, _, err := ExactInOffsets(exactOut); err == nil {
		t.Fatal("expected exact-out error")
	}
}

func TestExactInBlueprintZerosAmount(t *testing.T) {
	user := solana.NewWallet().PublicKey()
	inMint := solana.NewWallet().PublicKey()
	outMint := solana.NewWallet().PublicKey()
	outATA := solana.NewWallet().PublicKey()
	ix := solana.NewInstruction(ProgramID, []*solana.AccountMeta{
		{PublicKey: user, IsSigner: true, IsWritable: false},
		{PublicKey: outATA, IsSigner: false, IsWritable: true},
	}, routeData(99, 1000))

	h, err := NewExactIn(Params{
		InputMint: inMint, OutputMint: outMint, UserOutputATA: outATA, Swap: ix,
	})
	if err != nil {
		t.Fatal(err)
	}
	if h.VenueID() != "jupiter_v6" {
		t.Fatalf("venue %s", h.VenueID())
	}
	bp, err := h.BuildBlueprint(&hop.HopBuildCtx{User: user})
	if err != nil {
		t.Fatal(err)
	}
	if bp.AmountIn.Offset != 12 || bp.MinOut == nil || bp.MinOut.Offset != 20 {
		t.Fatalf("patch sites %+v", bp)
	}
	data, err := bp.Template.Data()
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint64(data[12:20]) != 0 {
		t.Fatal("amount_in should be zeroed")
	}
	if binary.LittleEndian.Uint64(data[20:28]) != 1000 {
		t.Fatal("quoted_out should remain until compile min_out patch")
	}
}

func TestInstructionFromAPI(t *testing.T) {
	raw := routeData(7, 8)
	payload, _ := json.Marshal(APIInstruction{
		ProgramID: ProgramID.String(),
		Accounts: []APIAccount{
			{Pubkey: solana.NewWallet().PublicKey().String(), IsSigner: true},
		},
		Data: base64.StdEncoding.EncodeToString(raw),
	})
	ix, err := InstructionFromAPI(payload)
	if err != nil {
		t.Fatal(err)
	}
	if ix.ProgramID() != ProgramID {
		t.Fatalf("program %s", ix.ProgramID())
	}
	got, _ := ix.Data()
	if binary.LittleEndian.Uint64(got[12:20]) != 7 {
		t.Fatalf("decoded amount %d", binary.LittleEndian.Uint64(got[12:20]))
	}
}

func TestCompileOwnHopThenJupiter(t *testing.T) {
	frame := solana.MustPublicKeyFromBase58("Fr8dvcgrSYKjpvJd471hQD2QuEjF7656WiEuUSb54obu")
	tape := 2048
	s := scratch.ForPublicFrame(frame, constants.DefaultProgramID, &tape)

	user := solana.NewWallet().PublicKey()
	mintA := solana.NewWallet().PublicKey()
	mintB := solana.NewWallet().PublicKey()
	mintC := solana.NewWallet().PublicKey()
	ataB := solana.NewWallet().PublicKey()
	ataC := solana.NewWallet().PublicKey()
	prog := solana.NewWallet().PublicKey()
	acc := []*solana.AccountMeta{{PublicKey: user, IsSigner: true, IsWritable: true}}

	jupIx := solana.NewInstruction(ProgramID, []*solana.AccountMeta{
		{PublicKey: user, IsSigner: true},
		{PublicKey: ataC, IsWritable: true},
	}, sharedData(1, 2))
	jup, err := NewExactIn(Params{
		InputMint: mintB, OutputMint: mintC, UserOutputATA: ataC, Swap: jupIx,
	})
	if err != nil {
		t.Fatal(err)
	}

	plan, err := orchestrator.New(s, user).
		AmountIn(1_000_000).
		MinAmountOut(1).
		Hop(mock.New("own_leg", prog, mintA, mintB, ataB, acc)).
		Hop(jup).
		Build()
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Instructions) < 5 {
		t.Fatalf("ix count %d", len(plan.Instructions))
	}
}

func TestRejectsWrongProgram(t *testing.T) {
	ix := solana.NewInstruction(solana.NewWallet().PublicKey(), nil, routeData(1, 2))
	_, err := NewExactIn(Params{
		InputMint:     solana.NewWallet().PublicKey(),
		OutputMint:    solana.NewWallet().PublicKey(),
		UserOutputATA: solana.NewWallet().PublicKey(),
		Swap:          ix,
	})
	if err == nil {
		t.Fatal("expected wrong program")
	}
}
