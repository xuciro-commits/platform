package build

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"platformserver/platform"
)

// ReleasePublication changes only publication metadata beside the mutable draft.
// The host commits these images with the release pointer in one release-result.
type ReleasePublication struct {
	Schema string          `json:"schema"`
	Image  json.RawMessage `json:"image"`
}

type ReleaseDeclaration struct {
	Entity       platform.Entity
	Actions      []platform.Action
	Pages        []platform.Page
	Application  *platform.Application
	Operation    *platform.Operation
	Flow         *platform.Flow
	PropertyType *platform.PropertyType
	LinkType     *platform.LinkType
	Query        *platform.NamedQuery
	Function     *platform.AIFunction
	Version      int
}

func (b *Build) ReleaseDeclaration(p ReleasePublication) (ReleaseDeclaration, error) {
	if p.Schema == SchemaPropertyType {
		saved, err := propertyTypeImage(p.Image)
		if err != nil {
			return ReleaseDeclaration{}, err
		}
		l := saved.definition()
		return ReleaseDeclaration{PropertyType: &l, Version: saved.Version}, nil
	}
	if p.Schema == SchemaLinkType {
		saved, err := linkTypeImage(p.Image)
		if err != nil {
			return ReleaseDeclaration{}, err
		}
		l := saved.definition()
		return ReleaseDeclaration{LinkType: &l, Version: saved.Version}, nil
	}
	if p.Schema == SchemaQuery {
		saved, err := queryImage(p.Image)
		if err != nil {
			return ReleaseDeclaration{}, err
		}
		q := saved.definition()
		return ReleaseDeclaration{Query: &q, Version: saved.Version}, nil
	}
	if p.Schema == SchemaCodePublish {
		code, err := codeImage(p.Image)
		if err != nil {
			return ReleaseDeclaration{}, err
		}
		installed, ok := wasPublished[Code](code.Published)
		if !ok {
			return ReleaseDeclaration{}, fmt.Errorf("compute has no frozen publication")
		}
		op := installed.definition()
		return ReleaseDeclaration{Operation: &op, Version: installed.Version}, nil
	}
	if p.Schema == SchemaProcess {
		saved, err := processImage(p.Image)
		if err != nil {
			return ReleaseDeclaration{}, err
		}
		flow := b.flowOf(saved)
		return ReleaseDeclaration{Flow: &flow, Version: saved.Version}, nil
	}
	if p.Schema == SchemaFunction {
		saved, err := functionImage(p.Image)
		if err != nil {
			return ReleaseDeclaration{}, err
		}
		f := saved.definition()
		return ReleaseDeclaration{Function: &f, Version: saved.Version}, nil
	}
	e, actions, pages, app, err := b.publicationImage(p.Schema, p.Image)
	return ReleaseDeclaration{Entity: e, Actions: actions, Pages: pages, Application: app}, err
}

