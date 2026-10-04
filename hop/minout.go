package hop

// withMinOut attaches a min_amount_out value to an ExactInHop (0 is valid).
type withMinOut struct {
	inner ExactInHop
	min   uint64
}

// WithMinOut returns h annotated with minOut for route compilation.
// Re-wrapping replaces any previous min on h.
func WithMinOut(h ExactInHop, minOut uint64) ExactInHop {
	if w, ok := h.(*withMinOut); ok {
		h = w.inner
	}
	return &withMinOut{inner: h, min: minOut}
}

// SplitMinOut peels WithMinOut. bare is always non-nil when h is.
func SplitMinOut(h ExactInHop) (minOut *uint64, bare ExactInHop) {
	if w, ok := h.(*withMinOut); ok {
		m := w.min
		return &m, w.inner
	}
	return nil, h
}

func (w *withMinOut) VenueID() string { return w.inner.VenueID() }
func (w *withMinOut) Input() Port     { return w.inner.Input() }
func (w *withMinOut) Output() Port    { return w.inner.Output() }
func (w *withMinOut) BuildBlueprint(cx *HopBuildCtx) (HopBlueprint, error) {
	return w.inner.BuildBlueprint(cx)
}

var _ ExactInHop = (*withMinOut)(nil)
