package polaris

// GetNamespace returns the namespace of the PlugPolaris instance.
// Safe to call after destroy: returns "default" when plugin is destroyed or not configured.
func (p *PlugPolaris) GetNamespace() string {
	if p.IsDestroyed() {
		return "default"
	}
	if p.conf != nil {
		return p.conf.Namespace
	}
	return "default"
}
