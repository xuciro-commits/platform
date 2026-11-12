package kernel

import (
	"platformserver/kernel/records"
	"regexp"
	"strconv"
	"strings"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

var migratedSequenceNumber = regexp.MustCompile(`\{n(?::[1-9])?\}`)

func (d *Decision) SequenceWaterline(app string, sequence platform.Sequence, year int) (int, *kernel.Error) {
	d.Records.Lock()
	defer d.Records.Unlock()
	owner := d.Records.Type(sequence.SeedRecord)
	if owner == nil || owner.Info().App != app {
		return 0, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "A migrated number range requires its original record owner")
	}
	field, found := owner.Info().Field(sequence.SeedField)
	if !found || field.Type != "text" {
		return 0, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A migrated number range requires its original text field")
	}
	index := migratedSequenceNumber.FindStringIndex(sequence.Pattern)
	if len(index) != 2 {
		return 0, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A migrated number range requires one numeric placeholder")
	}
	prefix := strings.ReplaceAll(sequence.Pattern[:index[0]], "{year}", strconv.Itoa(year))
	suffix := strings.ReplaceAll(sequence.Pattern[index[1]:], "{year}", strconv.Itoa(year))
	last := 0
	for _, row := range owner.Rows().All() {
		value := records.BusinessField(owner.Info(), row.Value, sequence.SeedField)
		number, ok := strings.CutPrefix(value, prefix)
		if !ok || !strings.HasSuffix(number, suffix) {
			continue
		}
		number = strings.TrimSuffix(number, suffix)
		n, err := strconv.Atoi(number)
		if err == nil && n > last && sequence.Format(year, n) == value {
			last = n
		}
	}
	return last, nil
}
