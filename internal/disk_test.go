package internal

import "testing"

func TestParseSizeThreshold(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"100GB", 100 * 1024 * 1024 * 1024},
		{"1TB", 1024 * 1024 * 1024 * 1024},
		{"500MB", 500 * 1024 * 1024},
		{"10KB", 10 * 1024},
	}
	for _, tc := range tests {
		got, err := parseSizeThreshold(tc.in)
		if err != nil {
			t.Fatalf("%q: %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("%q: got %d want %d", tc.in, got, tc.want)
		}
	}
}

func TestDiskFreePercentRoot(t *testing.T) {
	pct := diskFreePercent("/")
	if pct < 0 || pct > 100 {
		t.Fatalf("unexpected free percent: %v", pct)
	}
}

func TestDiskGateAllowsAction(t *testing.T) {
	m := NewModule(Config{})
	m.diskActMaxFreePercent = 0
	if !m.diskGateAllowsAction("/") {
		t.Fatal("disabled gate should always allow")
	}
	m.diskActMaxFreePercent = 100
	if !m.diskGateAllowsAction("/") {
		t.Fatal("100% threshold should allow action on any disk state")
	}
}

func TestFreeUpTargetMet(t *testing.T) {
	m := NewModule(Config{})
	if m.freeUpTargetMet("/", 0) {
		t.Fatal("zero target should never be met")
	}
}
