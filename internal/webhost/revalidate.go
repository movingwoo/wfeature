package webhost

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"
)

// The page's own files and the game list are answered the same way: a
// validator the browser can ask about, and gzip where the browser takes it.
//
// They used to be sent whole on every load with `no-store` — a quarter of a
// megabyte of script and style each time the page opened, uncompressed, which
// is a real cost on a phone reaching a server over a metered link. The service
// worker fetches the network first so a new build shows up at once; a
// revalidation keeps that and answers an unchanged file with 304 and no body.

// minCompressed is the smallest body worth compressing. Below it the gzip
// header and trailer eat most of what compression saves.
const minCompressed = 1024

// compressedTypes are the types that are text and therefore compress.
var compressedTypes = map[string]bool{
	contentTypes[".css"]:         true,
	contentTypes[".html"]:        true,
	contentTypes[".js"]:          true,
	contentTypes[".json"]:        true,
	contentTypes[".webmanifest"]: true,
	"text/plain; charset=utf-8":  true,
}

// maxCompressedEntries bounds the cache below. The shell is a few dozen files;
// a development server reading web/ from disk makes a new entry for every edit.
const maxCompressedEntries = 128

// compressedBodies holds each body's gzip form under its validator, so a file
// is compressed once rather than on every request.
type compressedBodies struct {
	mutex   sync.Mutex
	entries map[string][]byte
}

func (c *compressedBodies) get(tag string, body []byte) []byte {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if compressed, ok := c.entries[tag]; ok {
		return compressed
	}
	var out bytes.Buffer
	writer, _ := gzip.NewWriterLevel(&out, gzip.BestCompression)
	_, _ = writer.Write(body)
	_ = writer.Close()
	if c.entries == nil || len(c.entries) >= maxCompressedEntries {
		c.entries = make(map[string][]byte)
	}
	c.entries[tag] = out.Bytes()
	return out.Bytes()
}

// bodyTag is a weak validator: the same content is sent both compressed and
// not, and a weak tag names the content rather than one of those encodings.
func bodyTag(body []byte) string {
	sum := sha256.Sum256(body)
	return `W/"` + hex.EncodeToString(sum[:12]) + `"`
}

// acceptsGzip reads Accept-Encoding for gzip with a nonzero quality.
func acceptsGzip(request *http.Request) bool {
	for _, field := range request.Header.Values("Accept-Encoding") {
		for _, part := range strings.Split(field, ",") {
			name, parameters, _ := strings.Cut(strings.TrimSpace(part), ";")
			if !strings.EqualFold(strings.TrimSpace(name), "gzip") {
				continue
			}
			quality := strings.ReplaceAll(strings.TrimSpace(parameters), " ", "")
			return quality != "q=0" && quality != "q=0.0" && quality != "q=0.00" && quality != "q=0.000"
		}
	}
	return false
}

// serveRevalidated answers body so that an unchanged copy costs the browser a
// 304 and a changed one arrives compressed where it can be.
func (s *Server) serveRevalidated(writer http.ResponseWriter, request *http.Request, contentType string, body []byte) {
	header := writer.Header()
	securityHeaders(header)
	tag := bodyTag(body)
	header.Set("Content-Type", contentType)
	header.Set("Cache-Control", "no-cache")
	header.Set("ETag", tag)
	payload := body
	if compressedTypes[contentType] && len(body) >= minCompressed {
		header.Add("Vary", "Accept-Encoding")
		if acceptsGzip(request) {
			payload = s.compressed.get(tag, body)
			header.Set("Content-Encoding", "gzip")
		}
	}
	// ServeContent answers If-None-Match against the tag set above, and a
	// 304 drops the body headers.
	http.ServeContent(writer, request, "", time.Time{}, bytes.NewReader(payload))
}
