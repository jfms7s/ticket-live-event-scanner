// Package static serves the built dashboard (the Vite output embedded from
// web/frontend) from the same origin as the API.
package static

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"
)

const (
	// Vite content-hashes everything under /assets/, so a changed file gets
	// a new URL and the old one can be cached forever. Everything else
	// (index.html) must be revalidated so a new build is picked up.
	immutableCache  = "public, max-age=31536000, immutable"
	revalidateCache = "no-cache"
	indexName       = "index.html"
)

// Text types worth gzipping; images and fonts are already compressed.
var compressible = map[string]bool{
	".html": true, ".css": true, ".js": true, ".mjs": true, ".json": true, ".svg": true,
}

var inlineScript = regexp.MustCompile(`(?is)<script\b([^>]*)>(.*?)</script>`)
var srcAttr = regexp.MustCompile(`(?i)\ssrc\s*=`)

type file struct {
	contentType string
	data        []byte
	gzip        []byte // nil when the type is not compressible
	cache       string
}

// Handler serves the files of fsys (a Vite build output) with long-lived
// caching for /assets/, gzip, security headers and a Content-Security-Policy
// that allows only the inline scripts index.html contains. Unknown
// extension-less paths fall back to index.html so client-side routes load;
// unknown paths under /api/ and missing files answer 404.
func Handler(fsys fs.FS) http.Handler {
	files := map[string]*file{}
	_ = fs.WalkDir(fsys, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil
		}
		ext := strings.ToLower(path.Ext(name))
		f := &file{contentType: mime.TypeByExtension(ext), data: data, cache: revalidateCache}
		if f.contentType == "" {
			f.contentType = "application/octet-stream"
		}
		if strings.HasPrefix(name, "assets/") {
			f.cache = immutableCache
		}
		if compressible[ext] {
			var buf bytes.Buffer
			zw := gzip.NewWriter(&buf)
			_, _ = zw.Write(data)
			_ = zw.Close()
			f.gzip = buf.Bytes()
		}
		files[name] = f
		return nil
	})

	csp := contentSecurityPolicy(files[indexName])

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "" {
			name = indexName
		}
		f, ok := files[name]
		if !ok && path.Ext(name) == "" && !isAPI(name) {
			f, ok = files[indexName]
		}
		if !ok {
			http.NotFound(w, r)
			return
		}

		h := w.Header()
		h.Set("Content-Type", f.contentType)
		h.Set("Cache-Control", f.cache)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Content-Security-Policy", csp)
		body := f.data
		if f.gzip != nil {
			h.Add("Vary", "Accept-Encoding")
			if acceptsGzip(r) {
				body = f.gzip
				h.Set("Content-Encoding", "gzip")
			}
		}
		h.Set("Content-Length", strconv.Itoa(len(body)))
		if r.Method == http.MethodGet {
			_, _ = w.Write(body)
		}
	})
}

// isAPI reports paths owned by the API, which must 404 rather than fall back
// to index.html (a probe or scrape of /healthz/ must not see the dashboard).
func isAPI(name string) bool {
	for _, p := range []string{"api", "healthz", "metrics"} {
		if name == p || strings.HasPrefix(name, p+"/") {
			return true
		}
	}
	return false
}

// contentSecurityPolicy allows scripts from this origin plus the hash of
// each inline script in index.html (the theme bootstrap), without
// 'unsafe-inline'. The API shares the origin, so connect-src is 'self'.
func contentSecurityPolicy(index *file) string {
	scripts := []string{"'self'"}
	if index != nil {
		for _, m := range inlineScript.FindAllSubmatch(index.data, -1) {
			if srcAttr.Match(m[1]) {
				continue
			}
			sum := sha256.Sum256(m[2])
			scripts = append(scripts, "'sha256-"+base64.StdEncoding.EncodeToString(sum[:])+"'")
		}
	}
	return strings.Join([]string{
		"default-src 'self'",
		"script-src " + strings.Join(scripts, " "),
		"img-src 'self' https: data:",
		"connect-src 'self'",
		"style-src 'self'",
		"base-uri 'self'",
		"form-action 'self'",
		"frame-ancestors 'none'",
	}, "; ")
}

func acceptsGzip(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		coding, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		if strings.TrimSpace(coding) != "gzip" {
			continue
		}
		q := strings.ReplaceAll(strings.TrimSpace(params), " ", "")
		return q != "q=0" && q != "q=0.0" && q != "q=0.00" && q != "q=0.000"
	}
	return false
}
