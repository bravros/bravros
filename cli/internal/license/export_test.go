package license

// deactivatorAdapter wraps a minimal deactivator so the full ClientIface
// is satisfied. Only used in tests — panics on Activate/Verify.
type deactivatorAdapter struct {
	d interface {
		Deactivate(token, machineID string) error
	}
}

func (a *deactivatorAdapter) Activate(_, _ string) (string, error) {
	panic("Activate not implemented on deactivatorAdapter")
}
func (a *deactivatorAdapter) Verify(_, _ string) (string, error) {
	panic("Verify not implemented on deactivatorAdapter")
}
func (a *deactivatorAdapter) Deactivate(token, machineID string) error {
	return a.d.Deactivate(token, machineID)
}

// NewClientFromDeactivator wraps a minimal deactivator stub into a ClientIface.
func NewClientFromDeactivator(d interface {
	Deactivate(token, machineID string) error
}) ClientIface {
	return &deactivatorAdapter{d: d}
}
