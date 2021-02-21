package middleware

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"github.com/b2wdigital/restQL-golang/v4/pkg/restql"
	"github.com/go-redis/redis"
	"github.com/pquerna/cachecontrol/cacheobject"
	"github.com/valyala/fasthttp"
)

type cache struct {
	logger  restql.Logger
	storage *redis.Client
}

type cacheOption func(c *cache)

func (c *cache) checksum(ctx *fasthttp.RequestCtx) (string, error) {
	content := make([]byte, 0, len(ctx.Method())+len(ctx.RequestURI()))

	content = append(content, ctx.Method()...)
	content = append(content, '-')
	content = append(content, ctx.RequestURI()...)

	h := md5.New()
	_, err := h.Write(content)
	if err != nil {
		return "", err
	}

	checksum := hex.EncodeToString(h.Sum(nil))
	return checksum, nil
}

func (c *cache) getCache(key string) (cachedResponse, error) {
	result := c.storage.Get(key)
	if err := result.Err(); err == redis.Nil {
		return cachedResponse{}, nil
	} else if err != nil {
		return cachedResponse{}, err
	}
	bt, err := result.Bytes()
	if err != nil {
		return cachedResponse{}, err
	}
	var response cachedResponse
	if err = json.Unmarshal(bt, &response); err != nil {
		return cachedResponse{}, err
	}
	return response, nil
}

type cachedResponse struct {
	Response         []byte
	Headers          http.Header
	ValidUntilInSecs time.Time
}

func (r cachedResponse) Expired(now time.Time) bool {
	return now.Sub(r.ValidUntilInSecs) > 0
}

func (c *cache) setCache(key string, ctx *fasthttp.RequestCtx, validUntil time.Time) error {
	respHeaders := make(http.Header, ctx.Response.Header.Len())
	ctx.Response.Header.VisitAll(func(key, value []byte) { respHeaders.Add(string(key), string(value)) })

	value := cachedResponse{
		Headers:          respHeaders,
		Response:         ctx.Response.Body(),
		ValidUntilInSecs: validUntil,
	}
	content, err := json.Marshal(value)
	if err != nil {
		return err
	}
	expiration := validUntil.Sub(time.Now())
	return c.storage.Set(key, content, expiration).Err()
}

func (c *cache) serveFromCache(ctx *fasthttp.RequestCtx, cacheKey string, logger restql.Logger) bool {
	cached, err := c.getCache(cacheKey)
	if err != nil {
		logger.Warn("error reading cache", "err", err)
		return false
	}

	if cached.Expired(time.Now()) {
		logger.Debug("cache expired")
		return false
	}

	logger.Info("serving from cache")
	for key, values := range cached.Headers {
		for _, value := range values {
			ctx.Response.Header.Add(key, value)
		}
	}
	ctx.Response.SetBodyRaw(cached.Response)
	return true
}

func (c *cache) cacheMeta(ctx *fasthttp.RequestCtx) ([]cacheobject.Reason, time.Time, error) {
	respHeaders := make(http.Header, ctx.Response.Header.Len())
	ctx.Response.Header.VisitAll(func(key, value []byte) { respHeaders.Add(string(key), string(value)) })

	reqHeader := http.Header{}
	if authz := ctx.Request.Header.Peek(fasthttp.HeaderAuthorization); authz != nil {
		reqHeader.Set(fasthttp.HeaderAuthorization, string(authz))
	}

	req := &http.Request{
		Header: reqHeader,
		Method: string(ctx.Method()),
	}
	return cacheobject.UsingRequestResponse(req, ctx.Response.StatusCode(), respHeaders, false)
}

func (c *cache) Apply(h fasthttp.RequestHandler) fasthttp.RequestHandler {
	return func(ctx *fasthttp.RequestCtx) {
		logger := c.logger.With("restql-endpoint", string(ctx.Request.URI().Path()))

		// Caching only GET requests.
		if string(ctx.Method()) != fasthttp.MethodGet {
			h(ctx)
			return
		}

		cacheKey, err := c.checksum(ctx)
		if err != nil {
			logger.Debug("error creating checksum. skipping cache check", "err", err)
			h(ctx)
			return
		}

		if c.serveFromCache(ctx, cacheKey, logger) {
			return
		}

		h(ctx)

		if ctx.Response.IsBodyStream() {
			logger.Debug("ignoring cache storage because body is set via setBodyStream")
			return
		}

		dontCacheReasons, duration, err := c.cacheMeta(ctx)
		if err != nil {
			logger.Warn("error calculating cache duration", "err", err)
			return
		}

		if len(dontCacheReasons) != 0 {
			logger.Debug("got reasons to not cache this request")
			return
		}

		if duration.IsZero() {
			logger.Debug("cache disabled for this request")
			return
		}

		if err = c.setCache(cacheKey, ctx, duration); err != nil {
			logger.Warn("error updating cache", "err", err)
		}
	}
}

func newCache(log restql.Logger, options ...cacheOption) Middleware {
	c := cache{logger: log}

	for _, option := range options {
		option(&c)
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "", // no password set
		DB:       0,  // use default DB
	})
	c.storage = rdb

	return &c
}
