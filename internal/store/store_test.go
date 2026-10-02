package store

import (
	"context"
	"errors"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
)

// Test ID: STO-01
func TestAtomicPublication(t *testing.T) {
	s := Local{Dir: t.TempDir()}
	ctx := context.Background()
	k := "glyphs/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.json"
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e := s.PutIfAbsent(ctx, k, []byte("complete"), "application/json")
			if e == nil {
				wins.Add(1)
			} else if !errors.Is(e, ErrExists) {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatal(wins.Load())
	}
	b, e := s.Get(ctx, k)
	if e != nil || string(b) != "complete" {
		t.Fatalf("%s %v", b, e)
	}
	if _, e = s.Get(ctx, "../../etc/passwd"); e == nil {
		t.Fatal("traversal allowed")
	}
}

// s3Replies serves PUTs from a scripted list of statuses and error codes, the
// way S3 answers conditional writes, and counts them.
func s3Replies(t *testing.T, replies ...[2]string) (*S3, *atomic.Int32) {
	t.Helper()
	var puts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		if r.Method != http.MethodPut || r.Header.Get("If-None-Match") != "*" {
			t.Errorf("%s without If-None-Match: *", r.Method)
		}
		i := int(puts.Add(1)) - 1
		if i >= len(replies) {
			t.Errorf("unexpected PUT %d", i+1)
			w.WriteHeader(500)
			return
		}
		status, _ := strconv.Atoi(replies[i][0])
		if status == 200 {
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(status)
		fmt.Fprintf(w, "<Error><Code>%s</Code><Message>scripted</Message></Error>", replies[i][1])
	}))
	t.Cleanup(srv.Close)
	client := s3.New(s3.Options{Region: "ap-northeast-1", BaseEndpoint: aws.String(srv.URL), UsePathStyle: true,
		Credentials: credentials.NewStaticCredentialsProvider("id", "secret", "")})
	return &S3{Client: client, Bucket: "b"}, &puts
}

// Test ID: STO-02
func TestS3RetriesWriteConflict(t *testing.T) {
	k := "og/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.png"
	for _, c := range []struct {
		name    string
		replies [][2]string
		want    error
	}{
		// A competing write was still landing; once it has, the key exists.
		{"then exists", [][2]string{{"409", "ConditionalRequestConflict"}, {"412", "PreconditionFailed"}}, ErrExists},
		// The competing write never landed, so this one wins.
		{"then wins", [][2]string{{"409", "ConditionalRequestConflict"}, {"409", "ConditionalRequestConflict"}, {"200", ""}}, nil},
		{"exists", [][2]string{{"412", "PreconditionFailed"}}, ErrExists},
		// Still conflicting after the retries: left to the caller's reread.
		{"gives up", [][2]string{{"409", "ConditionalRequestConflict"}, {"409", "ConditionalRequestConflict"}, {"409", "ConditionalRequestConflict"}, {"409", "ConditionalRequestConflict"}, {"409", "ConditionalRequestConflict"}}, ErrExists},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, puts := s3Replies(t, c.replies...)
			e := s.PutIfAbsent(context.Background(), k, []byte("png"), "image/png")
			if !errors.Is(e, c.want) || c.want == nil && e != nil {
				t.Fatalf("got %v, want %v", e, c.want)
			}
			if int(puts.Load()) != len(c.replies) {
				t.Fatalf("%d PUTs, want %d", puts.Load(), len(c.replies))
			}
		})
	}
}
