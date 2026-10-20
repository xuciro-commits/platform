package platform

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"slices"
)

// RecordImageFormat is a previous persisted shape declared by its model owner.
// Model is the original struct embedding Record. Convert changes only business
// fields and returns the current shape; it must not read state or emit effects.
// The host strictly decodes Model before calling Convert and checks the current
// model and unchanged record identity afterwards.
type RecordImageFormat struct {
	Format  string
	Model   any
	Convert func(json.RawMessage) (json.RawMessage, error)
}

func checkRecordImageFormats(info EntityInfo) error {
	seen := map[string]bool{info.RecordFormat: true}
	for _, image := range info.RecordImages {
		t := reflect.TypeOf(image.Model)
		if seen[image.Format] || t == nil || t.Kind() != reflect.Struct || t.NumField() == 0 ||
			t.Field(0).Type != reflect.TypeFor[Record]() || !t.Field(0).Anonymous || image.Convert == nil {
			return fmt.Errorf("entity %s: invalid or duplicate historical record format %q", info.Type, image.Format)
		}
		seen[image.Format] = true
	}
	return nil
}

func decodeRecordImage(raw json.RawMessage, model reflect.Type) (Record, error) {
	value := reflect.New(model)
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value.Interface()); err != nil {
		return Record{}, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return Record{}, fmt.Errorf("record image has trailing data")
	}
	return value.Elem().Field(0).Interface().(Record), nil
}

// DecodeRecordImage validates one saved shape and converts it only through its
// owner's explicit declaration. It leaves the supplied original bytes untouched;
// callers retain those bytes for historical identity and predecessor checks.
func DecodeRecordImage(info EntityInfo, format string, raw json.RawMessage) (json.RawMessage, error) {
	if format == info.RecordFormat {
		_, err := decodeRecordImage(raw, info.Go)
		return raw, err
	}
	for _, image := range info.RecordImages {
		if image.Format != format {
			continue
		}
		before, err := decodeRecordImage(raw, reflect.TypeOf(image.Model))
		if err != nil {
			return nil, fmt.Errorf("record format %q: %w", format, err)
		}
		converted, err := image.Convert(slices.Clone(raw))
		if err != nil {
			return nil, err
		}
		after, err := decodeRecordImage(converted, info.Go)
		if err != nil {
			return nil, fmt.Errorf("record format %q: %w", info.RecordFormat, err)
		}
		if !reflect.DeepEqual(before, after) {
			return nil, fmt.Errorf("record format conversion changed record identity")
		}
		return converted, nil
	}
	return nil, fmt.Errorf("entity %s: unknown record image format %q", info.Type, format)
}
