package install

import (
	"net/http/httptest"
	"os/exec"
	"testing"
)

func TestCommandForOrdinaryValuesIsUnquoted(t *testing.T) {
	ConfiguredBaseURL = "https://rfspager.app"
	t.Cleanup(func() { ConfiguredBaseURL = "" })
	got := CommandFor(httptest.NewRequest("GET", "/", nil), "VP2R80BPXMUF1GXULUP59TO3")
	want := "curl -fsSL https://rfspager.app/api/install | sudo bash -s -- VP2R80BPXMUF1GXULUP59TO3"
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestShellQuoteNeutralisesHostileValues(t *testing.T) {
	for _, v := range []string{
		`https://evil"; id; echo "`,
		`it's`,
		`-rf`,
		`a b`,
		`$(id)`,
		"`id`",
		"",
	} {
		// The quoted value must come back from the shell as exactly one argument, verbatim.
		cmd := exec.Command("sh", "-c", "set -- "+shellQuote(v)+`; printf '%s|%s' "$#" "$1"`)
		got, err := cmd.Output()
		if err != nil {
			t.Fatalf("%q: %v", v, err)
		}
		if want := "1|" + v; string(got) != want {
			t.Fatalf("%q round-tripped as %q", v, got)
		}
	}
}
