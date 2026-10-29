package platformserver

import transport "platformserver/kernel/transport"

import (
	controlowner "platformserver/kernel/control"
	"platformserver/studio/packages"

	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"time"

	"platformkernel/kernel"

	"platformserver/platform"
)

// The preview stores inert content-addressed bytes, not an installation or grant.
func (h *Host) routesPackageSources(rt *routes) {
	rt.authorized(transport.Route{Pattern: "POST /v1/packages/inspect", Summary: "Inspect an online or local platform package without installing or executing it", Body: platform.PackageInspectRequest{}, Answer: platform.PackagePreview{}}, controlowner.PlatformApp, []string{controlowner.Admin}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		var request platform.PackageInspectRequest
		if err := decodeBodyLimit(r, &request, packages.MaxPackageImportBytes+4096); err != nil {
			WriteJSON(w, http.StatusBadRequest, map[string]string{"error": t.Say(t.Language(m, r), err.Error())})
			return
		}
		preview, err := t.inspectPackage(r.Context(), request)
		if err != nil {
			WriteJSON(w, http.StatusBadRequest, map[string]string{"error": t.Say(t.Language(m, r), err.Error())})
			return
		}
		WriteJSON(w, http.StatusOK, preview)
	})
}

func (t *Tenant) inspectPackage(ctx context.Context, request platform.PackageInspectRequest) (platform.PackagePreview, error) {
	var preview platform.PackagePreview
	content, origin := []byte(request.Content), "local-file"
	if (request.URL == "") == (len(content) == 0) {
		return preview, fmt.Errorf("Choose one package source: a URL or a local package file")
	}
	if request.URL != "" {
		address, err := url.Parse(request.URL)
		if err != nil || address.Scheme != "https" || address.Host == "" || address.User != nil || address.Fragment != "" {
			return preview, fmt.Errorf("Use an HTTPS address for a platform package, without embedded credentials")
		}
		ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, address.String(), nil)
		if err != nil {
			return preview, err
		}
		req.Header.Set("Accept", "application/json")
		send := t.Outbound
		if send == nil {
			send = guarded
		}
		response, err := send(req, false)
		if err != nil {
			return preview, fmt.Errorf("The package could not be downloaded; check its address and try again")
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return preview, fmt.Errorf("The package source answered HTTP %d", response.StatusCode)
		}
		content, err = io.ReadAll(io.LimitReader(response.Body, packages.MaxPackageImportBytes+1))
		if err != nil {
			return preview, fmt.Errorf("The package download could not be read")
		}
		address.RawQuery, address.ForceQuery = "", false
		origin = address.String()
	}
	if len(content) == 0 || len(content) > packages.MaxPackageImportBytes {
		return preview, fmt.Errorf("A platform package must be non-empty and at most 16 MiB")
	}
	descriptor, err := packages.DecodeContent(content)
	if err != nil {
		return preview, err
	}
	if err := descriptor.Validate(); err != nil {
		return preview, err
	}
	owner, ok := t.app(controlowner.PlatformApp).(interface {
		Precheck(platform.PackageDescriptor) platform.Precheck
	})
	if !ok {
		return preview, fmt.Errorf("Package management is unavailable")
	}
	precheck, refusal := recordRead(t, ctx, func() (platform.Precheck, *kernel.Error) { return owner.Precheck(descriptor), nil })
	if refusal != nil {
		return preview, fmt.Errorf("The package dependencies could not be inspected")
	}
	return t.storePackagePreview(ctx, descriptor, origin, precheck)
}

func (t *Tenant) storePackagePreview(ctx context.Context, descriptor platform.PackageDescriptor, origin string, precheck platform.Precheck) (platform.PackagePreview, error) {
	raw, err := json.Marshal(platform.Offer{Descriptor: descriptor, Origin: origin})
	if err != nil {
		return platform.PackagePreview{}, err
	}
	sum := sha256.Sum256(raw)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	if err = t.files().Put(ctx, packages.OfferKey(t.ID, digest), raw, "application/json"); err != nil {
		return platform.PackagePreview{}, err
	}
	return platform.PackagePreview{Descriptor: descriptor, Digest: digest, Origin: origin, Precheck: precheck}, nil
}
