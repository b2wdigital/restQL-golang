package middleware

import (
	"fmt"

	"github.com/b2wdigital/restQL-golang/v4/internal/platform/conf"
	"github.com/b2wdigital/restQL-golang/v4/internal/platform/plugins"
	"github.com/b2wdigital/restQL-golang/v4/pkg/restql"
	"github.com/valyala/fasthttp"
)

// Middleware defines a generic handler wrapper
// capable of controlling request flow.
type Middleware interface {
	Apply(h fasthttp.RequestHandler) fasthttp.RequestHandler
}

type noopMiddleware struct{}

func (nm noopMiddleware) Apply(h fasthttp.RequestHandler) fasthttp.RequestHandler {
	return h
}

// Decorator is a type responsible for wrapping handlers
// with default and config enabled middlewares.
type Decorator struct {
	log restql.Logger
	cfg *conf.Config
	pm  plugins.Lifecycle
	cm  *ConnManager
}

// NewDecorator creates a middleware Decorator
func NewDecorator(log restql.Logger, cfg *conf.Config, pm plugins.Lifecycle) *Decorator {
	cmEnabled := cfg.HTTP.Server.Middlewares.RequestCancellation.Enabled
	cmWatchingInterval := cfg.HTTP.Server.Middlewares.RequestCancellation.WatchInterval

	return &Decorator{
		log: log,
		cfg: cfg,
		pm:  pm,
		cm:  NewConnManager(log, cmEnabled, cmWatchingInterval),
	}
}

// Apply takes a base handler and fetches all middlewares enabled in configuration
// decorating the argument with each one.
func (d *Decorator) Apply(h fasthttp.RequestHandler) fasthttp.RequestHandler {
	mws := d.fetchEnabled()
	handler := h

	for i := len(mws) - 1; i >= 0; i-- {
		m := mws[i]
		d.log.Debug(fmt.Sprintf("applying middleware %T", m))
		handler = m.Apply(handler)
	}

	return handler
}

func (d *Decorator) fetchEnabled() []Middleware {
	mws := []Middleware{newRecoverer(d.log), newNativeContext(d.cm), newTransaction(d.pm)}

	mwCfg := d.cfg.HTTP.Server.Middlewares
	if mwCfg.Cache != nil {
		cache := newCache(d.log)
		mws = append(mws, cache)
	}

	if mwCfg.Compression != nil {
		mws = append(mws, newCompression())
	}

	if mwCfg.Timeout != nil {
		mws = append(mws, newTimeout(mwCfg.Timeout.Duration, d.log))
	}

	if mwCfg.RequestID != nil {
		mws = append(mws, newRequestID(mwCfg.RequestID.Header, mwCfg.RequestID.Strategy, d.log))
	}

	if mwCfg.Cors != nil {
		cors := newCors(d.log, corsOptions{
			AllowedOrigins:   mwCfg.Cors.AllowOrigin,
			AllowedMethods:   mwCfg.Cors.AllowMethods,
			AllowedHeaders:   mwCfg.Cors.AllowHeaders,
			ExposedHeaders:   mwCfg.Cors.ExposeHeaders,
			MaxAge:           mwCfg.Cors.MaxAge,
			AllowCredentials: mwCfg.Cors.AllowCredentials,
		})
		mws = append(mws, cors)
	}

	if d.cfg.HTTP.Server.Admin.Enable {
		admAuth := newAdminAuthorization(d.log, d.cfg.HTTP.Server.Admin.AuthorizationCode)
		mws = append(mws, admAuth)
	}

	return mws
}
