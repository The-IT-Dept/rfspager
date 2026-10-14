package importer

import (
	"archive/zip"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// acmaLocalData is where a developer unzipped the real register; the test
// that reads it skips when it is absent (CI never downloads 70 MB).
const acmaLocalData = "/tmp/acma-rrl/data"

func TestParseACMARealRegister(t *testing.T) {
	if _, err := os.Stat(filepath.Join(acmaLocalData, "device_details.csv")); err != nil {
		t.Skipf("real ACMA register not unpacked at %s", acmaLocalData)
	}
	txs, err := ParseACMA(acmaLocalData)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	seen := map[int64]bool{}
	for _, tx := range txs {
		counts[tx.Channel+"/"+tx.Licensee]++
		if seen[tx.SddID] {
			t.Errorf("duplicate sdd_id %d", tx.SddID)
		}
		seen[tx.SddID] = true
		if tx.Lat > -28 || tx.Lat < -38 || tx.Lng < 140 || tx.Lng > 154 {
			t.Errorf("transmitter %d %q outside NSW: %v,%v", tx.SddID, tx.Name, tx.Lat, tx.Lng)
		}
	}
	t.Logf("paging transmitters: %v", counts)
	rfs := counts["RFS/NSW RURAL FIRE SERVICE"]
	nswtaRFS := counts["RFS/NEW SOUTH WALES GOVERNMENT TELECOMMUNICATIONS AUTHORITY"]
	frnsw := counts["FRNSW/NEW SOUTH WALES GOVERNMENT TELECOMMUNICATIONS AUTHORITY"] + counts["FRNSW/NSW RURAL FIRE SERVICE"]
	// The register changes weekly; allow drift around the 2026-10-13 counts
	// (416 RFS + 36 NSWTA on 148.5875, 34 on 148.9625; two NSWTA
	// overlay units are area-licensed without a site and are skipped).
	near := func(got, want int) bool { return math.Abs(float64(got-want)) <= float64(want)/10+2 }
	if !near(rfs, 416) || !near(nswtaRFS, 34) || !near(frnsw, 34) {
		t.Errorf("counts RFS=%d NSWTA-on-RFS=%d FRNSW=%d, want about 416/34/34", rfs, nswtaRFS, frnsw)
	}
}

// A tiny register in a zip exercises the zip path, header lookup by name
// (columns deliberately out of the real order), the licence/licensee/status
// filters and EIRP units.
func TestParseACMAZip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rrl.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	files := map[string]string{
		"client.csv": "LICENCEE,CLIENT_NO\n" +
			"NSW RURAL FIRE SERVICE,1\n" +
			"NEW SOUTH WALES GOVERNMENT TELECOMMUNICATIONS AUTHORITY,2\n" +
			"Someone Else,3\n",
		"licence.csv": "LICENCE_NO,CLIENT_NO,STATUS_TEXT\n" +
			"L1,1,Granted\nL2,2,Granted\nL3,3,Granted\nL4,1,Surrendered\n",
		"device_details.csv": "SITE_ID,SDD_ID,LICENCE_NO,FREQUENCY,DEVICE_TYPE,HEIGHT,EIRP,EIRP_UNIT,AUTHORISATION_DATE\n" +
			"S1,10,L1,148587500,T,15,331,W,1998-09-16\n" + // RFS
			"S1,11,L1,148587500,R,15,331,W,1998-09-16\n" + // receiver
			"S2,12,L2,148962500.0,T,,20,dBW,\n" + // FRNSW, 100 W
			"S2,13,L3,148587500,T,15,331,W,\n" + // other licensee
			"S1,14,L4,148587500,T,15,331,W,\n" + // not granted
			"S1,15,L1,148700000,T,15,331,W,\n" + // other frequency
			"S9,16,L1,148587500,T,15,\"331\"x,W,\n", // no such site
		"site.csv": "SITE_ID,LATITUDE,LONGITUDE,NAME,ELEVATION\n" +
			"S1,-33.2,151.5,\"Mt \"\"Sugarloaf\"\" tower\",410\n" +
			"S2,-32.9,151.7,Caf\xe9 Hill,\n",
	}
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()

	txs, err := ParseACMA(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(txs) != 2 {
		t.Fatalf("got %d transmitters, want 2: %+v", len(txs), txs)
	}
	rfs, frnsw := txs[0], txs[1]
	if rfs.SddID != 10 || rfs.Channel != "RFS" || rfs.Name != `Mt "Sugarloaf" tower` || *rfs.EIRPW != 331 || *rfs.HeightM != 15 || *rfs.ElevationM != 410 || rfs.AuthorisedOn == nil {
		t.Errorf("RFS transmitter = %+v", rfs)
	}
	if frnsw.SddID != 12 || frnsw.Channel != "FRNSW" || frnsw.FreqHz != acmaFreqFRNSW || math.Abs(*frnsw.EIRPW-100) > 1e-9 || frnsw.HeightM != nil || frnsw.ElevationM != nil {
		t.Errorf("FRNSW transmitter = %+v", frnsw)
	}
	if frnsw.Name != "Café Hill" {
		t.Errorf("Latin-1 site name = %q, want Café Hill", frnsw.Name)
	}
}

func TestEIRPWatts(t *testing.T) {
	for _, tc := range []struct {
		value, unit string
		want        float64 // 0 = nil
	}{
		{"331", "W", 331},
		{"500", "mW", 0.5},
		{"2", "kW", 2000},
		{"20", "dBW", 100},
		{"50", "dBm", 100},
		{"", "W", 0},
		{"0", "W", 0},
		{"10", "furlongs", 0},
	} {
		got := eirpWatts(tc.value, tc.unit)
		if tc.want == 0 {
			if got != nil {
				t.Errorf("eirpWatts(%q,%q) = %v, want nil", tc.value, tc.unit, *got)
			}
			continue
		}
		if got == nil || math.Abs(*got-tc.want) > 1e-9 {
			t.Errorf("eirpWatts(%q,%q) = %v, want %v", tc.value, tc.unit, got, tc.want)
		}
	}
}
