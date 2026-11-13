package kernel_test

import (
	"sync"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

var epoch = time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)

func TestManualClock(t *testing.T) {
	clock := kernel.NewManualClock(epoch.In(time.FixedZone("KST", 9*3600)))
	if got := clock.Now(); !got.Equal(epoch) || got.Location() != time.UTC {
		t.Fatalf("Now = %v, want %v in UTC", got, epoch)
	}
	tests := []struct {
		name    string
		advance time.Duration
		want    time.Time
		wantErr bool
	}{
		{"forward", 90 * time.Second, epoch.Add(90 * time.Second), false},
		{"zero", 0, epoch.Add(90 * time.Second), false},
		{"backwards is refused and changes nothing", -time.Second, epoch.Add(90 * time.Second), true},
		{"forward again", time.Hour, epoch.Add(90*time.Second + time.Hour), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := clock.Advance(tt.advance)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Advance error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && kernel.CodeOf(err) != kernel.CodeInvalidValue {
				t.Fatalf("code = %q", kernel.CodeOf(err))
			}
			if got := clock.Now(); !got.Equal(tt.want) {
				t.Fatalf("Now = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestManualClockSetMayGoBack(t *testing.T) {
	clock := kernel.NewManualClock(epoch)
	clock.Set(epoch.Add(-time.Hour))
	if got := clock.Now(); !got.Equal(epoch.Add(-time.Hour)) {
		t.Fatalf("Now = %v", got)
	}
}

func TestManualClockIsSafeForConcurrentUse(t *testing.T) {
	clock := kernel.NewManualClock(epoch)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := clock.Advance(time.Second); err != nil {
				t.Error(err)
			}
			clock.Now()
		}()
	}
	wg.Wait()
	if got := clock.Now(); !got.Equal(epoch.Add(50 * time.Second)) {
		t.Fatalf("Now = %v, want 50 seconds after the start", got)
	}
}

var _ kernel.Clock = (*kernel.ManualClock)(nil)
var _ kernel.Clock = kernel.SystemClock{}
