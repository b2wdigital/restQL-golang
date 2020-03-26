package web

import (
	"fmt"
	"github.com/b2wdigital/restQL-golang/internal/domain"
	"github.com/b2wdigital/restQL-golang/internal/eval"
	"github.com/b2wdigital/restQL-golang/internal/parser"
	"github.com/b2wdigital/restQL-golang/internal/platform/conf"
	"github.com/b2wdigital/restQL-golang/internal/platform/logger"
	"github.com/b2wdigital/restQL-golang/internal/platform/web/middleware"
	"github.com/pkg/errors"
	"github.com/valyala/fasthttp"
	"net/http"
	"strconv"
)

var (
	ErrInvalidNamespace    = errors.New("invalid namespace")
	ErrInvalidQueryId      = errors.New("invalid query id")
	ErrInvalidRevision     = errors.New("invalid revision")
	ErrInvalidRevisionType = errors.New("invalid revision : must be an integer")
	ErrInvalidTenant       = errors.New("invalid tenant : no value provided")
)

type RestQl struct {
	config    *conf.Config
	log       *logger.Logger
	evaluator eval.Evaluator
}

func NewRestQl(l *logger.Logger, cfg *conf.Config, e eval.Evaluator) RestQl {
	return RestQl{config: cfg, log: l, evaluator: e}
}

func (r RestQl) ValidateQuery(ctx *fasthttp.RequestCtx) error {
	queryTxt := string(ctx.PostBody())
	_, err := parser.Parse(queryTxt)
	if err != nil {
		r.log.Error("an error occurred when parsing query", err)

		e := &Error{
			Err:    errors.Wrap(err, "invalid query"),
			Status: http.StatusUnprocessableEntity,
		}

		return RespondError(ctx, e)
	}

	return Respond(ctx, nil, http.StatusOK)
}

func (r RestQl) RunSavedQuery(ctx *fasthttp.RequestCtx) error {
	options, err := r.makeQueryOptions(ctx)
	if err != nil {
		return RespondError(ctx, NewRequestError(err, http.StatusUnprocessableEntity))
	}

	input := r.makeQueryInput(ctx)
	context := middleware.GetNativeContext(ctx)

	result, err := r.evaluator.SavedQuery(context, options, input)
	if err != nil {
		r.log.Debug("failed to evaluated saved query", "error", err)

		switch err := err.(type) {
		case eval.ValidationError:
			return RespondError(ctx, NewRequestError(err, http.StatusUnprocessableEntity))
		case eval.NotFoundError:
			return RespondError(ctx, NewRequestError(err, http.StatusNotFound))
		case eval.ParserError:
			return RespondError(ctx, NewRequestError(err, http.StatusInternalServerError))
		case eval.TimeoutError:
			return RespondError(ctx, NewRequestError(err, http.StatusOK))
		default:
			return RespondError(ctx, err)
		}
	}

	response := MakeQueryResponse(result)
	statusCode := CalculateStatusCode(result)
	return Respond(ctx, response, statusCode)
}

func (r RestQl) makeQueryOptions(ctx *fasthttp.RequestCtx) (domain.QueryOptions, error) {
	namespace, err := pathParamString(ctx, "namespace")
	if err != nil {
		r.log.Error("failed to load namespace path param", err)
		return domain.QueryOptions{}, err
	}

	queryId, err := pathParamString(ctx, "queryId")
	if err != nil {
		r.log.Error("failed to load query id path param", err)
		return domain.QueryOptions{}, err
	}

	revisionStr, err := pathParamString(ctx, "revision")
	if err != nil {
		r.log.Error("failed to load revision path param", err)
		return domain.QueryOptions{}, err
	}

	revision, err := strconv.Atoi(revisionStr)
	if err != nil {
		r.log.Debug("failed to convert revision to integer")
		return domain.QueryOptions{}, ErrInvalidRevisionType
	}

	var tenant string

	envTenant := r.config.Tenant
	if envTenant != "" {
		tenant = envTenant
	} else {
		tenant = string(ctx.QueryArgs().Peek("tenant"))
	}

	if tenant == "" {
		return domain.QueryOptions{}, ErrInvalidTenant
	}

	qo := domain.QueryOptions{
		Namespace: namespace,
		Id:        queryId,
		Revision:  revision,
		Tenant:    tenant,
	}

	return qo, nil
}

func (r RestQl) makeQueryInput(ctx *fasthttp.RequestCtx) domain.QueryInput {
	params := make(map[string]interface{})
	ctx.Request.URI().QueryArgs().VisitAll(func(keyByte, valueByte []byte) {
		key := string(keyByte)
		value := string(valueByte)

		if currentValue, ok := params[key]; ok {
			var newValue []interface{}

			switch currentValue := currentValue.(type) {
			case []interface{}:
				newValue = append(currentValue, value)
			default:
				newValue = []interface{}{currentValue, value}
			}

			params[key] = newValue
		} else {
			params[key] = value
		}

	})

	headers := make(map[string]string)
	ctx.Request.Header.VisitAll(func(key, value []byte) {
		headers[string(key)] = string(value)
	})

	return domain.QueryInput{
		Params:  params,
		Headers: headers,
	}
}

var paramNameToError = map[string]error{
	"namespace": ErrInvalidNamespace,
	"query":     ErrInvalidQueryId,
	"revision":  ErrInvalidRevision,
}

func pathParamString(ctx *fasthttp.RequestCtx, name string) (string, error) {
	param, found := ctx.UserValue(name).(string)
	if !found {
		e, ok := paramNameToError[name]
		if !ok {
			e = errors.New(fmt.Sprintf("path param not found : %s", name))
		}

		return "", &Error{
			Err:    e,
			Status: http.StatusUnprocessableEntity,
		}
	}

	return param, nil
}