package admin

import (
	"sort"
	"testing"
)

func TestCompareVersionsOrdersNumerically(t *testing.T) {
	got := []string{"4.1.0", "5.9.1", "unknown", "5.10.0", "3.5.4", "5.3.8", "5.3"}
	sort.Slice(got, func(i, j int) bool { return compareVersions(got[i], got[j]) > 0 })
	want := []string{"5.10.0", "5.9.1", "5.3.8", "5.3", "4.1.0", "3.5.4", "unknown"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}
