package handler

import "testing"

func TestIncidentKeyCollapsesBrigadeCopies(t *testing.T) {
	central := "13 October 2026 22:38:32 LHCENTR1 - 26-131320 - Electrical appliance fire - STRUCTURE FIRE - OPP 21 LOWER ST,CESSNOCK,CESSNOCK (NSW),2325 - [151.35,-32.83]"
	neath := "13 October 2026 22:42:13 LHNEATH7 - 26-131320 - Electrical appliance fire - STRUCTURE FIRE - OPP 21 LOWER ST,CESSNOCK,CESSNOCK (NSW),2325 - [151.35,-32.83]"
	pokolbin := "13 October 2026 22:47:41 LHPOKOL1 - 26-131320 - Electrical appliance fire - STRUCTURE FIRE - OPP 21 LOWER ST,CESSNOCK,CESSNOCK (NSW),2325 - [151.35,-32.83]"
	if IncidentKey(central) != IncidentKey(neath) || IncidentKey(neath) != IncidentKey(pokolbin) {
		t.Fatalf("same incident to three brigades should share a key:\n%s\n%s\n%s", IncidentKey(central), IncidentKey(neath), IncidentKey(pokolbin))
	}
	stop := "13 October 2026 23:10:02 26-131320 LHCENTR1 Stop Message // CESSNOCK - NO NEED TO ATTEND THANK YOU"
	if IncidentKey(stop) == IncidentKey(central) {
		t.Fatal("the stop message is a different message for the same incident")
	}
	stop2 := "13 October 2026 23:10:04 26-131320 LHNEATH7 Stop Message // CESSNOCK - NO NEED TO ATTEND THANK YOU"
	if IncidentKey(stop) != IncidentKey(stop2) {
		t.Fatalf("the same stop message to two brigades should share a key: %s vs %s", IncidentKey(stop), IncidentKey(stop2))
	}
	other := "13 October 2026 22:50:00 LHCENTR1 - 26-131321 - Grass fire - FIRECALL - 5 MAIN RD,ABERDARE,CESSNOCK (NSW),2325"
	if IncidentKey(other) == IncidentKey(central) {
		t.Fatal("a different incident must not share the key")
	}
}

func TestIncidentKeyFRNSWByIncidentNumber(t *testing.T) {
	a := "FRINC TYPE: AFA TURNOUT: 72 INC: 203357-13102026"
	b := "FRINC TYPE: AFA TURNOUT: 79 INC: 203357-13102026"
	c := "FRINC TYPE: AFA TURNOUT: 72 INC: 203358-13102026"
	if IncidentKey(a) != IncidentKey(b) {
		t.Fatal("two stations on one FRNSW incident should share a key")
	}
	if IncidentKey(a) == IncidentKey(c) {
		t.Fatal("different FRNSW incidents must differ")
	}
}

func TestIncidentKeyBroadcastsWithoutIncident(t *testing.T) {
	a := "13 October 2026 16:00:09 Training tonight at 1900 hrs. Please update ACTIV with your availability"
	b := "13 October 2026 16:00:42 Training tonight at 1900 hrs. Please update ACTIV with your availability"
	if IncidentKey(a) != IncidentKey(b) {
		t.Fatal("identical broadcasts with different page times should share a key")
	}
	if IncidentKey(a) == IncidentKey("13 October 2026 16:00:09 Training tomorrow at 1900 hrs.") {
		t.Fatal("different broadcasts must differ")
	}
	if IncidentKey("") != "" {
		t.Fatal("empty message has no key")
	}
}
