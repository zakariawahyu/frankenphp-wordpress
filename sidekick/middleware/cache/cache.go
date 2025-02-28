package cache

import (
	"os"
	"strings"
	"regexp"
	"strconv"
	"encoding/json"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"go.uber.org/zap"
	"net/http"
)

type Cache struct {
	logger   *zap.Logger
	Loc     string
	PurgePath string
	PurgeKey string
	BypassPathPrefixes []string
	BypassHome bool
	CacheResponseCodes []string
	TTL int
	Store *Store
}

func init() {
	caddy.RegisterModule(Cache{})
	httpcaddyfile.RegisterHandlerDirective("wp_cache", parseCaddyfileHandler)
}

func parseCaddyfileHandler(h httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler,
	error) {
	c := new(Cache)
	if err := c.UnmarshalCaddyfile(h.Dispenser); err != nil {
		return nil, err
	}

	return c, nil
}

func (c *Cache) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	for d.Next() {
		var value string

		key := d.Val()

		if !d.Args(&value) {
			continue
		}

		switch key {
		case "loc":
			c.Loc = value

		case "bypass_path_prefixes":
			c.BypassPathPrefixes = strings.Split(strings.TrimSpace(value), ",")

		case "bypass_home":
			if value == "true" {
				c.BypassHome = true
			}

		case "cache_response_codes":
			codes := strings.Split(strings.TrimSpace(value), ",")
			c.CacheResponseCodes = make([]string, len(codes))

			for i, code := range codes {
				code = strings.TrimSpace(code)
				if strings.Contains(code, "XX") {
					code = string(code[0])
				}
				c.CacheResponseCodes[i] = code
			}

		case "ttl":
			ttl, err := strconv.Atoi(value)
			if err != nil {
				c.logger.Error("Invalid TTL value", zap.Error(err))
				continue
			}
			c.TTL = ttl

		case "purge_path":
			c.PurgePath = value

		case "purge_key":
			c.PurgeKey = value
		}
	}

	return nil
}

func (c *Cache) Provision(ctx caddy.Context) error {
	c.logger = ctx.Logger(c)

	if c.Loc == "" {
		c.Loc = os.Getenv("CACHE_LOC")
	}

	if c.CacheResponseCodes == nil {
		codes := strings.Split(os.Getenv("CACHE_RESPONSE_CODES"), ",")
		c.CacheResponseCodes = make([]string, len(codes))

		for i, code := range codes {
			code = strings.TrimSpace(code)
			if strings.Contains(code, "XX") {
				code = string(code[0])
			}
			c.CacheResponseCodes[i] = code
		}
	}

	if c.BypassPathPrefixes == nil {
		c.BypassPathPrefixes = strings.Split(strings.TrimSpace(os.Getenv("BYPASS_PATH_PREFIX")), ",")
	}

	if c.BypassHome == false {
		if os.Getenv("BYPASS_HOME") == strings.ToLower("true") {
			c.BypassHome = true
		}
	}

	if c.TTL == 0 {
		ttl, err := strconv.Atoi(os.Getenv("TTL"))
		if err != nil {
			c.logger.Error("Invalid TTL value", zap.Error(err))
		}
		c.TTL = ttl
	}

	if c.PurgePath == "" {
		c.PurgePath = os.Getenv("PURGE_PATH")

		if c.PurgePath == "" {
			c.PurgePath = "/__wp_cache/purge"
		}
	}

	if c.PurgeKey == "" {
		c.PurgeKey = os.Getenv("PURGE_KEY")
	}

	c.Store = NewStore(c.Loc, c.TTL, c.logger)

	return nil
}

func (Cache) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID: "http.handlers.wp_cache",
		New: func() caddy.Module {
			return new(Cache)
		},
	}
}

// ServeHTTP implements the caddy.Handler interface.
func (c Cache) ServeHTTP(w http.ResponseWriter, r *http.Request,
	next caddyhttp.Handler) error {
	bypass := false
	encoding := ""

	c.logger.Debug("HTTP Version", zap.String("Version", r.Proto))

	for _, prefix := range c.BypassPathPrefixes {
		if strings.HasPrefix(r.URL.Path, prefix) && prefix != "" {
			c.logger.Debug("wp cache - bypass prefix", zap.String("prefix", prefix))
			bypass = true
			break
		}
	}

	// bypass all media, images, css, js, etc
	match, _ := regexp.MatchString(".*(\\.[^.]+)$", r.URL.Path)

	if match {
		bypass = true
	}

	if c.BypassHome && r.URL.Path == "/" {
		bypass = true
	}

	

	if bypass  {
		return next.ServeHTTP(w, r)
	}

	db := c.Store
	nw := NewCustomWriter(w, r, db, c.logger, r.URL.Path, c.CacheResponseCodes)

	if strings.Contains(r.URL.Path, c.PurgePath) && r.Method == "GET" {
		key := r.Header.Get("X-WPSidekick-Purge-Key")

		if key == c.PurgeKey {
			cacheList := db.List()

			json.NewEncoder(w).Encode(cacheList)

			return nil
		} else {
			c.logger.Warn("wp cache - purge - invalid key", zap.String("path", r.URL.Path))
		}
	}

	if strings.Contains(r.URL.Path, c.PurgePath) && r.Method == "POST" {
		key := r.Header.Get("X-WPSidekick-Purge-Key")

		if key == c.PurgeKey {
			pathToPurge := strings.Replace(r.URL.Path, c.PurgePath, "", 1)
			c.logger.Debug("wp cache - purge", zap.String("path", pathToPurge))

			if len(pathToPurge) < 2 {
				go db.Flush()
			} else {
				go db.Purge(pathToPurge)
			}
		} else {
			c.logger.Warn("wp cache - purge - invalid key", zap.String("path", r.URL.Path))
		}

		w.Write([]byte("OK"))

		return nil
	}
	
	// bypass if is logged in. We don't want to cache admin bars
	cookies := r.Header.Get("Cookie")
	if strings.Contains(cookies, "wordpress_logged_in") {
		return next.ServeHTTP(w, r)
	}

	requestHeader := r.Header
	requestEncoding := requestHeader["Accept-Encoding"]

	for _, re := range requestEncoding {
		if strings.Contains(re, "br") {
			encoding = "br"
			break
		} else if strings.Contains(re, "gzip") {
			encoding = "gzip"
		}
	}

	if encoding == "" {
		encoding = "none"
	}

	cacheKey := encoding + "::" + r.URL.Path
	cacheItem, err := db.Get(cacheKey)

	if err != nil {
		c.logger.Debug("wp cache - error - " + cacheKey, zap.Error(err))
	}

	if err == nil {		
		w.Header().Set("X-WPEverywhere-Cache", "HIT")
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		w.Header().Set("Server", "Caddy")
		w.Header().Set("Vary", "Accept-Encoding")
		w.Header().Set("Content-Encoding", encoding)
		w.Write(cacheItem)

		return nil
	}


	return next.ServeHTTP(nw, r)
}