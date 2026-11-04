package purchase

import (
	"github.com/ovh-buy/server/internal/ovh"
	"testing"
)

func TestDispatchStatisticsUseBoundedActualWrites(t *testing.T) {
	if summarizeDispatch(nil) != nil {
		t.Fatal("empty samples have percentiles")
	}
	if stats := summarizeDispatch([]int64{100, 10, 30, 20}); stats.P50US != 20 || stats.P99US != 100 || stats.Samples != 4 {
		t.Fatal(stats)
	}
	for i := 0; i < 300; i++ {
		tl := newTimeline()
		tl.dispatchKey = "stats-test"
		tl.mark("test")
		tl.dispatch = &ovh.DispatchSnapshot{WroteRequest: true, DetectionToWriteUS: int64(i)}
		recordTiming("stats-test-task", tl, "ordered")
	}
	last := LastTimings()["stats-test-task"].DispatchStats
	if last == nil || last.Samples != 256 || last.P50US != 171 || last.P99US != 297 {
		t.Fatal(last)
	}
	tl := newTimeline()
	tl.dispatchKey = "stats-test"
	tl.mark("test")
	tl.dispatch = &ovh.DispatchSnapshot{WroteRequest: false}
	recordTiming("stats-test-task", tl, "failed")
	if LastTimings()["stats-test-task"].DispatchStats.Samples != 256 {
		t.Fatal("failed write changed sample count")
	}
}
