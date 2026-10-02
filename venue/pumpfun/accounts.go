// Package pumpfun implements ExactInHop for Pump.fun native SOL buy/sell.
package pumpfun

import (
	"encoding/binary"
	"fmt"

	"github.com/gagliardetto/solana-go"
)

const (
	ProgramIDBase58    = "6EF8rrecthR5Dkzon8Nwu78hRvfCKubJ14M5uBEwF6P"
	FeeProgramIDBase58 = "pfeeUxB6jkeY1Hxd7CsFCAjcbHA9rWtchMGdZ6VojVZ"
)

// ProgramID is the Pump.fun program on mainnet.
var ProgramID = solana.MustPublicKeyFromBase58(ProgramIDBase58)

// FeeProgramID is the Pump fee program.
var FeeProgramID = solana.MustPublicKeyFromBase58(FeeProgramIDBase58)

// NativeSOL is the WSOL mint used as the quote-side mint identity for native SOL hops.
var NativeSOL = solana.MustPublicKeyFromBase58("So11111111111111111111111111111111111111112")

var (
	discBuyExactSolIn = [8]byte{56, 252, 116, 8, 158, 223, 205, 95}
	discSell          = [8]byte{51, 230, 133, 164, 1, 127, 131, 173}
	feeConfigConst    = []byte{
		1, 86, 224, 246, 147, 102, 90, 207, 68, 219, 21, 104, 191, 23,
		91, 170, 81, 137, 203, 151, 245, 210, 255, 59, 101, 93, 43,
		182, 253, 109, 24, 176,
	}
	pumpFeeRecipients = []string{
		"62qc2CNXwrYqQScmEdiZFFAnJR262PxWEuNQtxfafNgV",
		"7VtfL8fvgNfhz17qKRMjzQEXgbdpnHHHQRh54R9jP2RJ",
		"7hTckgnGnLQR6sdH7YkqFTAA7VwTfYFaZ6EhEsU3saCX",
		"9rPYyANsfQZw3DnDmKE3YCQF5E8oD89UXoHn9JFEhJUz",
		"AVmoTthdrX6tKt4nDjco2D775W2YK3sDhxPcMmzUAmTY",
		"CebN5WGQ4jvEPvsVU4EoHEpgzq1VV7AbicfhtW4xC9iM",
		"FWsW1xNtWscwNmKv6wVsU1iTzRN6wmmk3MjxRP5tT7hz",
		"G5UZAVbAf46s7cKWoyKu8kYTip9DGTpbLZ2qa9Aq69dP",
	}
	pumpReservedFeeRecipients = []string{
		"GesfTA3X2arioaHp8bbKdjG9vJtskViWACZoYvxp4twS",
		"4budycTjhs9fD6xw62VBducVTNgMgJJ5BgtKq7mAZwn6",
		"8SBKzEQU4nLSzcwF4a74F2iaUDQyTfjGndn6qUWBnrpR",
		"4UQeTP1T39KZ9Sfxzo3WR5skgsaP6NZa87BAkuazLEKH",
		"8sNeir4QsLsJdYpc9RZacohhK1Y5FLU3nC5LXgYB4aa6",
		"Fh9HmeLNUMVCvejxCtCL2DbYaRyBFVJ5xrWkLnMH6fdk",
		"463MEnMeGyJekNZFQSTUABBEbLnvMTALbT6ZmsxAbAdq",
		"6AUH3WEHucYZyC61hqpqYUWVto5qA5hjHuNQ32GNnNxA",
	}
	pumpBuybackFeeRecipients = []string{
		"5YxQFdt3Tr9zJLvkFccqXVUwhdTWJQc1fFg2YPbxvxeD",
		"9M4giFFMxmFGXtc3feFzRai56WbBqehoSeRE5GK7gf7",
		"GXPFM2caqTtQYC2cJ5yJRi9VDkpsYZXzYdwYpGnLmtDL",
		"3BpXnfJaUTiwXnJNe7Ej1rcbzqTTQUvLShZaWazebsVR",
		"5cjcW9wExnJJiqgLjq7DEG75Pm6JBgE1hNv4B2vHXUW6",
		"EHAAiTxcdDwQ3U4bU6YcMsQGaekdzLS3B5SmYo46kJtL",
		"5eHhjP8JaYkz83CWwvGU2uMUXefd3AazWGx4gpcuEEYD",
		"A7hAgCzFw14fejgCp387JUJRMNyz4j89JKnhtKU8piqW",
	}
)

