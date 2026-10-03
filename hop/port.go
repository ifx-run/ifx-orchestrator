package hop

import (
	"fmt"

	"github.com/gagliardetto/solana-go"
)

// WrappedSOLMint is native wrapped SOL (So111…), used as mint identity for both
// wallet lamports (Port.Native) and WSOL ATAs.
var WrappedSOLMint = solana.MustPublicKeyFromBase58("So11111111111111111111111111111111111111112")

// SolForm selects how native SOL / WSOL is presented at the route boundary.
type SolForm uint8

const (
	// SolAsEmitted leaves the first/last hop in the form the venue CPI uses.
	SolAsEmitted SolForm = iota
	// SolNative spends or settles user-wallet lamports.
	SolNative
	// SolWSOL spends or settles the user's WSOL ATA.
	SolWSOL
)

// Port is one hop endpoint: an SPL ATA, or native SOL in the user wallet.
type Port struct {
	Mint    solana.PublicKey
	Account solana.PublicKey // user ATA when !Native; must be zero when Native
	Native  bool             // true: value is user lamports; Mint must be WrappedSOLMint
}

// TokenPort is a normal SPL user ATA (including WSOL).
func TokenPort(mint, ata solana.PublicKey) Port {
	return Port{Mint: mint, Account: ata}
}

// NativeSOLPort is user-wallet lamports with Wrapped SOL mint identity.
func NativeSOLPort() Port {
	return Port{Mint: WrappedSOLMint, Native: true}
}

// Validate checks Native/mint/account consistency.
func (p Port) Validate() error {
	if p.Native {
		if !p.Mint.Equals(WrappedSOLMint) {
			return fmt.Errorf("native port mint must be wrapped SOL, got %s", p.Mint)
		}
		if !p.Account.IsZero() {
			return fmt.Errorf("native port must not set an ATA")
		}
		return nil
	}
	if p.Mint.IsZero() {
		return fmt.Errorf("token port mint is required")
	}
	return nil
}

// SameMint reports whether two ports share a mint (WSOL ATA and native SOL match).
func (p Port) SameMint(other Port) bool {
	return p.Mint.Equals(other.Mint)
}

// NodeFromPort builds a graph node from a hop port.
func NodeFromPort(p Port) RouteNode {
	n := RouteNode{Mint: p.Mint, Native: p.Native}
	if !p.Native {
		n.TokenAccount = p.Account
	}
	return n
}
