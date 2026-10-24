package platformserver

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/acceptance"
	"platformserver/internal/construction"
	"platformserver/internal/host"
	"platformserver/platform"
)

type PackageRemoval struct {
	ID           string   `json:"id"`
	OK           bool     `json:"ok"`
	NeedsCleanup bool     `json:"needsCleanup"`
	Blockers     []string `json:"blockers"`
}

func cleanupKey(id string, c construction.Connection) string {
	return id + "\x00" + c.ID + "\x00" + c.Protocol.Revision() + "\x00" + cleanupDigest(c)
}
func cleanupDigest(c construction.Connection) string {
	digest, _ := acceptance.Digest([]any{c.ID, c.Kind, c.Address, c.Secret, c.AllowPrivate, c.Protocol})
	return digest
}

func (t *Tenant) packageConnections(id string) ([]construction.Connection, []string) {
	connections := []construction.Connection{}
	blockers := []string{}
	if t.records.types[construction.ConnectionType] == nil {
		return connections, blockers
	}
	c := t.automation(construction.ID, false)
	rows, total, refusal := platform.Find[construction.Connection](c, platform.Query{Archived: true, Sort: []string{"id"}, Limit: platform.RecordSetMaxRows})
	if refusal != nil || total != len(rows) {
		return nil, []string{"The connection inventory is incomplete; cleanup cannot be proven"}
	}
	seen := map[string]bool{}
	add := func(connection construction.Connection) {
		if connection.Protocol == nil || connection.Protocol.Package != id {
			return
		}
		key := cleanupKey(id, connection)
		if !seen[key] {
			seen[key] = true
			connections = append(connections, connection)
		}
	}
	for _, row := range rows {
		inspect := func(connection construction.Connection) {
			if connection.Protocol == nil {
				return
			}
			// Cleanup inspects saved bindings, including drafts and old revisions.
			// It must not ask the execution gate to reactivate a draining package.
			add(connection)
		}
		inspect(row)
		t.records.mu.Lock()
		held := t.records.types[construction.ConnectionType].rows.Get(row.ID)
		t.records.mu.Unlock()
		if held == nil {
			blockers = append(blockers, "The connection history is unavailable")
			continue
		}
		history := held.RecordHistory()
		if len(history) > platform.RecordSetMaxRows {
			blockers = append(blockers, "The connection history exceeds the cleanup read budget")
			continue
		}
		var image map[string]json.RawMessage
		_ = json.Unmarshal(platform.Raw(row), &image)
		for i := len(history) - 1; i >= 0; i-- {
			for _, field := range history[i].Fields {
				if len(field.Before) == 0 {
					delete(image, field.Field)
				} else {
					image[field.Field] = field.Before
				}
			}
			var previous construction.Connection
			if json.Unmarshal(platform.Raw(image), &previous) != nil {
				blockers = append(blockers, "A retained connection image cannot be read")
				break
			}
			previous.ID = row.ID
			inspect(previous)
		}
	}
	if t.records.types[platform.DispatchType] != nil {
		runs, total, refusal := platform.Find[platform.DispatchRun](c, platform.Query{Archived: true, Limit: platform.RecordSetMaxRows})
		if refusal != nil || total != len(runs) {
			blockers = append(blockers, "The dispatch inventory is incomplete; cleanup cannot be proven")
		}
		for _, run := range runs {
			var request construction.WritebackRequest
			if json.Unmarshal(run.Request, &request) != nil || request.Protocol == nil || request.Protocol.Package != id {
				continue
			}
			add(construction.Connection{Record: platform.Record{ID: request.Connection}, Kind: "http", Address: request.Gateway, Secret: request.Secret, AllowPrivate: request.AllowPrivate, Protocol: request.Protocol})
			if !slices.Contains([]string{"completed", "failed", "cancelled", "compensated"}, run.State) {
				blockers = append(blockers, fmt.Sprintf("Resolve dispatch %s before uninstalling this package", run.ID))
			}
		}
	}
	slices.SortFunc(connections, func(a, b construction.Connection) int { return strings.Compare(cleanupKey(id, a), cleanupKey(id, b)) })
	return connections, blockers
}

