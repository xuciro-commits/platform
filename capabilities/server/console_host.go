package platformserver

import (
	"context"
	"io"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/host"
	"platformserver/platform"
)

func (h hostView) Apps() []platform.App { return h.t.liveApps() }
func (h hostView) App(id string) platform.App {
	if !h.t.ownerActive(id) {
		return nil
	}
	return h.t.app(id)
}
func (h hostView) BaseModules() []platform.CapabilityModule { return h.t.baseModules() }
func (h hostView) PackageBlockers(id string, proofs map[string]string) ([]string, bool) {
	return h.t.packageBlockers(id, proofs)
}
func (h hostView) CodeReleaseAssets() ([]platform.ReleaseAsset, error) {
	var assets []platform.ReleaseAsset
	for _, app := range h.t.liveApps() {
		owned, err := h.t.codeReleaseAssetsLocked(app)
		if err != nil {
			return nil, err
		}
		assets = append(assets, owned...)
	}
	return assets, nil
}
func (h hostView) ConsoleSetting(name string) string {
	return h.t.setting(h.t.automation(PlatformApp, false), name)
}
func (h hostView) Languages() []string   { return h.t.i18n.languages() }
func (h hostView) TenantSuspended() bool { return h.t.console.suspended() }
func (h hostView) GovernanceRead(c platform.Caller, name string) (any, *kernel.Error) {
	switch name {
	case "notifications":
		return h.t.notificationsFor(c.ID), nil
	case "audit":
		return h.t.Audit(), nil
	case "deliveries":
		return h.t.Deliveries(), nil
	case "work":
		return h.t.Tasks(), nil
	case "connectors":
		return h.t.Connectors(time.Now()), nil
	case "settings":
		return h.t.Settings(), nil
	case "endpoints":
		return h.t.Endpoints(), nil
	case "effects":
		return h.t.Effects(time.Now()), nil
	case "health":
		return h.t.health(time.Now()), nil
	case "personal-reads":
		return h.t.PersonalReads(), nil
	case "permissions":
		return h.t.Permissions(), nil
	}
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
}
func (h hostView) ConnectorHeartbeat(c platform.Caller, now time.Time) *kernel.Error {
	h.t.opsMu.Lock()
	defer h.t.opsMu.Unlock()
	return h.t.connectors.kernel.Heartbeat(c.Tenant, c.ID, now)
}
func (h hostView) PutPackageArtifact(key string, raw []byte) error {
	return h.t.files().Put(context.Background(), key, raw, "application/json")
}
func (h hostView) GetPackageArtifact(key string) ([]byte, error) {
	reader, _, err := h.t.files().Get(context.Background(), key)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(reader)
}

var _ host.ConsoleHost = hostView{}
