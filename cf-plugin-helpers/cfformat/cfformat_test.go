package cfformat

import "testing"

func TestByteSize(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0B"},
		{512, "512B"},
		{1024, "1.0K"},
		{1536, "1.5K"},
		{1048576, "1.0M"},
		{1073741824, "1.0G"},
		{1099511627776, "1.0T"},
	}
	for _, tt := range tests {
		got := ByteSize(tt.input)
		if got != tt.want {
			t.Errorf("ByteSize(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
