package blobstore

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestValidKey(t *testing.T) {
	for _, k := range []string{"a.wav", "radio/2026/10/08/123.wav"} {
		if err := ValidKey(k); err != nil {
			t.Errorf("ValidKey(%q) = %v, want ok", k, err)
		}
	}
	for _, k := range []string{"", "/abs", "../x", "a/../../x", "a//b", "a/./b", "a/", "a\\b", "a\x00b"} {
		if err := ValidKey(k); err == nil {
			t.Errorf("ValidKey(%q) accepted", k)
		}
	}
}

func TestLocalPutOpenSeek(t *testing.T) {
	ctx := context.Background()
	l, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("0123456789")
	if _, err := l.Put(ctx, "radio/x/1.wav", "audio/wav", bytes.NewReader(data), int64(len(data))); err != nil {
		t.Fatal(err)
	}
	obj, err := l.Open(ctx, "radio/x/1.wav")
	if err != nil {
		t.Fatal(err)
	}
	defer obj.Close()
	if obj.Size != 10 {
		t.Errorf("size = %d", obj.Size)
	}
	if _, err := obj.Seek(4, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	rest, _ := io.ReadAll(obj)
	if string(rest) != "456789" {
		t.Errorf("after seek read %q", rest)
	}
	if _, err := l.Open(ctx, "radio/x/missing.wav"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing object: %v, want ErrNotFound", err)
	}
	if _, err := l.Put(ctx, "../escape", "", bytes.NewReader(nil), 0); err == nil {
		t.Error("Put accepted a key outside the directory")
	}
	if _, err := l.Put(ctx, "short", "", bytes.NewReader(data), 99); err == nil {
		t.Error("Put accepted a size mismatch")
	}
}

// The example from the AWS S3 SigV4 documentation ("Example: GET Object"):
// https://docs.aws.amazon.com/AmazonS3/latest/API/sig-v4-header-based-auth.html
func TestSigV4MatchesAWSExample(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://examplebucket.s3.amazonaws.com/test.txt", nil)
	req.Header.Set("Range", "bytes=0-9")
	s := sigV4{
		accessKeyID:     "AKIAIOSFODNN7EXAMPLE",
		secretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		region:          "us-east-1",
		service:         "s3",
	}
	s.sign(req, emptyPayloadHash, time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC))
	want := "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request, " +
		"SignedHeaders=host;range;x-amz-content-sha256;x-amz-date, " +
		"Signature=f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41"
	if got := req.Header.Get("Authorization"); got != want {
		t.Errorf("Authorization =\n  %s\nwant\n  %s", got, want)
	}
}

func TestAWSURIEscape(t *testing.T) {
	if got := awsURIEscapePath("/b/radio/a b+c(1).wav"); got != "/b/radio/a%20b%2Bc%281%29.wav" {
		t.Errorf("escape = %q", got)
	}
}

// fakeS3 is just enough of S3 for the client: PUT, GET and ranged GET under
// path-style /bucket/key, requiring a SigV4 Authorization header.
type fakeS3 struct {
	mu      sync.Mutex
	objects map[string][]byte
	types   map[string]string
	gets    []string // Range header of each GET
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=key/") ||
		r.Header.Get("X-Amz-Date") == "" || r.Header.Get("X-Amz-Content-Sha256") == "" {
		http.Error(w, "<Error><Code>AccessDenied</Code></Error>", http.StatusForbidden)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.Method {
	case http.MethodPut:
		body, _ := io.ReadAll(r.Body)
		f.objects[r.URL.Path] = body
		f.types[r.URL.Path] = r.Header.Get("Content-Type")
	case http.MethodGet:
		f.gets = append(f.gets, r.Header.Get("Range"))
		data, ok := f.objects[r.URL.Path]
		if !ok {
			http.Error(w, "<Error><Code>NoSuchKey</Code></Error>", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", f.types[r.URL.Path])
		if rg := r.Header.Get("Range"); rg != "" {
			from, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(rg, "bytes="), "-"))
			w.Header().Set("Content-Length", strconv.Itoa(len(data)-from))
			w.WriteHeader(http.StatusPartialContent)
			w.Write(data[from:])
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.Write(data)
	}
}

func TestS3PutOpenRangedSeek(t *testing.T) {
	ctx := context.Background()
	fake := &fakeS3{objects: map[string][]byte{}, types: map[string]string{}}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	s, err := NewS3(S3Config{Endpoint: srv.URL, Bucket: "audio", AccessKeyID: "key", SecretAccessKey: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("RIFF....WAVEdata0123456789")
	if _, err := s.Put(ctx, "radio/2026/10/08/1.wav", "audio/wav", bytes.NewReader(data), int64(len(data))); err != nil {
		t.Fatal(err)
	}
	if got := fake.objects["/audio/radio/2026/10/08/1.wav"]; !bytes.Equal(got, data) {
		t.Fatalf("stored %q", got)
	}

	obj, err := s.Open(ctx, "radio/2026/10/08/1.wav")
	if err != nil {
		t.Fatal(err)
	}
	defer obj.Close()
	if obj.Size != int64(len(data)) || obj.ContentType != "audio/wav" {
		t.Errorf("object size %d type %q", obj.Size, obj.ContentType)
	}
	// What http.ServeContent does for "Range: bytes=16-": find the size,
	// rewind, then seek to the range.
	if end, _ := obj.Seek(0, io.SeekEnd); end != int64(len(data)) {
		t.Errorf("SeekEnd = %d", end)
	}
	obj.Seek(0, io.SeekStart)
	obj.Seek(16, io.SeekStart)
	got, err := io.ReadAll(obj)
	if err != nil || string(got) != "0123456789" {
		t.Fatalf("ranged read = %q, %v", got, err)
	}
	if len(fake.gets) != 2 || fake.gets[0] != "" || fake.gets[1] != "bytes=16-" {
		t.Errorf("GETs = %q, want the open then one ranged GET", fake.gets)
	}

	if _, err := s.Open(ctx, "radio/missing.wav"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing: %v, want ErrNotFound", err)
	}
	bad, _ := NewS3(S3Config{Endpoint: srv.URL, Bucket: "audio", AccessKeyID: "wrong", SecretAccessKey: "secret"})
	if _, err := bad.Put(ctx, "x.wav", "audio/wav", bytes.NewReader(data), int64(len(data))); err == nil || !strings.Contains(err.Error(), "AccessDenied") {
		t.Errorf("rejected put: %v", err)
	}
}
