package feehook_test

import (
	"encoding/binary"
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/ifx-run/ifx-orchestrator/feature/feehook"
	"github.com/ifx-run/ifx-orchestrator/orchestrator"
	"github.com/ifx-run/ifx-orchestrator/venue/mock"
	"github.com/ifx-run/ifx/go-sdk/constants"
	"github.com/ifx-run/ifx/go-sdk/scratch"
)

func TestCustomSolSettler(t *testing.T) {
	frame := solana.MustPublicKeyFromBase58("Fr8dvcgrSYKjpvJd471hQD2QuEjF7656WiEuUSb54obu")
	tape := 1024
	s := scratch.ForPublicFrame(frame, constants.DefaultProgramID, &tape)
	user := solana.NewWallet().PublicKey()
	feeProg := solana.NewWallet().PublicKey()
	treasury := solana.NewWallet().PublicKey()

	// Fake fee program: [disc:8][amount:u64] — amount at offset 8.
	data := make([]byte, 16)
	copy(data[:8], []byte{1, 2, 3, 4, 5, 6, 7, 8})
	binary.LittleEndian.PutUint64(data[8:], 0)
	template := solana.NewInstruction(feeProg, solana.AccountMetaSlice{
		{PublicKey: user, IsSigner: true, IsWritable: true},
		{PublicKey: treasury, IsWritable: true, IsSigner: false},
	}, data)

	h := mockHop(t, user)
	plan, err := orchestrator.New(s, user).
		AmountIn(1000).
		Feature(feehook.Fixed(treasury, 42_000).WithSolSettler(feehook.CustomIx(template, 8))).
		Hop(h).
		Build()
	if err != nil {
		t.Fatal(err)
	}
	foundFee := false
	for _, ix := range plan.Instructions {
		if ix.ProgramID().Equals(constants.DefaultProgramID) {
			t.Fatal("fixed custom fee must not use ifx")
		}
		if ix.ProgramID().Equals(feeProg) {
			foundFee = true
			got, err := ix.Data()
			if err != nil {
				t.Fatal(err)
			}
			if binary.LittleEndian.Uint64(got[8:]) != 42_000 {
				t.Fatalf("baked fee %d", binary.LittleEndian.Uint64(got[8:]))
			}
		}
	}
	if !foundFee {
		t.Fatal("expected baked custom fee instruction")
	}
}

func TestSingleHopProceedsBpsUsesIfx(t *testing.T) {
	frame := solana.MustPublicKeyFromBase58("Fr8dvcgrSYKjpvJd471hQD2QuEjF7656WiEuUSb54obu")
	tape := 2048
	s := scratch.ForPublicFrame(frame, constants.DefaultProgramID, &tape)
	user := solana.NewWallet().PublicKey()
	treasury := solana.NewWallet().PublicKey()
	plan, err := orchestrator.New(s, user).
		AmountIn(1000).
		Feature(feehook.ProceedsBPS(treasury, 30)).
		Hop(mockHop(t, user)).
		Build()
	if err != nil {
		t.Fatal(err)
	}
	if !hasIfx(plan.Instructions) {
		t.Fatal("single-hop bps fee must emit ifx (measure Δ then patch transfer)")
	}
	if plan.Instructions[0].ProgramID() != constants.DefaultProgramID {
		t.Fatal("ifx reset should be first when Frame is used")
	}
}

func TestSingleHopProceedsBpsNeedsScratch(t *testing.T) {
	user := solana.NewWallet().PublicKey()
	_, err := orchestrator.New(nil, user).
		AmountIn(1000).
		Feature(feehook.ProceedsBPS(solana.NewWallet().PublicKey(), 30)).
		Hop(mockHop(t, user)).
		Build()
	if err == nil {
		t.Fatal("bps fee without scratch should fail")
	}
}

func TestSingleHopTokenBpsUsesIfx(t *testing.T) {
	frame := solana.MustPublicKeyFromBase58("Fr8dvcgrSYKjpvJd471hQD2QuEjF7656WiEuUSb54obu")
	tape := 2048
	s := scratch.ForPublicFrame(frame, constants.DefaultProgramID, &tape)
	user := solana.NewWallet().PublicKey()
	recv := solana.NewWallet().PublicKey()
	h := mockHop(t, user)
	plan, err := orchestrator.New(s, user).
		AmountIn(1000).
		Feature(feehook.AtNode(1, recv).WithTokenBPS(50, recv)).
		Hop(h).
		Build()
	if err != nil {
		t.Fatal(err)
	}
	if !hasIfx(plan.Instructions) {
		t.Fatal("single-hop token bps must emit ifx")
	}
}

func TestRejectFixedAndBpsSameFeature(t *testing.T) {
	frame := solana.MustPublicKeyFromBase58("Fr8dvcgrSYKjpvJd471hQD2QuEjF7656WiEuUSb54obu")
	tape := 1024
	s := scratch.ForPublicFrame(frame, constants.DefaultProgramID, &tape)
	user := solana.NewWallet().PublicKey()
	treasury := solana.NewWallet().PublicKey()
	_, err := orchestrator.New(s, user).
		AmountIn(1000).
		Feature(feehook.ProceedsBPS(treasury, 30).WithFixed(1_000)).
		Hop(mockHop(t, user)).
		Build()
	if err == nil {
		t.Fatal("expected reject combining Fixed and ProceedsBps on one Feature")
	}
}

