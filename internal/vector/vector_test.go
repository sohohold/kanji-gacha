package vector

import (
	"math"
	"testing"
)

func TestSimilarity(t *testing.T) {
	a := Reading("セイ")
	if math.Abs(Cos(a, a)-1) > 1e-5 {
		t.Fatal("not normalized")
	}
	if Cos(a, Reading("ゼイ")) <= Cos(a, Reading("ハク")) {
		t.Fatal("phonetic proximity lost")
	}
}
