package metrics

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func record(t *testing.T, line string) map[string]any {
	t.Helper()
	var v map[string]any
	if e := json.Unmarshal([]byte(line), &v); e != nil {
		t.Fatal(e, line)
	}
	return v
}

func TestGeneratedRecord(t *testing.T) {
	var buf bytes.Buffer
	m := NewEMF(&buf, "KanjiGacha")
	m.Now = func() time.Time { return time.UnixMilli(1700000000123) }
	m.Generated(1500*time.Microsecond, "SR", true)
	if !strings.HasSuffix(buf.String(), "}\n") || strings.Count(buf.String(), "\n") != 1 {
		t.Fatalf("not one JSON line: %q", buf.String())
	}
	got := record(t, buf.String())
	want := map[string]any{
		"_aws": map[string]any{
			"Timestamp": 1700000000123.0,
			"CloudWatchMetrics": []any{
				map[string]any{"Namespace": "KanjiGacha", "Dimensions": []any{[]any{}}, "Metrics": []any{
					map[string]any{"Name": "GenerationTime", "Unit": "Milliseconds"},
					map[string]any{"Name": "Fallback", "Unit": "Count"},
				}},
				map[string]any{"Namespace": "KanjiGacha", "Dimensions": []any{[]any{"Rarity"}}, "Metrics": []any{
					map[string]any{"Name": "Glyphs", "Unit": "Count"},
				}},
			},
		},
		"GenerationTime": 1.5,
		"Fallback":       1.0,
		"Glyphs":         1.0,
		"Rarity":         "SR",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}
	buf.Reset()
	m.Generated(time.Millisecond, "N", false)
	if record(t, buf.String())["Fallback"] != 0.0 {
		t.Fatal("fallback counted", buf.String())
	}
}

func TestGenerationFailedRecord(t *testing.T) {
	var buf bytes.Buffer
	m := NewEMF(&buf, "KanjiGacha")
	m.GenerationFailed()
	got := record(t, buf.String())
	if got["GenerationFailed"] != 1.0 {
		t.Fatal(got)
	}
	// A failure has no served result, so it must not add to the time or the
	// rarity distribution.
	for _, k := range []string{"GenerationTime", "Glyphs", "Rarity"} {
		if _, ok := got[k]; ok {
			t.Fatalf("failure record carries %s: %v", k, got)
		}
	}
	d := got["_aws"].(map[string]any)["CloudWatchMetrics"].([]any)
	if len(d) != 1 || !reflect.DeepEqual(d[0].(map[string]any)["Dimensions"], []any{[]any{}}) {
		t.Fatal(d)
	}
}

func TestNilRecordsNothing(t *testing.T) {
	var m *EMF
	m.Generated(time.Second, "N", false)
	m.GenerationFailed()
}

type lineWriter struct {
	mu    sync.Mutex
	lines []string
}

func (w *lineWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.lines = append(w.lines, string(b))
	return len(b), nil
}

func TestConcurrentWritesStayWhole(t *testing.T) {
	w := &lineWriter{}
	m := NewEMF(w, "KanjiGacha")
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.Generated(time.Millisecond, "R", false)
		}()
	}
	wg.Wait()
	if len(w.lines) != 50 {
		t.Fatal(len(w.lines))
	}
	for _, l := range w.lines {
		record(t, l)
	}
}