// PrepareReleasePublications compiles frozen owner descriptors. It neither
// installs definitions nor edits drafts. Renaming an owning draft is not yet
// supported: the candidate has logical identities rather than editor row IDs.
func (b *Build) PrepareReleasePublications(assets []platform.ReleaseAsset) ([]ReleasePublication, []platform.ReleaseAsset, error) {
	objects, pages, apps, err := b.releaseInventory()
	if err != nil {
		return nil, nil, err
	}
	processes, err := b.processInventory()
	if err != nil {
		return nil, nil, err
	}
	var publications []ReleasePublication
	add := func(schema string, record any) error {
		raw, err := json.Marshal(record)
		if err == nil {
			publications = append(publications, ReleasePublication{schema, raw})
		}
		return err
	}
	for _, asset := range assets {
		if asset.Ref.App != ID {
			continue
		}
		switch asset.Ref.Kind {
		case platform.AssetObject:
			if slices.ContainsFunc([]platform.Entity{b.objectEntity(), b.pageEntity(), b.applicationEntity(), b.testPlanEntity(), b.processEntity(), b.queryEntity(), b.functionEntity(), b.functionCallEntity(), b.evaluationEntity()}, func(e platform.Entity) bool { return e.Type == asset.Ref.Name }) {
				continue // immutable code-owned metadata, not a tenant-authored object
			}
			var frozen Object
			if err := json.Unmarshal(asset.Body, &frozen); err != nil || TypeOf(frozen.Name) != asset.Ref.Name {
				return nil, nil, fmt.Errorf("invalid saved object %s", asset.Ref)
			}
			i := slices.IndexFunc(objects, func(o Object) bool { return !o.Archived && o.Name == frozen.Name })
			if i < 0 {
				return nil, nil, fmt.Errorf("saved object %s has no matching draft; renamed or archived definitions need an upgrade plan", asset.Ref)
			}
			frozen.Record = objects[i].Record
			objects[i].State, objects[i].Installed = "published", TypeOf(frozen.Name)
			objects[i].Published = published(frozen)
			if err := add(SchemaPublish, objects[i]); err != nil {
				return nil, nil, err
			}
		case platform.AssetPage:
			i := slices.IndexFunc(pages, func(p Page) bool { return !p.Archived && p.Name == asset.Ref.Name })
			if i < 0 { // the object's generated page has no second owner record
				continue
			}
			var pageDescriptor platform.Page
			if err := json.Unmarshal(asset.Body, &pageDescriptor); err != nil || pageDescriptor.Name != asset.Ref.Name {
				return nil, nil, fmt.Errorf("invalid saved page %s", asset.Ref)
			}
			if previous, ok := wasPublished[Page](pages[i].Published); ok && !maps.Equal(pageFunctionBindings(pageDescriptor), pageFunctionBindings(descriptor(previous))) {
				return nil, nil, fmt.Errorf("page %s changes AI function bindings; publish the binding through its owner before release activation", asset.Ref)
			}
			frozen := Page{Record: pages[i].Record, Name: pageDescriptor.Name, Title: pageDescriptor.Title, Description: pageDescriptor.Description,
				Object: pageDescriptor.Object.Name, List: pageDescriptor.ListFields, Detail: pageDescriptor.DetailFields, Selections: slices.Clone(pageDescriptor.Selections), Document: pageDescriptor.Document}
			for _, action := range pageDescriptor.Actions {
				frozen.Actions = append(frozen.Actions, action.Name)
			}
			for _, s := range pageDescriptor.Sections {
				section := Section{Breadcrumb: s.Breadcrumb, Avatar: s.Avatar, Image: s.Image, HistoryLimit: s.HistoryLimit, CommentDraftVariable: s.CommentDraftVariable, FileVariable: s.FileVariable, PdfPageVariable: s.PdfPageVariable, RecordComparison: s.RecordComparison, RecordSetVariable: s.RecordSetVariable, RecordCard: s.RecordCard, Sparkline: s.Sparkline, SparklineDecimalVariable: s.SparklineDecimalVariable, SparklineNumberVariable: s.SparklineNumberVariable, GroupValueVariable: s.GroupValueVariable, GroupSetVariable: s.GroupSetVariable, RowValueVariable: s.RowValueVariable, RowSetVariable: s.RowSetVariable, ColumnValueVariable: s.ColumnValueVariable, ColumnSetVariable: s.ColumnSetVariable, Scatter: s.Scatter, Histogram: s.Histogram, InputKind: s.InputKind, PickerValueVariable: s.PickerValueVariable, RecordPicker: s.RecordPicker, Spacer: s.Spacer, Separator: s.Separator, Notice: s.Notice, AlertValueVariable: s.AlertValueVariable, AlertBanner: s.AlertBanner, DateKind: s.DateKind, DateOffset: s.DateOffset, DateVariable: s.DateVariable, DateLabel: s.DateLabel, ChoiceSetVariable: s.ChoiceSetVariable, ChoiceVariable: s.ChoiceVariable, ChoiceInput: s.ChoiceInput, BooleanVariant: s.BooleanVariant, BooleanVariable: s.BooleanVariable, BooleanLabel: s.BooleanLabel, RangeInput: s.RangeInput, RangeMinVariable: s.RangeMinVariable, RangeMaxVariable: s.RangeMaxVariable, Leaderboard: s.Leaderboard, SummaryField: s.SummaryField, StatisticsVariable: s.StatisticsVariable, Gauge: s.Gauge, GaugeValueVariable: s.GaugeValueVariable, ProgressLabel: s.ProgressLabel, ProgressValueVariable: s.ProgressValueVariable, ProgressTotalVariable: s.ProgressTotalVariable, ProgressTotal: s.ProgressTotal, RecordGantt: s.RecordGantt, RecordCalendar: s.RecordCalendar, RecordEvents: s.RecordEvents, RecordChart: s.RecordChart, RecordList: s.RecordList, HeadingLevel: s.HeadingLevel, CountVariable: s.CountVariable, MetricPresentation: s.MetricPresentation, StatusTracker: s.StatusTracker, RecordLinks: slices.Clone(s.RecordLinks), Buttons: slices.Clone(s.Buttons), RecordView: s.RecordView, DetailPresentation: s.DetailPresentation, TableColumns: slices.Clone(s.TableColumns), ShowSearch: s.ShowSearch, Facets: slices.Clone(s.Facets), FilterSearchVariable: s.FilterSearchVariable, ID: s.ID, ConfigVersion: s.ConfigVersion, Widget: s.Widget, Title: s.Title, Width: s.Width, Object: s.Object.Name, Relation: s.Relation, Selection: s.Selection, RecordVariable: s.RecordVariable, SelectionVariable: s.SelectionVariable, FilterVariable: s.FilterVariable, SelectionSetVariable: s.SelectionSetVariable, CollectionVariable: s.CollectionVariable, ParentSelection: s.ParentSelection,
					Fields: s.Fields, Group: s.Group, Mark: s.Mark, ChartVariant: s.ChartVariant, ColumnGroup: s.ColumnGroup, TimeStart: s.TimeStart, TimeEnd: s.TimeEnd, TimeLabel: s.TimeLabel, TimeGroup: s.TimeGroup, CardLabel: s.CardLabel, Measure: s.Measure, Text: s.Text, Operation: s.Operation, Inputs: s.Inputs}
				if s.InlineEdit != nil {
					section.InlineEdit = &InlineEdit{Action: s.InlineEdit.Action.Name, Fields: slices.Clone(s.InlineEdit.Fields)}
				}
				if s.Query.Name != "" {
					section.Query = s.Query.App + "." + s.Query.Name
				}
				for _, action := range s.Actions {
					section.Actions = append(section.Actions, action.Name)
				}
				if s.Function != nil {
					var version int
					if _, err := fmt.Sscanf(s.Function.SourceVersion, definitionVersion+".function-%d", &version); err != nil || version < 1 {
						return nil, nil, fmt.Errorf("invalid saved function binding in page %s", asset.Ref)
					}
					section.Function = &platform.FunctionRef{Name: s.Function.Ref.Name, Version: version}
				}
				frozen.Sections = append(frozen.Sections, section)
			}
			pages[i].State, pages[i].Published = "published", published(frozen)
			if err := add(SchemaRelease, pages[i]); err != nil {
				return nil, nil, err
			}
		case platform.AssetApp:
			i := slices.IndexFunc(apps, func(a Application) bool { return !a.Archived && a.Name == asset.Ref.Name })
			if i < 0 {
				return nil, nil, fmt.Errorf("saved application %s has no matching draft", asset.Ref)
			}
			var frozen Application
			if err := json.Unmarshal(asset.Body, &frozen); err != nil || frozen.Name != asset.Ref.Name {
				return nil, nil, fmt.Errorf("invalid saved application %s", asset.Ref)
			}
			frozen.Record = apps[i].Record
			apps[i].State, apps[i].Published = "published", published(frozen)
			if err := add(SchemaHandOver, apps[i]); err != nil {
				return nil, nil, err
			}
		case platform.AssetFlow:
			i := slices.IndexFunc(processes, func(p Process) bool { return !p.Archived && TypeOf(p.Name) == asset.Ref.Name })
			if i < 0 {
				return nil, nil, fmt.Errorf("saved flow %s has no matching owner", asset.Ref)
			}
			frozen, err := ProcessFromReleaseAsset(asset)
			if err != nil {
				return nil, nil, err
			}
			if prior, ok := wasPublished[Process](processes[i].Published); ok {
				priorAsset, err := processReleaseAsset(prior, asset.SourceVersion)
				if err == nil {
					var a, z any
					_ = json.Unmarshal(priorAsset.Body, &a)
					_ = json.Unmarshal(asset.Body, &z)
					if reflect.DeepEqual(a, z) {
						continue
					}
				}
			}
			if processes[i].Version >= 64 {
				return nil, nil, fmt.Errorf("flow version family is full")
			}
			frozen.Record, frozen.Version, frozen.State = processes[i].Record, processes[i].Version+1, "published"
			frozen.Layout = processes[i].Layout
			processes[i].Version, processes[i].State = frozen.Version, "published"
			processes[i].Published = published(frozen)
			processes[i].Versions = append(slices.Clone(processes[i].Versions), processes[i].Published)
			if err := add(SchemaProcess, processes[i]); err != nil {
				return nil, nil, err
			}
		}
	}
	available, err := releaseAssets(objects, pages, apps, processes, b.Manifest().Version)
	if err != nil {
		return nil, nil, err
	}
	functions, err := b.functionAssets()
	if err != nil {
		return nil, nil, err
	}
	available = append(available, functions...)
	queryPublications, err := b.prepareQueryReleasePublications(assets)
	if err != nil {
		return nil, nil, err
	}
	publications = append(publications, queryPublications...)
	queries, err := b.queryAssets()
	if err != nil {
		return nil, nil, err
	}
	available = append(available, queries...)
	for _, asset := range assets {
		if asset.Ref.App == ID && asset.Ref.Kind == platform.AssetQuery {
			available = slices.DeleteFunc(available, func(old platform.ReleaseAsset) bool { return old.Ref == asset.Ref })
			available = append(available, asset)
		}
	}
	props, err := b.propertyTypeAssets()
	if err != nil {
		return nil, nil, err
	}
	available = append(available, props...)
	propertyPublications, err := b.preparePropertyTypeReleasePublications(assets)
	if err != nil {
		return nil, nil, err
	}
	publications = append(publications, propertyPublications...)
	for _, asset := range assets {
		if asset.Ref.App == ID && asset.Ref.Kind == platform.AssetPropertyType {
			available = slices.DeleteFunc(available, func(old platform.ReleaseAsset) bool { return old.Ref == asset.Ref })
			available = append(available, asset)
		}
	}
	links, err := b.linkTypeAssets()
	if err != nil {
		return nil, nil, err
	}
	available = append(available, links...)
	linkPublications, err := b.prepareLinkTypeReleasePublications(assets)
	if err != nil {
		return nil, nil, err
	}
	publications = append(publications, linkPublications...)
	for _, asset := range assets {
		if asset.Ref.App == ID && asset.Ref.Kind == platform.AssetLinkType {
			available = slices.DeleteFunc(available, func(old platform.ReleaseAsset) bool { return old.Ref == asset.Ref })
			available = append(available, asset)
		}
	}
	codePublications, err := b.PrepareCodeReleasePublications(assets)
	if err != nil {
		return nil, nil, err
	}
	publications = append(publications, codePublications...)
	code, err := b.CodeReleaseAssets()
	if err != nil {
		return nil, nil, err
	}
	available = append(available, code...)
	for _, asset := range assets {
		if asset.Ref.App == ID && asset.Ref.Kind == platform.AssetCompute {
			available = slices.DeleteFunc(available, func(prior platform.ReleaseAsset) bool { return prior.Ref == asset.Ref })
			available = append(available, asset)
		}
	}
	return publications, available, nil
}

func pageFunctionBindings(p platform.Page) map[string]string {
	bindings := map[string]string{}
	for _, section := range p.Sections {
		if section.Function != nil {
			bindings[section.Function.Ref.String()] = section.Function.SourceVersion
		}
		if section.Operation != nil {
			bindings[section.Operation.Ref.String()] = section.Operation.SourceVersion
		}
	}
	return bindings
}

// PublicationRecord identifies the metadata row, without executing a decision.
func PublicationRecord(p ReleasePublication) (string, string, error) {
	typ, _, ok := strings.Cut(p.Schema, ".publish")
	var record platform.Record
	if !ok || !slices.Contains([]string{ObjectType, PageType, AppType, ProcessType, PropertyTypeType, LinkTypeType, QueryType, FunctionType, CodeType}, typ) || json.Unmarshal(p.Image, &record) != nil || record.ID == "" {
		return "", "", fmt.Errorf("invalid release publication row")
	}
	return typ, record.ID, nil
}
