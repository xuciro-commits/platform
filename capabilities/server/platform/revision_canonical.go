package platform

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"slices"
	"strconv"
	"strings"
)

func canonicalBody(raw []byte) ([]byte, error) {
	if len(raw) == 0 || len(raw) > 1<<20 {
		return nil, fmt.Errorf("descriptor is empty or exceeds 1 MiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	value, err := readJSONValue(decoder)
	if err != nil {
		return nil, fmt.Errorf("invalid descriptor JSON: %w", err)
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, fmt.Errorf("descriptor JSON has trailing content")
	}
	if _, ok := value.(map[string]any); !ok {
		return nil, fmt.Errorf("descriptor must be a JSON object")
	}
	return json.Marshal(value)
}

func readJSONValue(d *json.Decoder) (any, error) {
	token, err := d.Token()
	if err != nil {
		return nil, err
	}
	switch token {
	case json.Delim('{'):
		object := map[string]any{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return nil, err
			}
			name := key.(string)
			if _, exists := object[name]; exists {
				return nil, fmt.Errorf("duplicate JSON key %q", name)
			}
			object[name], err = readJSONValue(d)
			if err != nil {
				return nil, err
			}
		}
		_, err := d.Token()
		return object, err
	case json.Delim('['):
		array := []any{}
		for d.More() {
			item, err := readJSONValue(d)
			if err != nil {
				return nil, err
			}
			array = append(array, item)
		}
		_, err := d.Token()
		return array, err
	default:
		if number, ok := token.(json.Number); ok {
			return canonicalNumber(number)
		}
		return token, nil
	}
}

func canonicalNumber(number json.Number) (json.Number, error) {
	text := string(number)
	if len(text) > 4096 {
		return "", fmt.Errorf("descriptor number exceeds canonical range")
	}
	if i := strings.IndexAny(text, "eE"); i >= 0 {
		exponent, err := strconv.Atoi(text[i+1:])
		if err != nil || exponent < -1024 || exponent > 1024 {
			return "", fmt.Errorf("descriptor exponent exceeds canonical range")
		}
	}
	value, ok := new(big.Rat).SetString(text)
	if !ok {
		return "", fmt.Errorf("invalid descriptor number %q", text)
	}
	if value.IsInt() {
		return json.Number(value.Num().String()), nil
	}
	denominator := new(big.Int).Set(value.Denom())
	two, five := big.NewInt(2), big.NewInt(5)
	quotient, remainder := new(big.Int), new(big.Int)
	precision := 0
	for _, factor := range []*big.Int{two, five} {
		count := 0
		for {
			quotient.QuoRem(denominator, factor, remainder)
			if remainder.Sign() != 0 {
				break
			}
			denominator.Set(quotient)
			count++
		}
		if count > precision {
			precision = count
		}
	}
	if denominator.Cmp(big.NewInt(1)) != 0 || precision > 1024 {
		return "", fmt.Errorf("descriptor number exceeds canonical range")
	}
	decimal := strings.TrimRight(value.FloatString(precision), "0")
	return json.Number(strings.TrimSuffix(decimal, ".")), nil
}

func compareRef(a, b AssetRef) int {
	if a.App != b.App {
		return compareString(a.App, b.App)
	}
	if a.Kind != b.Kind {
		return compareString(string(a.Kind), string(b.Kind))
	}
	return compareString(a.Name, b.Name)
}

func compareString(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func CandidateDiff(before, after ReleaseCandidate) (added, removed, changed []AssetRef, err error) {
	before, err = ReadCandidate(before.ID, before.Bytes)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("previous candidate: %w", err)
	}
	after, err = ReadCandidate(after.ID, after.Bytes)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("next candidate: %w", err)
	}
	prior := map[AssetRef][]byte{}
	for _, asset := range before.Assets {
		raw, _ := json.Marshal(asset)
		prior[asset.Ref] = raw
	}
	for _, asset := range after.Assets {
		raw, _ := json.Marshal(asset)
		old, found := prior[asset.Ref]
		if !found {
			added = append(added, asset.Ref)
		} else if !bytes.Equal(old, raw) {
			changed = append(changed, asset.Ref)
		}
		delete(prior, asset.Ref)
	}
	for ref := range prior {
		removed = append(removed, ref)
	}
	slices.SortFunc(added, compareRef)
	slices.SortFunc(removed, compareRef)
	slices.SortFunc(changed, compareRef)
	return added, removed, changed, nil
}
