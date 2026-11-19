package notify

import "testing"

func TestPageContent(t *testing.T) {
	cases := []struct {
		name                string
		data                string
		title, body, collap string
		correction          bool
	}{
		{"a page push", `{"id": 2501227, "title": "Fire and Rescue NSW | 102 Example", "body": "FRINC TYPE: q^C TURNOUT: 102", "message": "FRINC TYPE: q^C TURNOUT: 102"}`,
			"Fire and Rescue NSW | 102 Example", "FRINC TYPE: q^C TURNOUT: 102", "msg-2501227", false},
		{"its correction", `{"id": 2501227, "title": "Fire and Rescue NSW | 102 Example", "body": "FRINC TYPE: q^C TURNOUT: 102", "correction": true, "correction_body": "FRINC TYPE: MVA TURNOUT: 102 INC: 100002-17112026"}`,
			"Fire and Rescue NSW | 102 Example (corrected)", "FRINC TYPE: MVA TURNOUT: 102 INC: 100002-17112026", "msg-2501227", true},
		{"no title", `{"id": 7, "body": "x"}`, "RFS Pager", "x", "msg-7", false},
		{"no message id", `{"title": "t", "body": "x"}`, "t", "x", "", false},
	}
	for _, c := range cases {
		title, body, opts, err := pageContent([]byte(c.data))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if title != c.title || body != c.body || opts.CollapseID != c.collap || opts.Correction != c.correction {
			t.Errorf("%s: got %q / %q / %+v", c.name, title, body, opts)
		}
	}
	if _, _, _, err := pageContent([]byte("not json")); err == nil {
		t.Error("a payload that isn't JSON is an error")
	}
}
