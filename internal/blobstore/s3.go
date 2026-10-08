package blobstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// S3Config addresses an S3-compatible bucket. For Cloudflare R2 the endpoint
// is https://<account-id>.r2.cloudflarestorage.com and the region "auto".
type S3Config struct {
	Endpoint        string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
	Region          string // default "auto"
	// HTTPClient defaults to one with a 60s timeout.
	HTTPClient *http.Client
}

// S3 is a minimal S3 client: PUT and GET (ranged) of single objects with
// path-style addressing and SigV4, which is all the audio store needs. It
// avoids pulling the AWS SDK (a dozen modules) into the server for three
// requests.
type S3 struct {
	endpoint *url.URL
	bucket   string
	signer   sigV4
	http     *http.Client
}

func NewS3(cfg S3Config) (*S3, error) {
	if cfg.Endpoint == "" || cfg.Bucket == "" || cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" {
		return nil, errors.New("blobstore: S3 needs endpoint, bucket, access key id and secret access key")
	}
	u, err := url.Parse(strings.TrimRight(cfg.Endpoint, "/"))
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.RawQuery != "" {
		return nil, fmt.Errorf("blobstore: invalid S3 endpoint %q", cfg.Endpoint)
	}
	if strings.ContainsAny(cfg.Bucket, "/?#") {
		return nil, fmt.Errorf("blobstore: invalid bucket %q", cfg.Bucket)
	}
	region := cfg.Region
	if region == "" {
		region = "auto"
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	return &S3{
		endpoint: u,
		bucket:   cfg.Bucket,
		signer:   sigV4{accessKeyID: cfg.AccessKeyID, secretAccessKey: cfg.SecretAccessKey, region: region, service: "s3"},
		http:     client,
	}, nil
}

// objectURL is endpoint/bucket/key with the path escaped the way SigV4's
// canonical URI expects (see awsURIEscapePath).
func (s *S3) objectURL(key string) *url.URL {
	u := *s.endpoint
	raw := strings.TrimRight(u.Path, "/") + "/" + s.bucket + "/" + key
	u.Path = raw
	u.RawPath = awsURIEscapePath(raw)
	return &u
}

func (s *S3) Put(ctx context.Context, key, contentType string, r io.Reader, size int64) (string, error) {
	if err := ValidKey(key); err != nil {
		return "", err
	}
	if size < 0 {
		return "", errors.New("blobstore: S3 put needs the size up front")
	}
	u := s.objectURL(key)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u.String(), io.NopCloser(r))
	if err != nil {
		return "", err
	}
	req.URL = u // keep the SigV4-escaped RawPath that re-parsing would drop
	req.ContentLength = size
	if size == 0 {
		req.Body = http.NoBody
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	s.signer.sign(req, unsignedPayload, time.Now())
	resp, err := s.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", s3Error("put", resp)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return u.String(), nil
}

// Open GETs the whole object and reads it as a stream. Seeking elsewhere
// drops that stream and the next Read issues a ranged GET from the new
// offset, so http.ServeContent's Range handling costs one extra request
// rather than downloading the object to skip into it.
func (s *S3) Open(ctx context.Context, key string) (*Object, error) {
	if err := ValidKey(key); err != nil {
		return nil, err
	}
	o := &s3Object{s: s, ctx: ctx, key: key}
	resp, err := o.get(0)
	if err != nil {
		return nil, err
	}
	size := resp.ContentLength
	if size < 0 {
		resp.Body.Close()
		return nil, errors.New("blobstore: S3 response without Content-Length")
	}
	o.size, o.body = size, resp.Body
	modTime, _ := http.ParseTime(resp.Header.Get("Last-Modified"))
	return &Object{
		ReadSeekCloser: o,
		Size:           size,
		ContentType:    resp.Header.Get("Content-Type"),
		ModTime:        modTime,
	}, nil
}

type s3Object struct {
	s    *S3
	ctx  context.Context
	key  string
	size int64
	pos  int64
	body io.ReadCloser // positioned at pos, or nil
}

// get fetches the object from offset (0 = all of it).
func (o *s3Object) get(offset int64) (*http.Response, error) {
	u := o.s.objectURL(o.key)
	req, err := http.NewRequestWithContext(o.ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.URL = u
	if offset > 0 {
		req.Header.Set("Range", "bytes="+strconv.FormatInt(offset, 10)+"-")
	}
	o.s.signer.sign(req, emptyPayloadHash, time.Now())
	resp, err := o.s.http.Do(req)
	if err != nil {
		return nil, err
	}
	switch {
	case resp.StatusCode == http.StatusNotFound:
		resp.Body.Close()
		return nil, ErrNotFound
	case offset > 0 && resp.StatusCode != http.StatusPartialContent,
		offset == 0 && resp.StatusCode != http.StatusOK:
		defer resp.Body.Close()
		return nil, s3Error("get", resp)
	}
	return resp, nil
}

func (o *s3Object) Read(p []byte) (int, error) {
	if o.pos >= o.size {
		return 0, io.EOF
	}
	if o.body == nil {
		resp, err := o.get(o.pos)
		if err != nil {
			return 0, err
		}
		o.body = resp.Body
	}
	n, err := o.body.Read(p)
	o.pos += int64(n)
	if err == io.EOF && o.pos < o.size {
		err = io.ErrUnexpectedEOF
	}
	return n, err
}

func (o *s3Object) Seek(offset int64, whence int) (int64, error) {
	var abs int64
	switch whence {
	case io.SeekStart:
		abs = offset
	case io.SeekCurrent:
		abs = o.pos + offset
	case io.SeekEnd:
		abs = o.size + offset
	default:
		return 0, errors.New("blobstore: invalid whence")
	}
	if abs < 0 {
		return 0, errors.New("blobstore: negative position")
	}
	if abs != o.pos && o.body != nil {
		o.body.Close()
		o.body = nil
	}
	o.pos = abs
	return abs, nil
}

func (o *s3Object) Close() error {
	if o.body == nil {
		return nil
	}
	err := o.body.Close()
	o.body = nil
	return err
}

// s3Error turns an S3 error response into an error with its status and the
// start of its XML body (which carries the error code, never credentials).
func s3Error(op string, resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	return fmt.Errorf("blobstore: S3 %s: %s: %s", op, resp.Status, strings.TrimSpace(string(body)))
}
