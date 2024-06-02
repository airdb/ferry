package main

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"
)

// Module Compress Gzip Compression
type modCompressGzipResponseWriter struct {
	io.Writer
	http.ResponseWriter
}

func (w modCompressGzipResponseWriter) Write(b []byte) (int, error) {
	return w.Writer.Write(b)
}

func modCompressGzip(handler http.Handler, level int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			handler.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Encoding", "gzip")
		gz, err := gzip.NewWriterLevel(w, level)
		if err != nil {
			panic(err)
		}
		defer gz.Close()
		gzw := modCompressGzipResponseWriter{Writer: gz, ResponseWriter: w}
		handler.ServeHTTP(gzw, r)
	})
}
