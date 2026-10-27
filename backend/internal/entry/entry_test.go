package entry

import (
	"testing"
	"time"
)

func TestCursorRoundTrip(t *testing.T) {
	want := time.Date(2026, 10, 25, 13, 42, 1, 123456000, time.UTC)
	raw := encodeCursor(want, 481)
	got, id, err := decodeCursor(raw)
	if err != nil || id != 481 || !got.Equal(want) {
		t.Fatalf("decoded cursor = %v, %d, %v", got, id, err)
	}
}
