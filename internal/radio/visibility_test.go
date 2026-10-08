package radio

import (
	"strings"
	"testing"
)

func TestSetPublicAgencies(t *testing.T) {
	defer func(a []string) { _ = SetPublicAgencies(a) }(publicAgencies)

	for _, bad := range [][]string{{}, {" "}, {"RFS'; DROP TABLE x;--"}, {"RFS", "NSW AMB"}} {
		if err := SetPublicAgencies(bad); err == nil {
			t.Errorf("SetPublicAgencies(%q) accepted", bad)
		}
	}
	if err := SetPublicAgencies([]string{" RFS", "SES ", ""}); err != nil {
		t.Fatal(err)
	}
	q := vis(`SELECT 1 FROM t tg WHERE NOT COALESCE(tg.hidden, false)`)
	if !strings.Contains(q, "NOT IN ('RFS', 'SES')") || strings.Contains(q, "COALESCE(tg.hidden") {
		t.Errorf("vis = %s", q)
	}
}
