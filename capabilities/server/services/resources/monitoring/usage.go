package monitoring

import (
	"cmp"
	"encoding/json"
	"maps"
	"math"
	"platformserver/kernel/acceptedformat"
	"platformserver/kernel/protocol"
	"platformserver/kernel/records"
	"platformserver/platform"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"
)

type JournalSlice struct {
	At   time.Time
	App  string
	Body []byte
}

type UsageInput struct {
	Tenant   string
	Now      time.Time
	Records  *records.Store
	AIUsage  []platform.AIUsage
	Packages map[string]*protocol.InstalledPackage
	Position int64
	Entries  func(after int64) ([]JournalSlice, error)
}

type writeOrigin struct {
	Key    string
	App    string
	Record bool
}

type writeEvent struct {
	Seq    int64
	Day    string
	Origin writeOrigin
}

type UsageProjector struct {
	mu          sync.Mutex
	seq         int64
	byDay       map[string]int64
	byDayOrigin map[string]map[string]int64
	origins     map[string]writeOrigin
	tail        []writeEvent
}

const usageTailLimit = 512

func (p *UsageProjector) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.seq = 0
	p.byDay = nil
	p.byDayOrigin = nil
	p.origins = nil
	p.tail = nil
}

func (p *UsageProjector) snapshot(position int64, entries func(after int64) ([]JournalSlice, error)) (map[string]int64, map[string]map[string]int64, map[string]writeOrigin) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if position < p.seq && len(p.tail) == 0 {
		p.seq = 0
		p.byDay = nil
		p.byDayOrigin = nil
		p.origins = nil
	}
	if p.byDay == nil {
		p.byDay = map[string]int64{}
		p.byDayOrigin = map[string]map[string]int64{}
		p.origins = map[string]writeOrigin{}
	}
	if entries != nil && position > p.seq {
		if list, err := entries(p.seq); err == nil {
			limit := int(position - p.seq)
			if limit > len(list) {
				limit = len(list)
			}
			for i := 0; i < limit; i++ {
				seq := p.seq + int64(i) + 1
				item := list[i]
				origin, ok := classifyAcceptedWrite(item.App, item.Body)
				if !ok {
					continue
				}
				day := item.At.UTC().Format(time.DateOnly)
				p.byDay[day]++
				if p.byDayOrigin[day] == nil {
					p.byDayOrigin[day] = map[string]int64{}
				}
				p.byDayOrigin[day][origin.Key]++
				p.origins[origin.Key] = origin
				p.tail = append(p.tail, writeEvent{Seq: seq, Day: day, Origin: origin})
				if len(p.tail) > usageTailLimit {
					p.tail = p.tail[len(p.tail)-usageTailLimit:]
				}
			}
			p.seq += int64(limit)
		}
	}
	byDay := maps.Clone(p.byDay)
	byOrigin := make(map[string]map[string]int64, len(p.byDayOrigin))
	for day, bucket := range p.byDayOrigin {
		byOrigin[day] = maps.Clone(bucket)
	}
	origins := maps.Clone(p.origins)
	if position > 0 && position < p.seq {
		for i := len(p.tail) - 1; i >= 0 && p.tail[i].Seq > position; i-- {
			ev := p.tail[i]
			byDay[ev.Day]--
			if byDay[ev.Day] <= 0 {
				delete(byDay, ev.Day)
			}
			if bucket := byOrigin[ev.Day]; bucket != nil {
				bucket[ev.Origin.Key]--
				if bucket[ev.Origin.Key] <= 0 {
					delete(bucket, ev.Origin.Key)
				}
			}
		}
	}
	return byDay, byOrigin, origins
}

func classifyAcceptedWrite(entryApp string, raw []byte) (writeOrigin, bool) {
	if len(raw) == 0 {
		return writeOrigin{}, false
	}
	var envelope struct {
		Kind string `json:"kind"`
		App  string `json:"app"`
	}
	if json.Unmarshal(raw, &envelope) != nil {
		return writeOrigin{}, false
	}
	app := cmp.Or(envelope.App, entryApp, "platform")
	switch envelope.Kind {
	case "refusal", "usage-result", "work-result", "effect-result", "operation-claim", "host-result":
		return writeOrigin{}, false
	case "record-batch":
		res, receipt, err := acceptedformat.DecodeBatch(raw)
		if err != nil {
			return writeOrigin{}, false
		}
		app = cmp.Or(res.App, app)
		if sub, err := acceptedformat.BatchSubmission(res, receipt); err == nil {
			if typ := sub.GetTarget().GetType(); typ != "" {
				return writeOrigin{Key: typ, App: app, Record: true}, true
			}
		}
		if len(res.Rows) > 0 && res.Rows[0].Image.Type != "" {
			return writeOrigin{Key: res.Rows[0].Image.Type, App: app, Record: true}, true
		}
		return writeOrigin{Key: "app:" + app, App: app}, true
	case "input-result":
		res, err := acceptedformat.DecodeInput(raw)
		if err != nil || res.Refusal != nil {
			return writeOrigin{}, false
		}
		app = cmp.Or(res.App, app)
		return writeOrigin{Key: "app:" + app, App: app}, true
	case "release-result", "composite-result", "agent-result":
		return writeOrigin{Key: "app:" + app, App: app}, true
	default:
		res, _, err := acceptedformat.DecodeRecord(raw)
		if err != nil {
			return writeOrigin{}, false
		}
		app = cmp.Or(res.App, app)
		if res.Row.Type != "" {
			return writeOrigin{Key: res.Row.Type, App: app, Record: true}, true
		}
		return writeOrigin{Key: "app:" + app, App: app}, true
	}
}

