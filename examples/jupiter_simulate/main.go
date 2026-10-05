// Command jupiter_simulate: Jupiter quote for path discovery → our ExactInHop build → RPC simulate.
//
// Does NOT call Jupiter /swap (no JUP6 aggregator tx). Pool account layouts come from our venues.
//
// RPC / Jupiter proxy: SOLANA_RPC_URL or IFX_LAUNCHPAD_CONFIG (launchpad config.toml).
//
// Example:
//
//	IFX_LAUNCHPAD_CONFIG=/path/to/config.toml go run ./examples/jupiter_simulate/
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/ifx-run/ifx-orchestrator/feature"
	"github.com/ifx-run/ifx-orchestrator/hop"
	"github.com/ifx-run/ifx-orchestrator/orchestrator"
	"github.com/ifx-run/ifx-orchestrator/venue"
	"github.com/ifx-run/ifx-orchestrator/venue/meteoradammv2"
	"github.com/ifx-run/ifx-orchestrator/venue/raydiumammv4"
	"github.com/ifx-run/ifx-orchestrator/venue/raydiumcpmm"
	"github.com/ifx-run/ifx/go-sdk/constants"
	"github.com/ifx-run/ifx/go-sdk/scratch"
)

// USER_PUBKEY: funded wallet for simulate (SigVerify=false; no private key needed).
// USER_KEYPAIR: optional Solana JSON keypair path if you want a real signature.

const (
	wsolMint   = "So11111111111111111111111111111111111111112"
	usdcMint   = "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"
	jupiterAPI = "https://lite-api.jup.ag/swap/v1"
)