func (t *Tenant) packageBlockers(id string, proofs map[string]string) ([]string, bool) {
	connections, blockers := t.packageConnections(id)
	needs := false
	for _, connection := range connections {
		if proofs[cleanupKey(id, connection)] != cleanupDigest(connection) {
			needs = true
			blockers = append(blockers, fmt.Sprintf("Release connection %s before uninstalling this package", connection.ID))
		}
	}
	return blockers, needs
}

func (h *Host) routesPackageCleanup(rt *routes) {
	rt.authorized(Route{Pattern: "POST /v1/packages/{id}/cleanup", Summary: "Release a draining package's known protocol resources; retain all data and receipts", Answer: PackageRemoval{}}, PlatformApp, []string{Admin}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		id := r.PathValue("id")
		type plan struct {
			connections []construction.Connection
			blockers    []string
			generation  string
			proofs      map[string]string
		}
		capture, refusal := coordinatedRecordRead(t, r.Context(), func() (plan, *kernel.Error) {
			console, ok := t.app(PlatformApp).(host.PackageCleanupOwner)
			if !ok {
				return plan{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Package management is unavailable")
			}
			state, generation, proofs := console.PackageCleanupState(id)
			if state != "draining" {
				return plan{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Prepare this package for uninstalling first")
			}
			connections, blockers := t.packageConnections(id)
			return plan{connections: connections, blockers: blockers, generation: generation, proofs: proofs}, nil
		})
		if refusal != nil {
			Reply(w, nil, refusal)
			return
		}
		if len(capture.blockers) > 0 {
			WriteJSON(w, http.StatusConflict, PackageRemoval{ID: id, Blockers: capture.blockers})
			return
		}
		proofs := maps.Clone(capture.proofs)
		if proofs == nil {
			proofs = map[string]string{}
		}
		var problems []string
		for _, connection := range capture.connections {
			if proofs[cleanupKey(id, connection)] == cleanupDigest(connection) {
				continue
			}
			var status platform.DriverLifecycle
			if err := t.protocolHTTP(r.Context(), connection, http.MethodPost, "release", connection.Protocol, &status); err != nil {
				problems = append(problems, err.Error())
				continue
			}
			if !status.Released || status.State != "closed" || status.InFlight != 0 || len(status.Blockers) != 0 || status.Connection != connection.Protocol.ConnectionID(connection.ID) || status.Revision != connection.Protocol.Revision() {
				problems = append(problems, "The gateway has not confirmed resource cleanup")
				continue
			}
			proofs[cleanupKey(id, connection)] = cleanupDigest(connection)
		}
		proof := packageCleanupProof{ID: id, Generation: capture.generation, Connections: proofs}
		digest, _ := acceptance.Digest(proof)
		if err := t.files().Put(r.Context(), cleanupProofKey(t.ID, digest), platform.Raw(proof), "application/json"); err != nil {
			WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "The cleanup receipt could not be staged; retry to verify the same resources"})
			return
		}
		_, refusal = t.Submit(m, &pb.Submission{TenantId: t.ID, PrincipalId: m.ID, Authority: PlatformApp, IdempotencyKey: "package-cleanup:" + m.ID + ":" + digest, Target: &pb.EntityRef{Type: PackageType, Id: id}, Schema: &pb.SchemaRef{Name: SchemaPackageCleanupConfirm, Version: 1}, Payload: platform.Raw(map[string]string{"id": id, "digest": digest})}, Now())
		if refusal != nil {
			Reply(w, nil, refusal)
			return
		}
		removal := PackageRemoval{ID: id, OK: len(problems) == 0, Blockers: problems}
		code := http.StatusOK
		if !removal.OK {
			code = http.StatusConflict
		}
		WriteJSON(w, code, removal)
	})
}