func ProjectUsage(m *Monitoring, in UsageInput) protocol.TenantUsage {
	now := in.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	today := now.Format(time.DateOnly)
	monthPrefix := today[:7]
	days := monthDaysThrough(now)

	projector := &UsageProjector{}
	if m != nil && m.projector != nil {
		projector = m.projector
	}
	byDay, byDayOrigin, origins := projector.snapshot(in.Position, in.Entries)

	typeTitles := map[string]string{}
	typeApps := map[string]string{}
	recordSources := []protocol.UsageSource{}
	storageByType := map[string]int64{}
	recordsByDay := map[string]int64{}
	storageByDay := map[string]int64{}
	seenHashes := map[string]bool{}
	var totalRecords int64
	var totalStorage int64

	if in.Records != nil {
		types := in.Records.SortedTypes()
		for _, et := range types {
			info := et.Info()
			typeTitles[info.Type] = cmp.Or(info.Title, info.Type)
			typeApps[info.Type] = info.App
		}
		for _, et := range types {
			info := et.Info()
			if info.ReadOnlyProjection() {
				continue
			}
			var activeCount int64
			var typeBytes int64
			for _, row := range et.Rows().All() {
				if row == nil || !row.Value.IsValid() {
					continue
				}
				val := row.Value
				if val.Kind() == reflect.Pointer {
					if val.IsNil() {
						continue
					}
					val = val.Elem()
				}
				hdr := records.Header(val)
				if hdr.Archived {
					continue
				}
				activeCount++
				var rowBytes int64
				if raw, err := row.Image(); err == nil {
					rowBytes += int64(len(raw))
				}
				switch typed := val.Interface().(type) {
				case platform.File:
					if typed.Size > 0 && (typed.Hash == "" || !seenHashes[typed.Hash]) {
						if typed.Hash != "" {
							seenHashes[typed.Hash] = true
						}
						rowBytes += int64(typed.Size)
					}
				case platform.MediaSet:
					for _, item := range typed.Items {
						if item.Size > 0 && (item.Hash == "" || !seenHashes[item.Hash]) {
							if item.Hash != "" {
								seenHashes[item.Hash] = true
							}
							rowBytes += int64(item.Size)
						}
					}
				case platform.MediaSetVersion:
					for _, item := range typed.Items {
						if item.Size > 0 && (item.Hash == "" || !seenHashes[item.Hash]) {
							if item.Hash != "" {
								seenHashes[item.Hash] = true
							}
							rowBytes += int64(item.Size)
						}
					}
				}
				typeBytes += rowBytes
				if !hdr.Created.At.IsZero() {
					createdDay := hdr.Created.At.UTC().Format(time.DateOnly)
					if strings.HasPrefix(createdDay, monthPrefix) {
						recordsByDay[createdDay]++
						storageByDay[createdDay] += rowBytes
					}
				}
			}
			if activeCount > 0 {
				totalRecords += activeCount
				recordSources = append(recordSources, recordUsageSource(info.Type, cmp.Or(info.Title, info.Type), info.App, activeCount))
			}
			if typeBytes > 0 {
				totalStorage += typeBytes
				storageByType[info.Type] += typeBytes
			}
		}
	}
	sortUsageSources(recordSources)

	storageSources := make([]protocol.UsageSource, 0, len(storageByType))
	for typ, size := range storageByType {
		storageSources = append(storageSources, recordUsageSource(typ, cmp.Or(typeTitles[typ], typ), typeApps[typ], size))
	}
	sortUsageSources(storageSources)

	dailyWrites := make([]protocol.UsageDay, 0, len(byDay))
	var writesMonth int64
	for _, day := range slices.Sorted(maps.Keys(byDay)) {
		count := byDay[day]
		dailyWrites = append(dailyWrites, protocol.UsageDay{Date: day, Value: count})
		if strings.HasPrefix(day, monthPrefix) {
			writesMonth += count
		}
	}
	writesToday := byDay[today]
	writeSources := []protocol.UsageSource{}
	for key, count := range byDayOrigin[today] {
		origin := origins[key]
		if origin.Record && typeTitles[key] != "" {
			writeSources = append(writeSources, recordUsageSource(key, typeTitles[key], cmp.Or(typeApps[key], origin.App), count))
		} else {
			label := strings.TrimPrefix(key, "app:")
			writeSources = append(writeSources, protocol.UsageSource{
				Key:   key,
				Title: label,
				App:   origin.App,
				Value: count,
				Path:  "/operations/audit?app=" + origin.App,
			})
		}
	}
	sortUsageSources(writeSources)

	var aiTokens, aiInput, aiOutput, aiCalls int64
	var aiCost float64
	aiByModelTokens := map[string]int64{}
	aiByModelCost := map[string]float64{}
	aiByDayTokens := map[string]int64{}
	aiByDayCost := map[string]float64{}
	for _, u := range in.AIUsage {
		tokens := int64(u.Input + u.Output)
		aiInput += int64(u.Input)
		aiOutput += int64(u.Output)
		aiTokens += tokens
		aiCalls++
		aiCost += u.Cost
		model := cmp.Or(u.Model, "unknown")
		aiByModelTokens[model] += tokens
		aiByModelCost[model] += u.Cost
		if !u.At.IsZero() {
			day := u.At.UTC().Format(time.DateOnly)
			if strings.HasPrefix(day, monthPrefix) {
				aiByDayTokens[day] += tokens
				aiByDayCost[day] += u.Cost
			}
		}
	}
	aiCost = roundMoney6(aiCost)
	aiSources := make([]protocol.UsageSource, 0, len(aiByModelTokens))
	for model, tokens := range aiByModelTokens {
		aiSources = append(aiSources, protocol.UsageSource{
			Key:       model,
			Title:     model,
			App:       protocol.AIApp,
			Value:     tokens,
			Cost:      roundMoney6(aiByModelCost[model]),
			ObjectSet: &platform.ObjectSet{Object: protocol.AIModelType},
			Path:      "/admin/models",
		})
	}
	sortUsageSources(aiSources)

	var installedCount int64
	pkgSources := []protocol.UsageSource{}
	pkgByDay := map[string]int64{}
	for _, id := range slices.Sorted(maps.Keys(in.Packages)) {
		pkg := in.Packages[id]
		if pkg == nil || pkg.State != "active" {
			continue
		}
		installedCount++
		title := cmp.Or(pkg.Title, pkg.ID)
		if pkg.Version != "" {
			title += " " + pkg.Version
		}
		pkgSources = append(pkgSources, protocol.UsageSource{
			Key:       pkg.ID,
			Title:     title,
			App:       pkg.Namespace,
			Value:     1,
			ObjectSet: &platform.ObjectSet{Object: protocol.PackageType},
			Path:      "/delivery/installations",
		})
		if !pkg.InstalledAt.IsZero() {
			day := pkg.InstalledAt.UTC().Format(time.DateOnly)
			if strings.HasPrefix(day, monthPrefix) {
				pkgByDay[day]++
			}
		}
	}
	sortUsageSources(pkgSources)

	metrics := []protocol.UsageMetric{
		{Kind: "records", Unit: "records", Used: totalRecords, Trend: buildTrend(days, recordsByDay, nil), Sources: recordSources},
		{Kind: "storage", Unit: "bytes", Used: totalStorage, Trend: buildTrend(days, storageByDay, nil), Sources: storageSources},
		{Kind: "writes", Unit: "writes/day", Used: writesToday, Trend: buildTrend(days, byDay, nil), Sources: writeSources},
		{Kind: "ai", Unit: "tokens", Used: aiTokens, Cost: aiCost, Trend: buildTrend(days, aiByDayTokens, aiByDayCost), Sources: aiSources},
		{Kind: "packages", Unit: "packages", Used: installedCount, Trend: buildTrend(days, pkgByDay, nil), Sources: pkgSources},
	}

	return protocol.TenantUsage{
		Tenant:            in.Tenant,
		At:                now,
		Month:             monthPrefix,
		Records:           totalRecords,
		StorageBytes:      totalStorage,
		WritesToday:       writesToday,
		WritesMonth:       writesMonth,
		DailyWrites:       dailyWrites,
		AITokens:          aiTokens,
		AIInputTokens:     aiInput,
		AIOutputTokens:    aiOutput,
		AICalls:           aiCalls,
		AICost:            aiCost,
		InstalledPackages: installedCount,
		Metrics:           metrics,
	}
}

func recordUsageSource(typ, title, app string, value int64) protocol.UsageSource {
	return protocol.UsageSource{
		Key:       typ,
		Title:     title,
		App:       app,
		Value:     value,
		ObjectSet: &platform.ObjectSet{Object: typ},
		Path:      "/ontology/objects?type=" + typ,
	}
}

func sortUsageSources(list []protocol.UsageSource) {
	slices.SortFunc(list, func(a, b protocol.UsageSource) int {
		if a.Value != b.Value {
			if a.Value > b.Value {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Key, b.Key)
	})
}

func monthDaysThrough(now time.Time) []string {
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	var out []string
	for cur := start; !cur.After(end); cur = cur.AddDate(0, 0, 1) {
		out = append(out, cur.Format(time.DateOnly))
	}
	return out
}

func buildTrend(days []string, values map[string]int64, costs map[string]float64) []protocol.UsageDay {
	out := make([]protocol.UsageDay, 0, len(days))
	for _, day := range days {
		var cost float64
		if costs != nil {
			cost = roundMoney6(costs[day])
		}
		out = append(out, protocol.UsageDay{Date: day, Value: values[day], Cost: cost})
	}
	return out
}

func roundMoney6(v float64) float64 {
	return math.Round(v*1e6) / 1e6
}