var framePK = solana.MustPublicKeyFromBase58("Fr8dvcgrSYKjpvJd471hQD2QuEjF7656WiEuUSb54obu")

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	rpcURL, err := resolveRPC()
	if err != nil {
		log.Fatal(err)
	}
	jupBase := jupiterAPI
	proxyURL := os.Getenv("JUPITER_PROXY")
	skipProxy := os.Getenv("JUPITER_SKIP_PROXY") == "1"
	if cfgPath := os.Getenv("IFX_LAUNCHPAD_CONFIG"); cfgPath != "" {
		if u, j, p, e := launchpadSettings(cfgPath); e == nil {
			if u != "" {
				rpcURL = u
			}
			if j != "" {
				jupBase = strings.TrimRight(j, "/")
			}
			if !skipProxy && proxyURL == "" && p != "" {
				proxyURL = p
			}
		}
	}
	if skipProxy {
		proxyURL = ""
	}
	httpClient := newHTTPClient(proxyURL)

	userPK, signer, err := resolveUser()
	if err != nil {
		log.Fatal(err)
	}
	amountIn := uint64(10_000_000) // 0.01 SOL
	slippageBps := 50

	// Prefer pool types the example can decode + build without tick/bin remaining accounts.
	autoBuildDexes := []string{"Raydium", "Raydium CP", "Meteora DAMM v2"}
	quote, err := jupiterQuote(ctx, httpClient, jupBase, wsolMint, usdcMint, amountIn, slippageBps, autoBuildDexes)
	if err != nil {
		log.Fatalf("quote: %v", err)
	}

	report := map[string]any{
		"mode":    "jupiter_quote_then_own_build",
		"rpc":     redactRPC(rpcURL),
		"jupiter": jupBase,
		"proxy":   proxyURL,
		"user":    userPK.String(),
		"quote": map[string]any{
			"inAmount":  quote.InAmount,
			"outAmount": quote.OutAmount,
			"impact":    quote.PriceImpactPct,
			"routePlan": quote.RoutePlan,
		},
		"supportedVenueHints": venue.JupiterLabelHint,
	}

	client := rpc.New(rpcURL)
	if len(quote.RoutePlan) == 0 {
		report["error"] = "empty routePlan"
		encode(report)
		os.Exit(1)
	}

	leg := quote.RoutePlan[0]
	kind := classifyLabel(leg.SwapInfo.Label)
	report["classified"] = kind
	report["pool"] = leg.SwapInfo.AmmKey

	hopImpl, buildNote, err := buildHopFromLeg(ctx, client, userPK, leg)
	if err != nil {
		report["buildError"] = err.Error()
		report["note"] = "Jupiter discovery ok; this pool type needs more account decoding or is multi-hop — see venue packages"
		encode(report)
		os.Exit(1)
	}
	report["buildNote"] = buildNote
	report["venueID"] = hopImpl.VenueID()

	tape := 2048
	s := scratch.ForPublicFrame(framePK, constants.DefaultProgramID, &tape)

	b := orchestrator.New(s, userPK).
		AmountIn(amountIn).
		MinAmountOut(quote.MinOut()).
		AtaPolicy(feature.AtaCreateOnly)
	// Funded wallet usually holds native SOL; first hop WSOL ⇒ wrap AmountIn.
	if hopImpl.Input().Mint.Equals(hop.WrappedSOLMint) && !hopImpl.Input().Native {
		b = b.SolIn(hop.SolNative)
		report["solIn"] = "native→WSOL wrap AmountIn"
	}
	plan, err := b.HopWithMinOut(hopImpl, quote.MinOut()).Build()
	if err != nil {
		report["compileError"] = err.Error()
		encode(report)
		os.Exit(1)
	}
	report["ixCount"] = len(plan.Instructions)

	recent, err := client.GetLatestBlockhash(ctx, rpc.CommitmentConfirmed)
	if err != nil {
		log.Fatalf("blockhash: %v", err)
	}
	tx, err := solana.NewTransaction(
		plan.Instructions,
		recent.Value.Blockhash,
		solana.TransactionPayer(userPK),
	)
	if err != nil {
		log.Fatalf("tx: %v", err)
	}
	if signer != nil {
		_, err = tx.Sign(func(key solana.PublicKey) *solana.PrivateKey {
			if key.Equals(signer.PublicKey()) {
				return &signer.PrivateKey
			}
			return nil
		})
		if err != nil {
			log.Fatalf("sign: %v", err)
		}
	} else {
		// SigVerify=false: pad empty signatures for the required signers.
		n := int(tx.Message.Header.NumRequiredSignatures)
		tx.Signatures = make([]solana.Signature, n)
		report["sign"] = "empty (USER_PUBKEY simulate)"
	}

	sim, err := client.SimulateTransactionWithOpts(ctx, tx, &rpc.SimulateTransactionOpts{
		ReplaceRecentBlockhash: true,
		SigVerify:              false,
		Commitment:             rpc.CommitmentConfirmed,
	})
	if err != nil {
		log.Fatalf("simulate: %v", err)
	}
	ok := sim != nil && sim.Value != nil && sim.Value.Err == nil
	var units uint64
	if sim != nil && sim.Value != nil && sim.Value.UnitsConsumed != nil {
		units = *sim.Value.UnitsConsumed
	}
	var logs []string
	var simErr any
	if sim != nil && sim.Value != nil {
		logs = sim.Value.Logs
		simErr = sim.Value.Err
	}
	report["simulate"] = map[string]any{
		"ok":            ok,
		"unitsConsumed": units,
		"err":           simErr,
		"logs":          logs,
		"note":          "fresh wallet: expect AccountNotFound / insufficient funds — proves OUR ix path, not Jupiter /swap",
	}
	encode(report)
	// Exit 0 even if sim fails on funding — discovery+build is the success criterion.
}

