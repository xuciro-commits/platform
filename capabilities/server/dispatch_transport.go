package platformserver

import (
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/construction"
	"platformserver/internal/host"
	"platformserver/platform"
)

func (d *stagedDecision) DispatchTransport() platform.DispatchTransport {
	return hostView{t: d.tenant, app: d.tenant.app(construction.ID), decision: d}
}

func (h hostView) FreezeDispatch(c platform.Caller, subject, action string, spec platform.Dispatch, at time.Time) (string, []platform.ReleaseAsset, error) {
	if h.decision == nil || !c.Staging() || c.App != construction.ID {
		return "", nil, fmt.Errorf("Dispatch requires its original private authority")
	}
	assets, err := h.t.releaseAssetsLocked(nil, false)
	if err != nil {
		return "", nil, err
	}
	roots := []platform.AssetRef{{App: construction.ID, Kind: platform.AssetObject, Name: subject}, {App: construction.ID, Kind: platform.AssetAction, Name: action}}
	if spec.Readback.Source != "" {
		source, found := platform.Get[construction.Source](c, spec.Readback.Source)
		if !found {
			return "", nil, fmt.Errorf("Dispatch status source is unavailable")
		}
		roots = append(roots, platform.AssetRef{App: construction.ID, Kind: platform.AssetSource, Name: source.Name})
	}
	if spec.Readback.Observation != "" {
		observation, found := platform.Get[construction.Observation](c, spec.Readback.Observation)
		if !found {
			return "", nil, fmt.Errorf("Dispatch status observation is unavailable")
		}
		roots = append(roots, platform.AssetRef{App: construction.ID, Kind: platform.AssetObservation, Name: observation.Name})
	}
	closed, err := platform.Candidate(roots, assets)
	if err != nil {
		return "", nil, err
	}
	if len(closed.Bytes) > 192<<10 {
		return "", nil, fmt.Errorf("Dispatch definition exceeds its retained execution budget")
	}
	return h.t.releases.active, closed.Assets, nil
}

func (h hostView) QueueDispatch(c platform.Caller, r *pb.ChangeRecord, run platform.DispatchRun, at time.Time) *kernel.Error {
	if h.decision == nil || !c.Staging() || c.App != construction.ID || run.Intent == "" {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Dispatch transport requires its original owner")
	}
	command, body, key := run.Command, run.Request, run.Key
	if run.Compensating {
		if run.CompensationCommand == nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Dispatch has no original reverse command")
		}
		command, body, key = *run.CompensationCommand, run.CompensationRequest, run.Key+":compensate"
	}
	var request construction.WritebackRequest
	if json.Unmarshal(body, &request) != nil || request.Dispatch != run.ID || request.Version != 1 || request.Connection != run.Target || request.SafeRetry != command.SafeRetry() || key == "" {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Dispatch delivery has no original frozen request")
	}
	if refusal := h.t.checkWritebackCredential(request); refusal != nil {
		return refusal
	}
	request.DispatchKey = key
	typ, _, _ := strings.Cut(run.Subject, "/")
	applications, err := h.t.bindApplications(platform.AssetRef{App: construction.ID, Kind: platform.AssetObject, Name: typ}, run.Release, "")
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{why}", err.Error())
	}
	x := platform.Effect{ID: run.Intent, App: construction.ID, Endpoint: construction.WritebackEndpoint + request.Connection, Event: construction.ID + "/dispatch", Key: fmt.Sprintf("%s:%d", run.ID, run.Attempt), Target: run.Subject, At: at, State: "pending", Due: at, Body: string(platform.Raw(request)), Applications: applications}
	if !slices.ContainsFunc(h.decision.intents, func(e platform.Effect) bool { return e.ID == x.ID }) {
		h.decision.intents = append(h.decision.intents, x)
	}
	return nil
}

func (h hostView) ResolveDispatchDelivery(c platform.Caller, run platform.DispatchRun, at time.Time) *kernel.Error {
	if h.decision == nil || !c.Staging() || c.App != construction.ID {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Dispatch resolution requires its original owner")
	}
	owned := func(x platform.Effect) bool {
		var request construction.WritebackRequest
		return x.App == construction.ID && x.Event == construction.ID+"/dispatch" && json.Unmarshal([]byte(x.Body), &request) == nil && request.Dispatch == run.ID
	}
	h.t.opsMu.Lock()
	inFlight := slices.ContainsFunc(h.t.outbound, func(x *effect) bool {
		answering := h.decision.callbackOrigin != nil && h.decision.callbackOrigin.ID == x.ID
		return owned(x.Effect) && x.sending && !answering
	})
	h.t.opsMu.Unlock()
	if inFlight {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The dispatch delivery is still in flight")
	}
	view := h.decision.controlHost()
	for _, x := range view.outbound {
		if !owned(x.Effect) || settled(x.State) {
			continue
		}
		x.State, x.Error, x.Last = "discarded", run.Reason, at
		if slices.Contains([]string{"accepted", "running", "stuck", "failed", "completed", "compensated"}, run.State) {
			x.State = "delivered"
		}
	}
	return nil
}

