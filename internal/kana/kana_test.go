package kana

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

// Test IDs: the id of each case in shared/kana-cases.json (IN-NN).
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
		Position                *int
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
			} else if issue == nil || issue.Code != c.Code || issue.Field != c.Field || !reflect.DeepEqual(issue.Position, c.Position) || issue.Normalized != c.Normalized {
				t.Fatalf("got %+v, want %s/%s at %v in %q", issue, c.Field, c.Code, c.Position, c.Normalized)
			}
		})
	}
}

// Test IDs: REQ-01 to REQ-16, REQ-18 and REQ-19, one per case.
func TestJSONBoundary(t *testing.T) {
	for _, c := range []struct {
		id, body    string
		status      int
		field, code string
	}{
		{"REQ-01", "{\"reading\":\"\xff\"}", 400, "", ""},
		{"REQ-02", `{"mode":"on","reading":"\ud800"}`, 400, "", ""},
		{"REQ-03", `{"mode":"on","reading":"\udc00"}`, 400, "", ""},
		{"REQ-04", `{"mode":"on","reading":"\ud800\u0041"}`, 400, "", ""},
		{"REQ-05", `{"mode":"on","reading":"\ud83d\ude00"}`, 0, "", ""},
		{"REQ-06", `{"mode":"on","reading":"\\ud800"}`, 0, "", ""},
		{"REQ-07", `{`, 400, "", ""},
		{"REQ-08", `[]`, 400, "", ""},
		{"REQ-09", `null`, 400, "", ""},
		{"REQ-10", `{"mode":"on","reading":"ア"} {}`, 400, "", ""},
		{"REQ-11", `{"mode":"on","reading":"ア","reading":"イ"}`, 400, "", ""},
		{"REQ-12", `{"mode":"on","reading":null}`, 422, "reading", "type"},
		{"REQ-13", `{"mode":"on","reading":5}`, 422, "reading", "type"},
		{"REQ-14", `{"mode":"on"}`, 422, "reading", "required"},
		{"REQ-15", `{"mode":"on","reading":"ア"}`, 0, "", ""},
		{"REQ-16", `{"mode":"on","reading":"ア","seed":1}`, 422, "seed", "unknown"},
		{"REQ-18", `{"mode":"kun","reading":"ア","okurigana":null}`, 422, "okurigana", "type"},
		{"REQ-19", `{"mode":"kun","reading":"ア","okurigana":5}`, 422, "okurigana", "type"},
	} {
		t.Run(c.id, func(t *testing.T) {
			_, status, e := Decode([]byte(c.body))
			if status != c.status {
				t.Fatalf("%q status=%d err=%v want=%d", c.body, status, e, c.status)
			}
			if c.status != 422 {
				return
			}
			// Type and missing-value errors name the field and code, never a position.
			var issue *Issue
			if !errors.As(e, &issue) || issue.Field != c.field || issue.Code != c.code || issue.Position != nil || issue.Normalized != "" {
				t.Fatalf("%q: got %+v, want %s/%s without a position", c.body, e, c.field, c.code)
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
