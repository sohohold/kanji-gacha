// Package metrics writes CloudWatch embedded metric format (EMF) records. Each
// record is one JSON line; CloudWatch Logs extracts the metrics from the Lambda
// log stream, so no PutMetricData call or extra IAM permission is needed.
package metrics

import (
	"encoding/json"
	"io"
	"sync"
	"time"
)

// EMF records generation metrics. A nil *EMF records nothing, which keeps local
// runs and tests free of metric lines.
//
// Every dimension value becomes a separate custom metric with its own monthly
// charge, so the only dimension is the rarity, which the generator config limits
// to the API enum. Input, SVG, IDs and addresses are never written.
type EMF struct {
	Namespace string
	Now       func() time.Time
	mu        sync.Mutex
	w         io.Writer
}

func NewEMF(w io.Writer, namespace string) *EMF {
	return &EMF{Namespace: namespace, Now: time.Now, w: w}
}

type metric struct {
	Name string `json:"Name"`
	Unit string `json:"Unit"`
}
type directive struct {
	Namespace  string     `json:"Namespace"`
	Dimensions [][]string `json:"Dimensions"`
	Metrics    []metric   `json:"Metrics"`
}

// Generated records one successful draw: how long the generator took, whether
// it had to return the fallback candidate, and which rarity was drawn. The time
// covers the generator only, not input validation or saving.
func (m *EMF) Generated(d time.Duration, rarity string, fallback bool) {
	if m == nil {
		return
	}
	fb := 0
	if fallback {
		fb = 1
	}
	m.write([]directive{
		{m.Namespace, [][]string{{}}, []metric{{"GenerationTime", "Milliseconds"}, {"Fallback", "Count"}}},
		{m.Namespace, [][]string{{"Rarity"}}, []metric{{"Glyphs", "Count"}}},
	}, map[string]any{"GenerationTime": float64(d.Microseconds()) / 1000, "Fallback": fb, "Glyphs": 1, "Rarity": rarity})
}

// GenerationFailed records a draw the generator could not complete. Its time is
// left out of GenerationTime so that metric stays the time of served results.
func (m *EMF) GenerationFailed() {
	if m == nil {
		return
	}
	m.write([]directive{{m.Namespace, [][]string{{}}, []metric{{"GenerationFailed", "Count"}}}}, map[string]any{"GenerationFailed": 1})
}

func (m *EMF) write(ds []directive, values map[string]any) {
	values["_aws"] = map[string]any{"Timestamp": m.Now().UnixMilli(), "CloudWatchMetrics": ds}
	b, e := json.Marshal(values)
	if e != nil {
		return
	}
	// One Write per line under the lock, so concurrent requests never interleave
	// and each line reaches CloudWatch Logs as a whole JSON event.
	m.mu.Lock()
	defer m.mu.Unlock()
	m.w.Write(append(b, '\n'))
}
