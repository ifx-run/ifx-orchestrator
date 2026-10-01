package feature

// AtaPolicy mirrors Exact TokenAccountLifecycleStrategy (implementation in Phase 2).
type AtaPolicy uint8

const (
	AtaUseOnly AtaPolicy = iota
	AtaUseAndClose
	AtaCreateAndCloseCreated
	AtaCreateAndCloseAll
	AtaCreateOnly
)

// Ata is a Feature stub that records the chosen policy (no ix emission yet).
type Ata struct {
	Base
	Policy AtaPolicy
}

// WithAta returns an Ata Feature stub.
func WithAta(p AtaPolicy) *Ata { return &Ata{Policy: p} }
