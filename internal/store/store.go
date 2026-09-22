package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

var ErrNotFound = errors.New("not found")
var ErrExists = errors.New("already exists")

type Store interface {
	Get(context.Context, string) ([]byte, error)
	PutIfAbsent(context.Context, string, []byte, string) error
}

var keyPattern = regexp.MustCompile(`^(glyphs/[0-9a-f]{32}\.json|og/[0-9a-f]{32}\.png)$`)

func checkKey(k string) error {
	if !keyPattern.MatchString(k) {
		return fmt.Errorf("invalid object key")
	}
	return nil
}

type Local struct{ Dir string }

func (l Local) Get(ctx context.Context, k string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := checkKey(k); err != nil {
		return nil, err
	}
	b, e := os.ReadFile(filepath.Join(l.Dir, k))
	if errors.Is(e, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	return b, e
}

// Link publishes a fully written file atomically, without overwriting an existing result.
func (l Local) PutIfAbsent(ctx context.Context, k string, b []byte, _ string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := checkKey(k); err != nil {
		return err
	}
	p := filepath.Join(l.Dir, k)
	if e := os.MkdirAll(filepath.Dir(p), 0755); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(p), ".pending-")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if _, e = f.Write(b); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	if e = ctx.Err(); e != nil {
		return e
	}
	e = os.Link(f.Name(), p)
	if errors.Is(e, os.ErrExist) {
		return ErrExists
	}
	return e
}
