package search

import "testing"

func TestBooleanQuery(t *testing.T) {
	tests := []struct{ in, want string }{
		{"鲸鱼 海豚", `+"鲸鱼" +"海豚"`},
		{"  golang  ", `+"golang"`},
		{"山", "+山*"},
		{`鲸鱼 -"(*`, `+"鲸鱼"`},
		{`a+b`, "+a* +b*"},
		{"   ", ""},
		{"1 2 3 4 5 6 7 8 9 10", "+1* +2* +3* +4* +5* +6* +7* +8*"},
	}
	for _, tt := range tests {
		if got := booleanQuery(tt.in); got != tt.want {
			t.Errorf("booleanQuery(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
