package platformserver

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestJournalConcurrentOpen(t *testing.T) {
	dsn := os.Getenv("PLATFORM_TEST_DATABASE")
	if dsn == "" {
		t.Skip("PLATFORM_TEST_DATABASE is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	start := make(chan struct{})
	results := make(chan error, 4)
	for range 4 {
		go func() {
			<-start
			journal, err := OpenJournal(ctx, dsn)
			if err == nil {
				err = journal.Pool().Ping(ctx)
				journal.Close()
			}
			results <- err
		}()
	}
	close(start)
	for range 4 {
		if err := <-results; err != nil {
			t.Errorf("concurrent host journal startup: %v", err)
		}
	}
}
