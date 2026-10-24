package platform

// DriverLifecycle reports live technical resources and retained attempt
// blockers. Closing a connection never rewrites a business or device receipt.
type DriverLifecycle struct {
	ResourceState
	Connection string   `json:"connection"`
	Revision   string   `json:"revision"`
	Draining   bool     `json:"draining"`
	Released   bool     `json:"released"`
	Blockers   []string `json:"blockers"`
}
