package httpclient

import (
	"context"
	"fmt"
	"github.com/b2wdigital/restQL-golang/internal/domain"
	"github.com/b2wdigital/restQL-golang/internal/platform/conf"
	"github.com/b2wdigital/restQL-golang/internal/platform/logger"
	"github.com/b2wdigital/restQL-golang/internal/platform/plugins"
	"github.com/pkg/errors"
	"github.com/rs/dnscache"
	"github.com/valyala/fasthttp"
	"net"
	"time"
)

type HttpClient struct {
	client        *fasthttp.Client
	log           *logger.Logger
	pluginManager plugins.Manager
}

func New(log *logger.Logger, pm plugins.Manager, cfg *conf.Config) *HttpClient {
	clientCfg := cfg.Web.Client

	r := &dnscache.Resolver{}
	go func() {
		t := time.NewTicker(10 * time.Minute)
		defer t.Stop()
		for range t.C {
			r.Refresh(true)
		}
	}()
	var dialer = &fasthttp.TCPDialer{
		Resolver: &net.Resolver{
			PreferGo:     true,
			StrictErrors: false,
			Dial: func(ctx context.Context, network, address string) (conn net.Conn, err error) {
				host, port, err := net.SplitHostPort(address)
				if err != nil {
					return nil, err
				}
				ips, err := r.LookupHost(context.Background(), host)
				if err != nil {
					return nil, err
				}
				for _, ip := range ips {
					var dialer net.Dialer
					conn, err = dialer.Dial(network, net.JoinHostPort(ip, port))
					if err == nil {
						break
					}
				}
				return
			},
		},
	}
	c := &fasthttp.Client{
		Name:                     "restql",
		NoDefaultUserAgentHeader: false,
		ReadTimeout:              clientCfg.ReadTimeout,
		WriteTimeout:             clientCfg.WriteTimeout,
		MaxConnsPerHost:          clientCfg.MaxConnsPerHost,
		MaxIdleConnDuration:      clientCfg.MaxIdleConnDuration,
		MaxConnDuration:          clientCfg.MaxConnDuration,
		MaxConnWaitTimeout:       clientCfg.MaxConnWaitTimeout,
		Dial:                     dialer.Dial,
	}

	return &HttpClient{client: c, log: log, pluginManager: pm}
}

func (hc *HttpClient) Do(ctx context.Context, request domain.HttpRequest) (domain.HttpResponse, error) {
	req := fasthttp.AcquireRequest()
	res := fasthttp.AcquireResponse()
	defer func() {
		fasthttp.ReleaseRequest(req)
		fasthttp.ReleaseResponse(res)
	}()

	err := setupRequest(request, req)
	if err != nil {
		hc.log.Debug("failed to setup http client request", "error", err)
		return domain.HttpResponse{}, err
	}

	var response domain.HttpResponse

	requestCtx := hc.pluginManager.RunBeforeRequest(ctx, request)
	defer func() {
		hc.pluginManager.RunAfterRequest(requestCtx, request, response, err)
	}()

	timeout, cancel := context.WithTimeout(requestCtx, request.Timeout)
	defer cancel()
	duration, err := hc.executeWithContext(timeout, req, res)

	switch {
	case err == domain.ErrRequestTimeout:
		hc.log.Info("request timed out", "url", fmt.Sprintf("%s://%s%s", request.Schema, request.Host, request.Path), "method", request.Method, "duration", duration.Milliseconds())
		response = makeErrorResponse(req, duration, err)
		return response, err
	case err != nil:
		response = makeErrorResponse(req, duration, err)
		return response, errors.Wrap(err, "request execution failed")
	}

	response, err = makeResponse(req, res, duration)
	if err != nil {
		response = makeErrorResponse(req, duration, err)
		return response, err
	}

	return response, nil
}

func (hc *HttpClient) executeWithContext(ctx context.Context, req *fasthttp.Request, res *fasthttp.Response) (time.Duration, error) {
	var start time.Time

	errCh := make(chan error)
	go func() {
		start = time.Now()
		errCh <- hc.client.Do(req, res)
	}()

	select {
	case e := <-errCh:
		finish := time.Since(start)
		return finish, e
	case <-ctx.Done():
		finish := time.Since(start)
		return finish, domain.ErrRequestTimeout
	}
}
