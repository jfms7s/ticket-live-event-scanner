package static

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

const inlineBody = "\n    var theme = 1;\n  "

const indexHTML = `<!DOCTYPE html><html><head><script>` + inlineBody + `</script>` +
	`<script type="module" src="/assets/app-abc123.js"></script></head><body></body></html>`

func testFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html":           {Data: []byte(indexHTML)},
		"assets/app-abc123.js": {Data: []byte(strings.Repeat("console.log(1);", 50))},
		"favicon.png":          {Data: []byte("png")},
	}
}

func get(h http.Handler, method, target string, hdr ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestIndexAtRoot(t *testing.T) {
	rec := get(Handler(testFS()), http.MethodGet, "/")
	if rec.Code != http.StatusOK || rec.Body.String() != indexHTML {
		t.Fatalf("GET / = %d %q", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("index Cache-Control = %q, want no-cache", got)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Errorf("index Content-Type = %q", got)
	}
}

func TestAssetsAreImmutable(t *testing.T) {
	rec := get(Handler(testFS()), http.MethodGet, "/assets/app-abc123.js")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Errorf("asset Cache-Control = %q, want immutable", got)
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "javascript") {
		t.Errorf("asset Content-Type = %q", got)
	}
}

func TestUnknownPathsFallBackOrNotFound(t *testing.T) {
	h := Handler(testFS())
	if rec := get(h, http.MethodGet, "/some/client/route"); rec.Code != http.StatusOK || rec.Body.String() != indexHTML {
		t.Errorf("SPA route = %d, want index.html", rec.Code)
	}
	for _, p := range []string{"/assets/missing.js", "/missing.png", "/api/nope", "/api", "/healthz/", "/metrics/"} {
		if rec := get(h, http.MethodGet, p); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", p, rec.Code)
		}
	}
}

func TestTraversalCannotEscape(t *testing.T) {
	rec := get(Handler(testFS()), http.MethodGet, "/../../etc/passwd")
	if rec.Code == http.StatusOK && rec.Body.String() != indexHTML {
		t.Errorf("traversal served %q", rec.Body.String())
	}
}

func TestGzipNegotiation(t *testing.T) {
	h := Handler(testFS())
	rec := get(h, http.MethodGet, "/assets/app-abc123.js", "Accept-Encoding", "br, gzip;q=0.8")
	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatal("gzip not served to a client that accepts it")
	}
	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(zr)
	if !strings.HasPrefix(string(body), "console.log(1);") {
		t.Errorf("decompressed body = %q", body)
	}
	for _, ae := range []string{"", "gzip;q=0", "identity"} {
		rec := get(h, http.MethodGet, "/assets/app-abc123.js", "Accept-Encoding", ae)
		if rec.Header().Get("Content-Encoding") != "" {
			t.Errorf("Accept-Encoding %q got gzip", ae)
		}
		if rec.Header().Get("Vary") != "Accept-Encoding" {
			t.Errorf("Vary missing for %q", ae)
		}
	}
	if rec := get(h, http.MethodGet, "/favicon.png", "Accept-Encoding", "gzip"); rec.Header().Get("Content-Encoding") != "" {
		t.Error("png was gzipped")
	}
}

func TestSecurityHeadersAndCSPHash(t *testing.T) {
	rec := get(Handler(testFS()), http.MethodGet, "/")
	csp := rec.Header().Get("Content-Security-Policy")
	// sha256 of inlineBody, base64.
	if !strings.Contains(csp, "script-src 'self' 'sha256-") || strings.Count(csp, "'sha256-") != 1 {
		t.Errorf("CSP should hash exactly the one inline script: %q", csp)
	}
	if strings.Contains(csp, "unsafe-inline") || !strings.Contains(csp, "frame-ancestors 'none'") {
		t.Errorf("CSP = %q", csp)
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("missing nosniff")
	}
}

func TestMethods(t *testing.T) {
	h := Handler(testFS())
	if rec := get(h, http.MethodPost, "/"); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST / = %d", rec.Code)
	}
	rec := get(h, http.MethodHead, "/")
	if rec.Code != http.StatusOK || rec.Body.Len() != 0 || rec.Header().Get("Content-Length") == "" {
		t.Errorf("HEAD / = %d, body %d bytes", rec.Code, rec.Body.Len())
	}
}

func TestEmptyBuildServesNotFound(t *testing.T) {
	h := Handler(fstest.MapFS{".gitkeep": {}})
	if rec := get(h, http.MethodGet, "/"); rec.Code != http.StatusNotFound {
		t.Errorf("GET / without a build = %d, want 404", rec.Code)
	}
}

func TestVaryIsAppendedNotReplaced(t *testing.T) {
	h := Handler(testFS())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	rec.Header().Set("Vary", "Origin")
	h.ServeHTTP(rec, req)
	if got := rec.Header().Values("Vary"); len(got) != 2 {
		t.Errorf("Vary = %v, want Origin and Accept-Encoding", got)
	}
}

func TestDataSrcDoesNotExemptInlineScript(t *testing.T) {
	idx := &file{data: []byte(`<script data-src="x">var a;</script>`)}
	if csp := contentSecurityPolicy(idx); !strings.Contains(csp, "'sha256-") {
		t.Errorf("inline script with data-src not hashed: %q", csp)
	}
}
