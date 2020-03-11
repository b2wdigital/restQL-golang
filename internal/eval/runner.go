package eval

import (
	"context"
	"fmt"
	"github.com/b2wdigital/restQL-golang/internal/domain"
	"github.com/b2wdigital/restQL-golang/internal/eval/runner"
	"strings"
)

type Runner struct {
	config Configuration
	log    Logger
	client runner.HttpClient
}

func New(config Configuration, log Logger) Runner {
	c := runner.NewHttpClient()

	return Runner{
		config: config,
		log:    log,
		client: c,
	}
}

func (r Runner) ExecuteQuery(ctx context.Context, query domain.Query, mappings map[string]string) interface{} {
	responses := make([]interface{}, len(query.Statements))

	for i, statement := range query.Statements {
		resource := mappings[statement.Resource]
		resource = strings.Replace(resource, ":id", fmt.Sprintf("%v", statement.With["id"]), 1)

		r.log.Debug("resource url done", "url", resource)

		queryArgs := make(map[string]string)
		for key, value := range statement.With {
			queryArgs[key] = fmt.Sprintf("%v", value)
		}

		headers := make(map[string]string)
		for key, value := range statement.Headers {
			headers[key] = fmt.Sprintf("%v", value)
		}

		req := runner.Request{
			Host:    resource,
			Query:   queryArgs,
			Body:    nil,
			Headers: headers,
		}

		response, err := r.client.Do(ctx, req)
		if err != nil {
			r.log.Debug("request failed", "error", err)
			return nil
		}

		responses[i] = response
	}

	return responses
}