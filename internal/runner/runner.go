package runner

import (
	"context"
	"github.com/b2wdigital/restQL-golang/internal/domain"
	"github.com/b2wdigital/restQL-golang/pkg/restql"
	"github.com/pkg/errors"
	"sync"
	"time"
)

var requestChannelPool = sync.Pool{
	New: func() interface{} {
		return make(chan request, 10)
	},
}

var resultChannelPool = sync.Pool{
	New: func() interface{} {
		return make(chan result, 10)
	},
}

var outputChannelPool = sync.Pool{
	New: func() interface{} {
		return make(chan domain.Resources)
	},
}

var errorChannelPool = sync.Pool{
	New: func() interface{} {
		return make(chan error)
	},
}

var ErrQueryTimedOut = errors.New("query timed out")

type Runner struct {
	log                restql.Logger
	executor           Executor
	globalQueryTimeout time.Duration
}

func NewRunner(log restql.Logger, executor Executor, globalQueryTimeout time.Duration) Runner {
	return Runner{
		log:                log,
		executor:           executor,
		globalQueryTimeout: globalQueryTimeout,
	}
}

func (r Runner) ExecuteQuery(ctx context.Context, query domain.Query, queryCtx domain.QueryContext) (domain.Resources, error) {
	var cancel context.CancelFunc
	queryTimeout, ok := r.parseQueryTimeout(query)
	if ok {
		ctx, cancel = context.WithTimeout(ctx, queryTimeout)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}
	defer cancel()

	resources, err := r.initializeResources(query, queryCtx)
	if err != nil {
		return nil, err
	}

	state := NewState(resources)

	requestCh := requestChannelPool.Get().(chan request)
	outputCh := outputChannelPool.Get().(chan domain.Resources)
	errorCh := errorChannelPool.Get().(chan error)
	resultCh := resultChannelPool.Get().(chan result)

	defer func() {
		requestChannelPool.Put(requestCh)
		outputChannelPool.Put(outputCh)
		errorChannelPool.Put(errorCh)
		resultChannelPool.Put(resultCh)
	}()

	stateWorker := &stateWorker{
		log:       r.log,
		requestCh: requestCh,
		resultCh:  resultCh,
		outputCh:  outputCh,
		state:     state,
		ctx:       ctx,
	}

	requestWorker := &requestWorker{
		requestCh: requestCh,
		resultCh:  resultCh,
		errorCh:   errorCh,
		executor:  r.executor,
		queryCtx:  queryCtx,
		ctx:       ctx,
	}

	go stateWorker.Run()
	go requestWorker.Run()

	select {
	case output := <-outputCh:
		return output, nil
	case err := <-errorCh:
		r.log.Debug("an error occurred when running the query", "error", err)
		return nil, err
	case <-ctx.Done():
		r.log.Debug("query timed out")
		return nil, ErrQueryTimedOut
	}
}

func (r Runner) parseQueryTimeout(query domain.Query) (time.Duration, bool) {
	timeout, found := query.Use["timeout"]
	if !found {
		return r.globalQueryTimeout, false
	}

	duration, ok := timeout.(int)
	if !ok {
		return r.globalQueryTimeout, false
	}

	return time.Millisecond * time.Duration(duration), true
}

func (r Runner) initializeResources(query domain.Query, queryCtx domain.QueryContext) (domain.Resources, error) {
	resources := domain.NewResources(query.Statements)

	resources = ResolveVariables(resources, queryCtx.Input)
	err := ValidateChainedValues(resources)
	if err != nil {
		return nil, err
	}

	resources = ApplyModifiers(resources, query.Use)
	resources = ApplyEncoders(resources, r.log)
	resources = MultiplexStatements(resources)

	return resources, nil
}

type request struct {
	ResourceIdentifier domain.ResourceId
	Statement          interface{}
}

type result struct {
	ResourceIdentifier domain.ResourceId
	Response           interface{}
}

type stateWorker struct {
	log       restql.Logger
	requestCh chan request
	resultCh  chan result
	outputCh  chan domain.Resources
	state     *State
	ctx       context.Context
}

func (sw *stateWorker) Run() {
	for !sw.state.HasFinished() {
		availableResources := sw.state.Available()
		for resourceId := range availableResources {
			sw.state.SetAsRequest(resourceId)
		}

		availableResources = ResolveChainedValues(availableResources, sw.state.Done())
		availableResources = ApplyEncoders(availableResources, sw.log)
		availableResources = MultiplexStatements(availableResources)
		availableResources = UnwrapNoMultiplex(availableResources)

		for resourceId, stmt := range availableResources {
			resourceId, stmt := resourceId, stmt
			go func() {
				select {
				case sw.requestCh <- request{ResourceIdentifier: resourceId, Statement: stmt}:
				case <-sw.ctx.Done():
				}
			}()
		}

		select {
		case result := <-sw.resultCh:
			sw.state.UpdateDone(result.ResourceIdentifier, result.Response)
		case <-sw.ctx.Done():
			return
		}
	}

	sw.outputCh <- sw.state.Done()
}

type requestWorker struct {
	requestCh chan request
	resultCh  chan result
	errorCh   chan error
	executor  Executor
	queryCtx  domain.QueryContext
	ctx       context.Context
}

func (rw *requestWorker) Run() {
	for {
		select {
		case req := <-rw.requestCh:
			resourceId := req.ResourceIdentifier
			statement := req.Statement

			switch statement := statement.(type) {
			case domain.Statement:
				go func() {
					response, err := rw.executor.DoStatement(rw.ctx, statement, rw.queryCtx)
					if err != nil {
						select {
						case rw.errorCh <- err:
						case <-rw.ctx.Done():
							break
						}
					}
					writeResult(rw.ctx, rw.resultCh, result{ResourceIdentifier: resourceId, Response: response})
				}()
			case []interface{}:
				go func() {
					responses, err := rw.executor.DoMultiplexedStatement(rw.ctx, statement, rw.queryCtx)
					if err != nil {
						select {
						case rw.errorCh <- err:
						case <-rw.ctx.Done():
							break
						}
					}
					writeResult(rw.ctx, rw.resultCh, result{ResourceIdentifier: resourceId, Response: responses})
				}()
			}
		case <-rw.ctx.Done():
			return
		}
	}
}

func writeResult(ctx context.Context, out chan result, r result) {
	select {
	case out <- r:
	case <-ctx.Done():
	}
}