// Patch offsets (bytes from start of ix data).
const (
	AmountInOffset uint16 = 8
	MinOutOffset   uint16 = 16
)

// CurveMeta is the subset of bonding-curve state needed to build accounts.
type CurveMeta struct {
	Creator        solana.PublicKey
	IsMayhemMode   bool
	CashbackEnabled bool
}

// DecodeCurveMeta parses creator + flags from a bonding-curve account.
func DecodeCurveMeta(data []byte) (CurveMeta, error) {
	const minLen = 8 + 73
	if len(data) < minLen {
		return CurveMeta{}, fmt.Errorf("pumpfun: bonding curve data too short: %d", len(data))
	}
	body := data[8:]
	meta := CurveMeta{
		Creator: solana.PublicKeyFromBytes(body[41:73]),
	}
	if len(body) > 73 {
		meta.IsMayhemMode = body[73] != 0
	}
	if len(body) > 74 {
		meta.CashbackEnabled = body[74] != 0
	}
	return meta, nil
}

func EventAuthorityPDA() (solana.PublicKey, error) {
	pda, _, err := solana.FindProgramAddress([][]byte{[]byte("__event_authority")}, ProgramID)
	return pda, err
}

func GlobalPDA() (solana.PublicKey, error) {
	pda, _, err := solana.FindProgramAddress([][]byte{[]byte("global")}, ProgramID)
	return pda, err
}

func CreatorVaultPDA(creator solana.PublicKey) (solana.PublicKey, error) {
	pda, _, err := solana.FindProgramAddress(
		[][]byte{[]byte("creator-vault"), creator.Bytes()},
		ProgramID,
	)
	return pda, err
}

func GlobalVolumeAccumulatorPDA() (solana.PublicKey, error) {
	pda, _, err := solana.FindProgramAddress([][]byte{[]byte("global_volume_accumulator")}, ProgramID)
	return pda, err
}

func UserVolumeAccumulatorPDA(user solana.PublicKey) (solana.PublicKey, error) {
	pda, _, err := solana.FindProgramAddress(
		[][]byte{[]byte("user_volume_accumulator"), user.Bytes()},
		ProgramID,
	)
	return pda, err
}

func FeeConfigPDA() (solana.PublicKey, error) {
	pda, _, err := solana.FindProgramAddress(
		[][]byte{[]byte("fee_config"), feeConfigConst},
		FeeProgramID,
	)
	return pda, err
}

func BondingCurvePDA(mint solana.PublicKey) (solana.PublicKey, error) {
	pda, _, err := solana.FindProgramAddress(
		[][]byte{[]byte("bonding-curve"), mint.Bytes()},
		ProgramID,
	)
	return pda, err
}

func BondingCurveV2PDA(mint solana.PublicKey) (solana.PublicKey, error) {
	pda, _, err := solana.FindProgramAddress(
		[][]byte{[]byte("bonding-curve-v2"), mint.Bytes()},
		ProgramID,
	)
	return pda, err
}

func PickFeeRecipient(mayhemMode bool) solana.PublicKey {
	list := pumpFeeRecipients
	if mayhemMode {
		list = pumpReservedFeeRecipients
	}
	return solana.MustPublicKeyFromBase58(list[0])
}

func PickBuybackFeeRecipient() solana.PublicKey {
	return solana.MustPublicKeyFromBase58(pumpBuybackFeeRecipients[0])
}

func ataAddress(owner, mint, tokenProgram solana.PublicKey) (solana.PublicKey, error) {
	pda, _, err := solana.FindProgramAddress(
		[][]byte{owner.Bytes(), tokenProgram.Bytes(), mint.Bytes()},
		solana.SPLAssociatedTokenAccountProgramID,
	)
	return pda, err
}

func putU64LE(buf []byte, v uint64) {
	binary.LittleEndian.PutUint64(buf, v)
}
