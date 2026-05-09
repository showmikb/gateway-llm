package replay

// S3-compatible blob store implemented without an AWS SDK dependency so
// gateway-llm stays a single static binary. Uses signed PUT/GET with the
// AWS sigv4 signature, which is ~200 lines and works against S3, MinIO,
// Cloudflare R2, Backblaze B2 S3-compat, and every other S3-compatible
// store.
//
// For the v0 we use a small subset: path-style or virtual-hosted PUT/GET,
// and pick credentials from environment variables the way aws-sdk does.

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

type s3Store struct {
	bucket  string
	prefix  string
	region  string
	endpoint string
	accessKey string
	secretKey string
	client  *http.Client
}

func NewS3Store(bucket, prefix string) (BlobStore, error) {
	if bucket == "" {
		return nil, fmt.Errorf("s3 bucket required")
	}
	region := envDefault("AWS_REGION", "us-east-1")
	endpoint := os.Getenv("GATEWAY_LLM_S3_ENDPOINT") // e.g. https://minio.example.com
	if endpoint == "" {
		endpoint = fmt.Sprintf("https://s3.%s.amazonaws.com", region)
	}
	return &s3Store{
		bucket:    bucket,
		prefix:    strings.TrimPrefix(prefix, "/"),
		region:    region,
		endpoint:  endpoint,
		accessKey: os.Getenv("AWS_ACCESS_KEY_ID"),
		secretKey: os.Getenv("AWS_SECRET_ACCESS_KEY"),
		client:    &http.Client{Timeout: 30 * time.Second},
	}, nil
}

func envDefault(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func (s *s3Store) Scheme() string { return "s3" }

func (s *s3Store) key(innerKey string) string {
	if s.prefix == "" {
		return innerKey
	}
	return s.prefix + "/" + innerKey
}

func (s *s3Store) Put(ctx context.Context, _ string, data []byte) (string, error) {
	gz, err := gzipBytes(data)
	if err != nil {
		return "", err
	}
	innerKey, _ := hashKey(data)
	fullKey := s.key(innerKey)
	u := fmt.Sprintf("%s/%s/%s", s.endpoint, s.bucket, fullKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u, bytes.NewReader(gz))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/gzip")
	req.ContentLength = int64(len(gz))
	if err := s.signV4(req, gz); err != nil {
		return "", err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("s3 put %s: %d: %s", u, resp.StatusCode, string(body))
	}
	return fmt.Sprintf("s3://%s/%s", s.bucket, fullKey), nil
}

func (s *s3Store) Get(ctx context.Context, raw string) ([]byte, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	bucket := u.Host
	key := strings.TrimPrefix(u.Path, "/")
	endpoint := fmt.Sprintf("%s/%s/%s", s.endpoint, bucket, key)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if err := s.signV4(req, nil); err != nil {
		return nil, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("s3 get %s: %d", endpoint, resp.StatusCode)
	}
	gz, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return gunzipBytes(gz)
}

// signV4 signs a request using AWS Signature V4. It's intentionally minimal:
// we do not attempt to handle every edge case of the spec — only the subset
// we need for PUT/GET to S3-compatible stores.
func (s *s3Store) signV4(req *http.Request, body []byte) error {
	if s.accessKey == "" || s.secretKey == "" {
		return nil // allow unsigned requests against dev MinIO with public buckets
	}
	now := time.Now().UTC()
	date := now.Format("20060102T150405Z")
	shortDate := now.Format("20060102")

	payloadHash := hashHex(body)
	req.Header.Set("x-amz-content-sha256", payloadHash)
	req.Header.Set("x-amz-date", date)

	canonicalHeaders, signedHeaders := canonicalizeHeaders(req)
	canonicalQuery := canonicalizeQuery(req.URL)
	canonicalURI := req.URL.EscapedPath()

	canonicalRequest := strings.Join([]string{
		req.Method,
		canonicalURI,
		canonicalQuery,
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")

	scope := fmt.Sprintf("%s/%s/s3/aws4_request", shortDate, s.region)
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		date,
		scope,
		hashHex([]byte(canonicalRequest)),
	}, "\n")

	kDate := hmacSHA256([]byte("AWS4"+s.secretKey), []byte(shortDate))
	kRegion := hmacSHA256(kDate, []byte(s.region))
	kService := hmacSHA256(kRegion, []byte("s3"))
	kSigning := hmacSHA256(kService, []byte("aws4_request"))
	sig := hex.EncodeToString(hmacSHA256(kSigning, []byte(stringToSign)))

	auth := fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		s.accessKey, scope, signedHeaders, sig)
	req.Header.Set("Authorization", auth)
	return nil
}

func hashHex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key, data []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write(data)
	return m.Sum(nil)
}

func canonicalizeHeaders(req *http.Request) (canonical, signed string) {
	// Host is mandatory.
	if req.Header.Get("Host") == "" {
		req.Header.Set("Host", req.URL.Host)
	}
	keys := make([]string, 0, len(req.Header))
	for k := range req.Header {
		keys = append(keys, strings.ToLower(k))
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteString(":")
		b.WriteString(strings.TrimSpace(req.Header.Get(k)))
		b.WriteString("\n")
	}
	return b.String(), strings.Join(keys, ";")
}

func canonicalizeQuery(u *url.URL) string {
	q := u.Query()
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		for _, v := range q[k] {
			parts = append(parts, url.QueryEscape(k)+"="+url.QueryEscape(v))
		}
	}
	return strings.Join(parts, "&")
}
