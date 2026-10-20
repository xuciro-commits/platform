package journal

import (
	"context"
	"errors"
	"os"
	"testing"
)

func TestFileUnsyncedAppendStopsFurtherSharedWrites(t *testing.T) {
	j, err := OpenFile(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	ctx := context.Background()
	for _, tenant := range []string{"failed", "neighbor"} {
		if _, err := j.Entries(ctx, tenant, 0); err != nil {
			t.Fatal(err)
		}
	}
	file := j.file
	j.file = writer // write succeeds, but a pipe cannot fsync an accepted entry
	err = j.Append(ctx, "failed", Entry{Kind: "test"})
	j.file = file
	if !errors.Is(err, ErrCommitUnknown) || j.Position("failed") != 0 {
		t.Fatalf("unsynced entry was treated as committed: %v", err)
	}
	if err := j.Append(ctx, "neighbor", Entry{Kind: "test"}); !errors.Is(err, ErrCommitUnknown) || j.Position("neighbor") != 0 {
		t.Fatalf("shared journal accepted writes after an unresolved append: %v", err)
	}
	if info, err := file.Stat(); err != nil || info.Size() != 0 {
		t.Fatalf("an entry followed the unresolved append: %v", err)
	}
}
