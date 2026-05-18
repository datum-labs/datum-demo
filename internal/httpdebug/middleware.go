// Package httpdebug provides an HTTP middleware that logs every served
// request: method, path, remote address, user-agent, response status,
// bytes written, and handler duration. Used by --debug mode in both the
// prober and origin subcommands.
package httpdebug

import (
	"log"
	"net/http"
	"time"
)

func LogMiddleware(tag string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &recorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.Printf("%s: << %s %s from=%s ua=%q status=%d bytes=%d dur=%.2fms",
			tag,
			r.Method, r.URL.RequestURI(),
			r.RemoteAddr, r.Header.Get("User-Agent"),
			rec.status, rec.bytes,
			float64(time.Since(start).Microseconds())/1000.0,
		)
	})
}

type recorder struct {
	http.ResponseWriter
	status      int
	bytes       int
	wroteHeader bool
}

func (r *recorder) WriteHeader(code int) {
	if r.wroteHeader {
		return
	}
	r.status = code
	r.wroteHeader = true
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Write(b []byte) (int, error) {
	if !r.wroteHeader {
		r.wroteHeader = true
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}
