package dict

import (
	_ "embed"
	"sync"
)

//go:embed dictionary.json.gz
var bundled []byte
var Load = sync.OnceValues(func() (Dictionary, error) { return Decode(bundled) })
