package blobstore

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"sort"
	"strings"
	"time"
)

// AWS Signature Version 4 for S3, header-signed. Reference:
// https://docs.aws.amazon.com/AmazonS3/latest/API/sig-v4-header-based-auth.html

const (
	// unsignedPayload skips hashing upload bodies. TLS already protects them
	// in transit, and it lets Put stream rather than buffer to hash.
	unsignedPayload  = "UNSIGNED-PAYLOAD"
	emptyPayloadHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	sigV4Algorithm   = "AWS4-HMAC-SHA256"
	sigV4TimeFormat  = "20060102T150405Z"
	sigV4DateFormat  = "20060102"
)

type sigV4 struct {
	accessKeyID     string
	secretAccessKey string
	region          string
	service         string
}

// sign adds x-amz-date, x-amz-content-sha256 and Authorization to req. It
// signs Host, any Content-Type and Range, and the x-amz-* headers.
func (s sigV4) sign(req *http.Request, payloadHash string, now time.Time) {
	now = now.UTC()
	amzDate := now.Format(sigV4TimeFormat)
	date := now.Format(sigV4DateFormat)
	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)

	host := req.URL.Host
	if req.Host != "" {
		host = req.Host
	}
	headers := map[string]string{"host": host}
	for name, values := range req.Header {
		lower := strings.ToLower(name)
		if lower == "content-type" || lower == "range" || strings.HasPrefix(lower, "x-amz-") {
			headers[lower] = strings.Join(trimAll(values), ",")
		}
	}
	names := make([]string, 0, len(headers))
	for n := range headers {
		names = append(names, n)
	}
	sort.Strings(names)
	var canonHeaders strings.Builder
	for _, n := range names {
		canonHeaders.WriteString(n + ":" + headers[n] + "\n")
	}
	signedHeaders := strings.Join(names, ";")

	canonicalRequest := strings.Join([]string{
		req.Method,
		req.URL.EscapedPath(),
		canonicalQuery(req),
		canonHeaders.String(),
		signedHeaders,
		payloadHash,
	}, "\n")

	scope := date + "/" + s.region + "/" + s.service + "/aws4_request"
	stringToSign := strings.Join([]string{sigV4Algorithm, amzDate, scope, hexSHA256(canonicalRequest)}, "\n")

	key := hmacSHA256([]byte("AWS4"+s.secretAccessKey), date)
	key = hmacSHA256(key, s.region)
	key = hmacSHA256(key, s.service)
	key = hmacSHA256(key, "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(key, stringToSign))

	req.Header.Set("Authorization", sigV4Algorithm+" Credential="+s.accessKeyID+"/"+scope+
		", SignedHeaders="+signedHeaders+", Signature="+signature)
}

func canonicalQuery(req *http.Request) string {
	q := req.URL.Query()
	if len(q) == 0 {
		return ""
	}
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		vals := q[k]
		sort.Strings(vals)
		for _, v := range vals {
			parts = append(parts, awsURIEscape(k, true)+"="+awsURIEscape(v, true))
		}
	}
	return strings.Join(parts, "&")
}

// awsURIEscapePath escapes a path for SigV4's canonical URI: every byte
// except unreserved characters and '/' is percent-encoded.
func awsURIEscapePath(p string) string {
	return awsURIEscape(p, false)
}

func awsURIEscape(s string, encodeSlash bool) string {
	const hexDigits = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case 'A' <= c && c <= 'Z', 'a' <= c && c <= 'z', '0' <= c && c <= '9',
			c == '-', c == '_', c == '.', c == '~':
			b.WriteByte(c)
		case c == '/' && !encodeSlash:
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte(hexDigits[c>>4])
			b.WriteByte(hexDigits[c&15])
		}
	}
	return b.String()
}

func trimAll(values []string) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = strings.Join(strings.Fields(v), " ")
	}
	return out
}

func hmacSHA256(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}

func hexSHA256(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
