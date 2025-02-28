package cache

import (
	"net/http"
	"go.uber.org/zap"
	"strconv"
)

func NewCustomWriter(rw http.ResponseWriter, r *http.Request, db *Store, logger *zap.Logger, path string, codes []string) *CustomWriter {	
	nw := CustomWriter{rw, r, db, logger, path, 0, codes, 200}
	
	return &nw
}

// CustomWriter handles the response and provide the way to cache the value
type CustomWriter struct {
	http.ResponseWriter
	*http.Request
	*Store
	*zap.Logger
	path string
	idx int
	cacheResponseCodes []string
	status int
}

func (r *CustomWriter) Header() http.Header {
	return r.ResponseWriter.Header()
}

func (r *CustomWriter) WriteHeader(status int) {
	r.Logger.Debug("==========-SetHeader-==========")
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// Write will write the response body
func (r *CustomWriter) Write(b []byte) (int, error) {
	r.Logger.Debug("Writing customwriter response", zap.String("path", r.path))
	// content encoding
	ct := r.Header().Get("Content-Encoding")
	r.Header().Set("X-WPEverywhere-Cache", "MISS")
	bypass := true

	// check if the response code is in the cache response codes
	for _, code := range r.cacheResponseCodes {
		status := strconv.Itoa(r.status)
		r.Logger.Debug("Checking status code", zap.String("code", code), zap.String("status", status))

		if code == status {
			r.Logger.Debug("Caching because of status code", zap.String("code", code), zap.String("status", status))
			bypass = false
			break
		}

		// code may be single digit because of wildcard usage (e.g. 2XX, 4XX, 5XX)
		if len(code) == 1 {
			if code == status[0:1] {
				r.Logger.Debug("Caching because of wildcard", zap.String("code", code), zap.String("status", status))
				bypass = false
				break
			}
		}
	}

	if !bypass {
		if ct == "" {
			ct = "none"
		}

		cacheKey := ct + "::" + r.path

		r.Logger.Debug("Cache Key", zap.String("Key", cacheKey))
		r.Store.Set(cacheKey, r.idx, b)
		r.idx++
	}

	return r.ResponseWriter.Write(b)
}