func encode(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func classifyLabel(label string) string {
	n := strings.ToLower(label)
	switch {
	case strings.Contains(n, "cpmm") || strings.Contains(n, "raydium cp"):
		return "raydium_cpmm"
	case strings.Contains(n, "clmm"):
		return "raydium_clmm"
	case strings.Contains(n, "launchlab") || strings.Contains(n, "launchpad"):
		return "raydium_launchpad"
	case n == "raydium" || strings.Contains(n, "raydium v4"):
		return "raydium_amm_v4"
	case strings.Contains(n, "damm") || (strings.Contains(n, "meteora") && strings.Contains(n, "cp")):
		return "meteora_damm_v2"
	case strings.Contains(n, "dlmm"):
		return "meteora_dlmm"
	case strings.Contains(n, "bonding curve") || strings.Contains(n, "dbc"):
		return "meteora_dbc"
	case strings.Contains(n, "whirlpool") || strings.Contains(n, "orca"):
		return "orca_whirlpool"
	case strings.Contains(n, "pump") && strings.Contains(n, "amm"):
		return "pump_amm"
	case strings.Contains(n, "pump"):
		return "pumpfun"
	default:
		return "unsupported:" + label
	}
}

type routeLeg struct {
	SwapInfo struct {
		AmmKey     string `json:"ammKey"`
		Label      string `json:"label"`
		InputMint  string `json:"inputMint"`
		OutputMint string `json:"outputMint"`
		InAmount   string `json:"inAmount"`
		OutAmount  string `json:"outAmount"`
	} `json:"swapInfo"`
}

func buildHopFromLeg(ctx context.Context, client *rpc.Client, user solana.PublicKey, leg routeLeg) (hop.ExactInHop, string, error) {
	kind := classifyLabel(leg.SwapInfo.Label)
	poolID := solana.MustPublicKeyFromBase58(leg.SwapInfo.AmmKey)
	inMint := solana.MustPublicKeyFromBase58(leg.SwapInfo.InputMint)
	outMint := solana.MustPublicKeyFromBase58(leg.SwapInfo.OutputMint)
	inATA, _, err := solana.FindAssociatedTokenAddress(user, inMint)
	if err != nil {
		return nil, "", err
	}
	outATA, _, err := solana.FindAssociatedTokenAddress(user, outMint)
	if err != nil {
		return nil, "", err
	}

	info, err := client.GetAccountInfo(ctx, poolID)
	if err != nil || info == nil || info.Value == nil {
		return nil, "", fmt.Errorf("fetch pool %s: %v", poolID, err)
	}
	data := info.Value.Data.GetBinary()

	switch kind {
	case "raydium_cpmm":
		ps, err := raydiumcpmm.DecodePoolState(data)
		if err != nil {
			return nil, "", err
		}
		h, err := raydiumcpmm.NewExactIn(raydiumcpmm.Params{
			User: user, PoolID: poolID, Pool: ps,
			InputMint: inMint, OutputMint: outMint,
			UserInputATA: inATA, UserOutputATA: outATA,
		})
		return h, "built venue/raydiumcpmm from on-chain pool", err
	case "raydium_amm_v4":
		ps, err := raydiumammv4.DecodePoolState(data)
		if err != nil {
			return nil, "", err
		}
		h, err := raydiumammv4.NewExactIn(raydiumammv4.Params{
			User: user, PoolID: poolID, Pool: ps,
			InputMint: inMint, OutputMint: outMint,
			UserInputATA: inATA, UserOutputATA: outATA,
		})
		return h, "built venue/raydiumammv4 from on-chain pool", err
	case "meteora_damm_v2":
		ps, err := meteoradammv2.DecodePoolState(data)
		if err != nil {
			return nil, "", err
		}
		tpA, err := mintTokenProgram(ctx, client, ps.MintA)
		if err != nil {
			return nil, "", err
		}
		tpB, err := mintTokenProgram(ctx, client, ps.MintB)
		if err != nil {
			return nil, "", err
		}
		h, err := meteoradammv2.NewExactIn(meteoradammv2.Params{
			User: user, PoolID: poolID, Pool: ps,
			InputMint: inMint, OutputMint: outMint,
			UserInputATA: inATA, UserOutputATA: outATA,
			TokenProgramA: tpA,
			TokenProgramB: tpB,
		})
		return h, "built venue/meteoradammv2 from on-chain pool", err
	default:
		return nil, "", fmt.Errorf("auto-build not wired for %q (label=%s); venue package exists — pass tick/bin arrays / extra PDAs manually", kind, leg.SwapInfo.Label)
	}
}

type quoteResult struct {
	InAmount             string     `json:"inAmount"`
	OutAmount            string     `json:"outAmount"`
	OtherAmountThreshold string     `json:"otherAmountThreshold"`
	PriceImpactPct       string     `json:"priceImpactPct"`
	RoutePlan            []routeLeg `json:"routePlan"`
}

func (q *quoteResult) MinOut() uint64 {
	for _, s := range []string{q.OtherAmountThreshold, q.OutAmount} {
		if s == "" {
			continue
		}
		var v uint64
		if _, err := fmt.Sscanf(s, "%d", &v); err == nil {
			return v
		}
	}
	return 1
}

func mintTokenProgram(ctx context.Context, client *rpc.Client, mint solana.PublicKey) (solana.PublicKey, error) {
	info, err := client.GetAccountInfo(ctx, mint)
	if err != nil {
		return solana.PublicKey{}, err
	}
	if info == nil || info.Value == nil {
		return solana.PublicKey{}, fmt.Errorf("mint not found: %s", mint)
	}
	return info.Value.Owner, nil
}

func resolveUser() (solana.PublicKey, *solana.Wallet, error) {
	if path := os.Getenv("USER_KEYPAIR"); path != "" {
		pk, err := solana.PrivateKeyFromSolanaKeygenFile(path)
		if err != nil {
			return solana.PublicKey{}, nil, fmt.Errorf("USER_KEYPAIR: %w", err)
		}
		w := &solana.Wallet{PrivateKey: pk}
		return w.PublicKey(), w, nil
	}
	if pub := os.Getenv("USER_PUBKEY"); pub != "" {
		pk, err := solana.PublicKeyFromBase58(pub)
		if err != nil {
			return solana.PublicKey{}, nil, fmt.Errorf("USER_PUBKEY: %w", err)
		}
		return pk, nil, nil
	}
	w := solana.NewWallet()
	return w.PublicKey(), w, nil
}

func resolveRPC() (string, error) {
	if u := os.Getenv("SOLANA_RPC_URL"); u != "" {
		return u, nil
	}
	if p := os.Getenv("IFX_LAUNCHPAD_CONFIG"); p != "" {
		u, _, _, err := launchpadSettings(p)
		if err != nil {
			return "", err
		}
		if u != "" {
			return u, nil
		}
	}
	return "", fmt.Errorf("set SOLANA_RPC_URL or IFX_LAUNCHPAD_CONFIG")
}

func launchpadSettings(path string) (rpcURL, jupiterURL, proxyURL string, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", "", err
	}
	text := string(data)
	if m := regexp.MustCompile(`(?m)^\s*rpc_url\s*=\s*"([^"]+)"`).FindStringSubmatch(text); len(m) > 1 {
		rpcURL = m[1]
	}
	if m := regexp.MustCompile(`(?m)^\s*api_url\s*=\s*"([^"]+)"`).FindStringSubmatch(text); len(m) > 1 {
		jupiterURL = m[1]
	}
	if m := regexp.MustCompile(`(?m)^\s*proxy_url\s*=\s*"([^"]+)"`).FindStringSubmatch(text); len(m) > 1 {
		proxyURL = m[1]
	}
	return rpcURL, jupiterURL, proxyURL, nil
}

func newHTTPClient(proxyURL string) *http.Client {
	transport := &http.Transport{}
	if proxyURL != "" {
		u, err := url.Parse(proxyURL)
		if err == nil {
			transport.Proxy = http.ProxyURL(u)
		}
	}
	return &http.Client{Timeout: 60 * time.Second, Transport: transport}
}

func redactRPC(u string) string {
	if i := strings.LastIndex(u, "/"); i > 8 && len(u) > i+8 {
		return u[:i+1] + "…"
	}
	return u
}

func jupiterQuote(ctx context.Context, client *http.Client, base, inMint, outMint string, amount uint64, slippageBps int, dexes []string) (*quoteResult, error) {
	q := url.Values{}
	q.Set("inputMint", inMint)
	q.Set("outputMint", outMint)
	q.Set("amount", fmt.Sprintf("%d", amount))
	q.Set("slippageBps", fmt.Sprintf("%d", slippageBps))
	q.Set("swapMode", "ExactIn")
	q.Set("onlyDirectRoutes", "true")
	if len(dexes) > 0 {
		q.Set("dexes", strings.Join(dexes, ","))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/quote?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(body), 400))
	}
	var out quoteResult
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