func TestAfterRouteOrderFixedThenProceeds(t *testing.T) {
	// AfterRoute is reversed within Settlement ⇒ register Proceeds then Fixed
	// so Fixed System transfer runs before Proceeds patched transfer.
	frame := solana.MustPublicKeyFromBase58("Fr8dvcgrSYKjpvJd471hQD2QuEjF7656WiEuUSb54obu")
	tape := 4096
	s := scratch.ForPublicFrame(frame, constants.DefaultProgramID, &tape)
	user := solana.NewWallet().PublicKey()
	treasury := solana.NewWallet().PublicKey()
	plan, err := orchestrator.New(s, user).
		AmountIn(1000).
		Feature(feehook.ProceedsBPS(treasury, 30)).
		Feature(feehook.Fixed(treasury, 7_000)).
		Hop(mockHop(t, user)).
		Build()
	if err != nil {
		t.Fatal(err)
	}
	var fixedIdx, bpsIdx = -1, -1
	for i, ix := range plan.Instructions {
		if ix.ProgramID().Equals(solana.SystemProgramID) {
			data, _ := ix.Data()
			if len(data) >= 12 && binary.LittleEndian.Uint64(data[4:12]) == 7_000 {
				fixedIdx = i
			}
		}
		if ix.ProgramID().Equals(constants.DefaultProgramID) {
			data, _ := ix.Data()
			if len(data) > 0 && data[0] == constants.IxDiscPatchedCpi {
				bpsIdx = i
			}
		}
	}
	if fixedIdx < 0 || bpsIdx < 0 {
		t.Fatalf("fixed=%d bpsCpi=%d", fixedIdx, bpsIdx)
	}
	if fixedIdx > bpsIdx {
		t.Fatalf("want Fixed before Proceeds CPI, fixed=%d bps=%d", fixedIdx, bpsIdx)
	}
}

func TestMidGraphOrderFixedThenTokenBps(t *testing.T) {
	frame := solana.MustPublicKeyFromBase58("Fr8dvcgrSYKjpvJd471hQD2QuEjF7656WiEuUSb54obu")
	tape := 8192
	s := scratch.ForPublicFrame(frame, constants.DefaultProgramID, &tape)
	user := solana.NewWallet().PublicKey()
	treasury := solana.NewWallet().PublicKey()
	feeATA := solana.NewWallet().PublicKey()
	a, b, c := solana.NewWallet().PublicKey(), solana.NewWallet().PublicKey(), solana.NewWallet().PublicKey()
	ataB, ataC := solana.NewWallet().PublicKey(), solana.NewWallet().PublicKey()
	p := solana.NewWallet().PublicKey()
	acc := []*solana.AccountMeta{{PublicKey: user, IsSigner: true, IsWritable: true}}
	plan, err := orchestrator.New(s, user).
		AmountIn(1_000_000).
		Feature(feehook.AtNode(1, treasury).WithFixed(9_999)).
		Feature(feehook.AtNode(1, treasury).WithTokenBPS(50, feeATA)).
		Hop(mock.New("ab", p, a, b, ataB, acc)).
		Hop(mock.New("bc", p, b, c, ataC, acc)).
		Build()
	if err != nil {
		t.Fatal(err)
	}
	var fixedIdx, tokenFeeIdx = -1, -1
	for i, ix := range plan.Instructions {
		if ix.ProgramID().Equals(solana.SystemProgramID) {
			data, _ := ix.Data()
			if len(data) >= 12 && binary.LittleEndian.Uint64(data[4:12]) == 9_999 {
				fixedIdx = i
			}
		}
		if ix.ProgramID().Equals(constants.DefaultProgramID) {
			data, _ := ix.Data()
			if len(data) > 0 && data[0] == constants.IxDiscPatchedCpi {
				// first patched CPI after hop0 is token fee; hop1 venue is second
				if tokenFeeIdx < 0 {
					tokenFeeIdx = i
				}
			}
		}
	}
	if fixedIdx < 0 || tokenFeeIdx < 0 {
		t.Fatalf("fixed=%d tokenFee=%d", fixedIdx, tokenFeeIdx)
	}
	if fixedIdx > tokenFeeIdx {
		t.Fatalf("want Fixed before TokenBps CPI, fixed=%d token=%d", fixedIdx, tokenFeeIdx)
	}
}

func hasIfx(ixs []solana.Instruction) bool {
	for _, ix := range ixs {
		if ix.ProgramID().Equals(constants.DefaultProgramID) {
			return true
		}
	}
	return false
}

func mockHop(t *testing.T, user solana.PublicKey) *mock.ExactIn {
	t.Helper()
	in := solana.NewWallet().PublicKey()
	out := solana.NewWallet().PublicKey()
	ata := solana.NewWallet().PublicKey()
	prog := solana.NewWallet().PublicKey()
	return mock.New("mock", prog, in, out, ata, solana.AccountMetaSlice{
		{PublicKey: user, IsSigner: true, IsWritable: true},
	})
}
