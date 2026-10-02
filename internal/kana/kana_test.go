package kana

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

// Test IDs: IN-01 to IN-43, the id of each case in shared/kana-cases.json.
func TestSharedCases(t *testing.T) {
	b, e := os.ReadFile("../../shared/kana-cases.json")
	if e != nil {
		t.Fatal(e)
	}
	var cases []struct {
		ID, Name                string
		Input                   Input
		Valid                   bool
		Normalized, Code, Field string
		Morae                   []string
	}
	if e = json.Unmarshal(b, &cases); e != nil {
		t.Fatal(e)
	}
	for _, c := range cases {
		t.Run(c.ID+" "+c.Name, func(t *testing.T) {
			r, issue := Validate(c.Input)
			if c.Valid {
				if issue != nil {
					t.Fatal(issue)
				}
				if r.Reading != c.Normalized || !reflect.DeepEqual(r.Morae, c.Morae) {
					t.Fatalf("got %+v", r)
				}
			} else if issue == nil || issue.Code != c.Code || issue.Field != c.Field {
				t.Fatalf("got %+v, want %s/%s", issue, c.Field, c.Code)
			}
		})
	}
}

// Test IDs: REQ-01 to REQ-16, one per case.
func TestJSONBoundary(t *testing.T) {
	for _, c := range []struct {
		id, body string
		status   int
	}{
		{"REQ-01", "{\"reading\":\"\xff\"}", 400},
		{"REQ-02", `{"mode":"on","reading":"\ud800"}`, 400},
		{"REQ-03", `{"mode":"on","reading":"\udc00"}`, 400},
		{"REQ-04", `{"mode":"on","reading":"\ud800\u0041"}`, 400},
		{"REQ-05", `{"mode":"on","reading":"\ud83d\ude00"}`, 0},
		{"REQ-06", `{"mode":"on","reading":"\\ud800"}`, 0},
		{"REQ-07", `{`, 400},
		{"REQ-08", `[]`, 400},
		{"REQ-09", `null`, 400},
		{"REQ-10", `{"mode":"on","reading":"ア"} {}`, 400},
		{"REQ-11", `{"mode":"on","reading":"ア","reading":"イ"}`, 400},
		{"REQ-12", `{"mode":"on","reading":null}`, 422},
		{"REQ-13", `{"mode":"on","reading":5}`, 422},
		{"REQ-14", `{"mode":"on"}`, 422},
		{"REQ-15", `{"mode":"on","reading":"ア"}`, 0},
		{"REQ-16", `{"mode":"on","reading":"ア","seed":1}`, 422},
	} {
		t.Run(c.id, func(t *testing.T) {
			if _, status, e := Decode([]byte(c.body)); status != c.status {
				t.Errorf("%q status=%d err=%v want=%d", c.body, status, e, c.status)
			}
		})
	}
}

// Test ID: REQ-17
func FuzzDecode(f *testing.F) {
	f.Add([]byte(`{"mode":"on","reading":"ｷｬ"}`))
	f.Add([]byte(`{"reading":"\ud800"}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > 1024 {
			return
		}
		in, _, e := Decode(b)
		if e != nil {
			return
		}
		r, issue := Validate(in)
		if issue == nil {
			if r.Reading != Normalize(r.Reading) || len(r.Morae) == 0 || strings.Join(r.Morae, "") != r.Reading {
				t.Fatalf("bad normalization %+v", r)
			}
		}
	})
}
