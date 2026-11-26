package store

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestUpgradeAppliesEachStepAndRefusesGaps(t *testing.T) {
	steps := map[int]Migration{
		1: {From: 1, Name: "rename", Apply: func(e Entry) (Entry, error) { e.Kind = "renamed"; return e, nil }},
		2: {From: 2, Name: "body", Apply: func(e Entry) (Entry, error) { e.Body = json.RawMessage(`{"v":3}`); return e, nil }},
	}
	got, err := upgrade(Entry{Kind: "accepted-result", Body: json.RawMessage(`{}`), Epoch: 1}, 1, 3, steps)
	if err != nil || got.Epoch != 3 || got.Kind != "renamed" || string(got.Body) != `{"v":3}` {
		t.Fatalf("upgrade 1→3 = %+v, %v", got, err)
	}
	same, err := upgrade(Entry{Kind: "accepted-result", Epoch: 3}, 1, 3, steps)
	if err != nil || same.Kind != "accepted-result" {
		t.Fatalf("current epoch entry changed: %+v, %v", same, err)
	}
	for _, epoch := range []int{0, 4} {
		if _, err := upgrade(Entry{Epoch: epoch}, 1, 3, steps); err == nil {
			t.Fatalf("epoch %d outside frozen..target was read", epoch)
		}
	}
	delete(steps, 2)
	if _, err := upgrade(Entry{Epoch: 1}, 1, 3, steps); err == nil || !strings.Contains(err.Error(), "no migration to epoch 3") {
		t.Fatalf("missing step 2→3 not named: %v", err)
	}
	steps[2] = Migration{From: 2, Name: "broken", Apply: func(Entry) (Entry, error) { return Entry{}, errors.New("bad body") }}
	if _, err := upgrade(Entry{Epoch: 2}, 1, 3, steps); err == nil || !strings.Contains(err.Error(), "broken") {
		t.Fatalf("failing migration not named: %v", err)
	}
	if FrozenEpoch > FormatEpoch {
		t.Fatalf("FrozenEpoch %d is after FormatEpoch %d", FrozenEpoch, FormatEpoch)
	}
}
