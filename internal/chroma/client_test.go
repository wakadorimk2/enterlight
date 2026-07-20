package chroma

import "testing"

func TestRGBToColorRef(t *testing.T) {
	got := rgbToColorRef(0x123456)
	if got != 0x563412 {
		t.Fatalf("got %#x, want %#x", got, uint32(0x563412))
	}
}

func TestCustomKeyEffectOnlyLightsEnter(t *testing.T) {
	effect := customKeyEffect(0x3B82F6)
	if effect.Effect != "CHROMA_CUSTOM_KEY" {
		t.Fatalf("unexpected effect: %s", effect.Effect)
	}
	nonzero := 0
	for r := range effect.Param.Key {
		for c, v := range effect.Param.Key[r] {
			if v != 0 {
				nonzero++
				if r != enterRow || c != enterCol {
					t.Fatalf("unexpected lit key at %d,%d", r, c)
				}
			}
		}
	}
	if nonzero != 1 {
		t.Fatalf("got %d lit keys, want 1", nonzero)
	}
}