func (h hostView) CheckDispatchRead(run platform.DispatchRun, at time.Time) *kernel.Error {
	member, known := h.Member(run.Member)
	if !known || !member.Active() || !h.Readable(member, run.Subject, at) {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The dispatch applicant can no longer read the subject")
	}
	read := run.Definition.Readback
	if read.Record != "" {
		return nil // Records applies the applicant's current row and field access.
	}
	var connection construction.Connection
	if json.Unmarshal(run.Connection, &connection) != nil || connection.ID == "" {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The original dispatch read connection is unavailable")
	}
	if construction.Guarded(connection.Marking) || construction.Guarded(h.t.connectionMarking(connection.ID)) {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Dispatch status requires an unclassified source projection")
	}
	if read.Source != "" {
		var source construction.Source
		if json.Unmarshal(run.Source, &source) != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The original dispatch status source is unavailable")
		}
		publisher, found := h.Member(source.Puller)
		if !found || !publisher.Active() || !h.Readable(publisher, construction.SourceType+"/"+read.Source, at) {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Dispatch status source access is unavailable")
		}
		return nil
	}
	var definition platform.ObservationDefinition
	typ, id, valid := strings.Cut(run.Subject, "/")
	if json.Unmarshal(run.Observation, &definition) != nil || !valid || definition.Object.Name != typ {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The original dispatch observation is unavailable")
	}
	caller := platform.NewCaller(runtime{h.t}, member, definition.Object.App, false, false)
	if refusal := authorizationRefusal(h.t.authorizeNative(caller, definition.Name, "read", definition.Roles, true, authorizationDetails{At: at})); refusal != nil {
		return refusal
	}
	permission := h.t.authorizeFrom(h.t.records, caller, platform.AuthzRequest{Principal: member.ID, Tenant: h.t.ID, Kind: "read", Type: typ, Record: id, Fields: []string{definition.DeviceField}}, at, false)
	if refusal := authorizationRefusal(permission); refusal != nil {
		return refusal
	}
	if len(permission.Withheld) > 0 {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The dispatch observation device is outside this member's permissions")
	}
	return nil
}

func (h hostView) RetargetDispatch(c platform.Caller, run platform.DispatchRun, target string, at time.Time) (json.RawMessage, json.RawMessage, *kernel.Error) {
	if h.decision == nil || !c.Staging() || c.App != construction.ID || c.Agent || !c.Holds(construction.ID, run.Definition.ReviewerRole) || target == "" {
		return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Dispatch retargeting requires its original owner")
	}
	typ, id, _ := strings.Cut(run.Subject, "/")
	if _, refusal := c.ReadRecord(typ, id, at); refusal != nil {
		return nil, nil, refusal
	}
	value, found := h.Record(construction.ConnectionType + "/" + target)
	connection, valid := value.(construction.Connection)
	if !found || !valid || connection.State != "ready" || connection.Archived {
		return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The selected dispatch connection is not ready")
	}
	connection, err := h.t.app(construction.ID).(host.IntegrationDefinitions).ExecutionConnection(connection)
	if err != nil || !slices.Contains([]string{"http", "odata"}, connection.Kind) {
		return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Dispatch requires a published HTTP connection")
	}
	var request construction.WritebackRequest
	if json.Unmarshal(run.Request, &request) != nil || request.Dispatch != run.ID {
		return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The original dispatch request is unavailable")
	}
	path, err := url.Parse(request.Path)
	if err != nil || path.IsAbs() || path.Host != "" {
		return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Changing target requires a relative command path")
	}
	request.Connection, request.URL, request.Secret, request.AllowPrivate = connection.ID, connection.Resolve(request.Path), connection.Secret, connection.AllowPrivate
	readConnection := run.Connection
	var original construction.Connection
	if json.Unmarshal(run.Connection, &original) == nil && original.ID == run.Target {
		readConnection = platform.Raw(connection)
	}
	return platform.Raw(request), readConnection, nil
}

var _ host.DispatchHost = hostView{}
