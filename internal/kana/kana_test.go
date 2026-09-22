package kana

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestSharedCases(t *testing.T) {
	b, e := os.ReadFile("../../shared/kana-cases.json")
	if e != nil {
		t.Fatal(e)
	}
	var cases []struct {
		Name                    string
		Input                   Input
		Valid                   bool
		Normalized, Code, Field string
		Morae                   []string
	}
	if e = json.Unmarshal(b, &cases); e != nil {
		t.Fatal(e)
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
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
func TestJSONBoundary(t *testing.T) {
	for _, c := range []struct {
		body   string
		status int
	}{{`{"mode":"on","reading":"ア"}`, 0}, {`{"mode":"on","reading":"ア","reading":"イ"}`, 400}, {`{"mode":"on","reading":"\ud800"}`, 400}, {`{"mode":"on","reading":"\udc00"}`, 400}, {`{"mode":"on","reading":"\ud800\u0041"}`, 400}, {`{"mode":"on","reading":"\ud83d\ude00"}`, 0}, {`{"mode":"on","reading":"\\ud800"}`, 0}, {`{"mode":"on","reading":null}`, 422}, {`{"mode":"on","reading":5}`, 422}, {`{"mode":"on"}`, 422}, {`{"mode":"on","reading":"ア","seed":1}`, 422}, {`{"mode":"on","reading":"ア"} {}`, 400}, {"{\"reading\":\"\xff\"}", 400}, {`[]`, 400}, {`{`, 400}, {`null`, 400}} {
		_, status, e := Decode([]byte(c.body))
		if status != c.status {
			t.Errorf("%q status=%d err=%v want=%d", c.body, status, e, c.status)
		}
	}
}
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
