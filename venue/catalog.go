// Package venue lists ExactInHop venue identifiers and Jupiter-style labels.
package venue

// JupiterLabelHint maps orchestrator VenueID() strings to Jupiter route labels (hints only).
var JupiterLabelHint = map[string]string{
	"raydium_cpmm":                  "Raydium CPMM",
	"raydium_amm_v4":                "Raydium",
	"raydium_clmm":                  "Raydium CLMM",
	"raydium_launchpad_buy":         "Raydium Launchlab",
	"raydium_launchpad_sell":        "Raydium Launchlab",
	"meteora_damm_v2":               "Meteora DAMM v2",
	"meteora_dlmm":                  "Meteora DLMM",
	"meteora_dbc":                   "Dynamic Bonding Curve",
	"orca_whirlpool":                "Whirlpool",
	"pump_amm_sell":                 "Pump.fun Amm",
	"pump_amm_buy":                  "Pump.fun Amm",
	"pumpfun_buy_exact_sol_in":      "Pump.fun",
	"pumpfun_sell":                  "Pump.fun",
	"pumpfun_buy_exact_quote_in_v2": "Pump.fun",
	"pumpfun_sell_v2":               "Pump.fun",
}

// JupiterDexFilter is the set of Jupiter quote `dexes` query values for venues we can build.
// Prefer these when discovering routes so Jupiter does not return hops we cannot compile.
var JupiterDexFilter = []string{
	"Raydium",
	"Raydium CLMM",
	"Raydium CP",
	"Whirlpool",
	"Meteora",
	"Meteora DLMM",
	"Pump.fun",
	"Pump.fun Amm",
	"Dynamic Bonding Curve",
	"Raydium Launchlab",
}
