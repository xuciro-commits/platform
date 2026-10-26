package platformserver

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/acceptance"
	"platformserver/platform"
)

const MaxPackageImportBytes = 16 << 20
const SchemaPackageImport = "platform.package.import"
const SchemaPackageRemoveSource = "platform.package.remove-source"

type PackageSource struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Digest  string `json:"digest"`
	Origin  string `json:"origin"`
}

type PackageInspectRequest struct {
	URL     string          `json:"url,omitempty"`
	Content json.RawMessage `json:"content,omitempty"`
}

type PackagePreview struct {
	Descriptor PackageDescriptor `json:"descriptor"`
	Digest     string            `json:"digest"`
	Origin     string            `json:"origin"`
	Precheck   Precheck          `json:"precheck"`
}

type packageOffer struct {
	Descriptor PackageDescriptor `json:"descriptor"`
	Origin     string            `json:"origin"`
}

func packageOfferKey(tenant, digest string) string {
	return "packages/" + tenant + "/offers/" + strings.TrimPrefix(digest, "sha256:") + ".json"
}

// The preview stores inert content-addressed bytes, not an installation or grant.
func (h *Host) routesPackageSources(rt *routes) {
	rt.authorized(Route{Pattern: "POST /v1/packages/inspect", Summary: "Inspect an online or local platform package without installing or executing it", Body: PackageInspectRequest{}, Answer: PackagePreview{}}, PlatformApp, []string{Admin}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		var request PackageInspectRequest
		if err := decodeBodyLimit(r, &request, MaxPackageImportBytes+4096); err != nil {
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

func (t *Tenant) inspectPackage(ctx context.Context, request PackageInspectRequest) (PackagePreview, error) {
	var preview PackagePreview
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
		content, err = io.ReadAll(io.LimitReader(response.Body, MaxPackageImportBytes+1))
		if err != nil {
			return preview, fmt.Errorf("The package download could not be read")
		}
		address.RawQuery, address.ForceQuery = "", false
		origin = address.String()
	}
	if len(content) == 0 || len(content) > MaxPackageImportBytes {
		return preview, fmt.Errorf("A platform package must be non-empty and at most 16 MiB")
	}
	descriptor, err := decodePackageContent(content)
	if err != nil {
		return preview, err
	}
	if err := descriptor.validate(); err != nil {
		return preview, err
	}
	owner, ok := t.app(PlatformApp).(interface {
		Precheck(PackageDescriptor) Precheck
	})
	if !ok {
		return preview, fmt.Errorf("Package management is unavailable")
	}
	precheck, refusal := recordRead(t, ctx, func() (Precheck, *kernel.Error) { return owner.Precheck(descriptor), nil })
	if refusal != nil {
		return preview, fmt.Errorf("The package dependencies could not be inspected")
	}
	return t.storePackagePreview(ctx, descriptor, origin, precheck)
}

func (t *Tenant) storePackagePreview(ctx context.Context, descriptor PackageDescriptor, origin string, precheck Precheck) (PackagePreview, error) {
	raw, err := json.Marshal(packageOffer{Descriptor: descriptor, Origin: origin})
	if err != nil {
		return PackagePreview{}, err
	}
	sum := sha256.Sum256(raw)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	if err = t.files().Put(ctx, packageOfferKey(t.ID, digest), raw, "application/json"); err != nil {
		return PackagePreview{}, err
	}
	return PackagePreview{Descriptor: descriptor, Digest: digest, Origin: origin, Precheck: precheck}, nil
}

func (d *Console) packageOffer(digest string) (packageOffer, error) {
	var offer packageOffer
	if !platform.ValidDigest(digest) {
		return offer, fmt.Errorf("The reviewed package digest is invalid")
	}
	raw, err := d.host.GetPackageArtifact(packageOfferKey(d.tenant, digest))
	if err != nil {
		return offer, fmt.Errorf("The reviewed package is unavailable; inspect it again")
	}
	sum := sha256.Sum256(raw)
	if "sha256:"+hex.EncodeToString(sum[:]) != digest {
		return offer, fmt.Errorf("The reviewed package content changed")
	}
	if err = acceptance.DecodeJSON(raw, &offer, "reviewed package"); err != nil {
		return offer, err
	}
	if err := offer.Descriptor.validate(); err != nil {
		return offer, err
	}
	return offer, nil
}

func (d *Console) decidePackageSource(_ platform.Caller, s *pb.Submission) (func(*pb.ChangeRecord), *kernel.Error) {
	var input struct{ ID, Digest string }
	if json.Unmarshal(s.GetPayload(), &input) != nil || input.ID == "" || !platform.ValidDigest(input.Digest) {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose the reviewed package content")
	}
	if s.GetSchema().GetName() == SchemaPackageRemoveSource {
		d.mu.Lock()
		source, found := d.packageSources[input.Digest]
		held := d.packages[input.ID]
		d.mu.Unlock()
		if !found || source.ID != input.ID {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "The package source is unavailable")
		}
		if held != nil && held.State != "retired" && held.Version == source.Version {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Uninstall this version before removing its source")
		}
		return func(*pb.ChangeRecord) { delete(d.packageSources, input.Digest) }, nil
	}
	offer, err := d.packageOffer(input.Digest)
	if err != nil || offer.Descriptor.ID != input.ID {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The reviewed package does not match this request")
	}
	index, err := d.packageIndex()
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The package catalogue cannot be read")
	}
	if prior, found := index.find(offer.Descriptor.ID, offer.Descriptor.Version); found {
		before, _ := json.Marshal(prior)
		after, _ := json.Marshal(offer.Descriptor)
		if !bytes.Equal(before, after) {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A package version cannot be replaced; publish a new version")
		}
	}
	if err = d.verifyPackageVersion(offer.Descriptor, d.packagesOf()[input.ID]); err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{reason}", err.Error())
	}
	return func(*pb.ChangeRecord) {
		if d.packageSources == nil {
			d.packageSources = map[string]PackageSource{}
		}
		d.packageSources[input.Digest] = PackageSource{ID: input.ID, Version: offer.Descriptor.Version, Digest: input.Digest, Origin: offer.Origin}
	}, nil
}

func (d *Console) sourceIndex(index *PackageIndex) (*PackageIndex, error) {
	out := &PackageIndex{Directory: index.Directory, Packages: slices.Clone(index.Packages)}
	d.mu.Lock()
	sources := make([]PackageSource, 0, len(d.packageSources))
	for _, source := range d.packageSources {
		sources = append(sources, source)
	}
	d.mu.Unlock()
	slices.SortFunc(sources, func(a, b PackageSource) int { return strings.Compare(a.Digest, b.Digest) })
	for _, source := range sources {
		offer, err := d.packageOffer(source.Digest)
		if err != nil {
			return index, err
		}
		if prior, found := out.find(source.ID, source.Version); found {
			a, _ := json.Marshal(prior)
			b, _ := json.Marshal(offer.Descriptor)
			if !bytes.Equal(a, b) {
				return index, fmt.Errorf("package %s@%s has conflicting contents", source.ID, source.Version)
			}
			continue
		}
		out.Packages = append(out.Packages, offer.Descriptor)
	}
	slices.SortFunc(out.Packages, func(a, b PackageDescriptor) int {
		if a.ID != b.ID {
			return strings.Compare(a.ID, b.ID)
		}
		return strings.Compare(a.Version, b.Version)
	})
	return out, nil
}
