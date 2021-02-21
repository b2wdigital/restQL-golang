package middleware

import (
	"github.com/valyala/fasthttp"
)

type compression struct {
}

func newCompression() Middleware {
	return compression{}
}

func (t compression) Apply(h fasthttp.RequestHandler) fasthttp.RequestHandler {
	return fasthttp.CompressHandler(h)
}
