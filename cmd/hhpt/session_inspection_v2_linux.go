//go:build linux

package main

import (
	"context"
	"errors"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/exteriorinspection"
)

type sessionConditionResult struct {
	responses []exteriorinspection.ConditionResponseV2
	err       error
}

type sessionConditionRequest struct {
	ctx    context.Context
	batch  inspectionConditionBatch
	result chan sessionConditionResult
}

// The existing console goroutine owns the complete four-line request. Arming it
// before the single prompt prevents gaps that could discard buffered responses.
func (console *sessionConsole) ReadInspectionConditions(ctx context.Context, prompt func() error) ([]exteriorinspection.ConditionResponseV2, error) {
	if nilRetestInputValue(ctx) || prompt == nil {
		return nil, errSessionInput
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	request := &sessionConditionRequest{ctx: ctx, result: make(chan sessionConditionResult, 1)}
	console.mutex.Lock()
	if console.stopped || console.failure != nil || console.request != nil ||
		console.conditionRequest != nil || console.discard != nil || console.ctx.Err() != nil {
		console.mutex.Unlock()
		return nil, errSessionInput
	}
	console.conditionPaused = false
	console.conditionRequest = request
	console.mutex.Unlock()
	if err := prompt(); err != nil {
		console.mutex.Lock()
		if console.conditionRequest == request {
			console.conditionRequest = nil
			console.conditionPaused = true
		}
		console.mutex.Unlock()
		return nil, errInspectionConditionPrompt
	}
	select {
	case result := <-request.result:
		return result.responses, result.err
	case <-ctx.Done():
		console.mutex.Lock()
		if console.conditionRequest == request {
			console.respondCondition(ctx.Err(), true)
		}
		console.mutex.Unlock()
		result := <-request.result
		return result.responses, errors.Join(result.err, ctx.Err())
	}
}

// The caller holds the existing console mutex.
func (console *sessionConsole) respondCondition(err error, incomplete bool) {
	if request := console.conditionRequest; request != nil {
		request.result <- sessionConditionResult{responses: request.batch.responses(incomplete), err: err}
		console.conditionRequest = nil
		console.conditionPaused = true
	}
}

// The caller holds the existing console mutex.
func (console *sessionConsole) consumeCondition(character byte) {
	request := console.conditionRequest
	if request == nil {
		return
	}
	if err := inspectionConditionContextStop(request.ctx); err != nil {
		console.respondCondition(err, true)
		return
	}
	before := request.batch.completed
	done, err := request.batch.consume(character)
	if err == nil {
		err = inspectionConditionContextStop(request.ctx)
	}
	if done || err != nil {
		console.respondCondition(err, !done && request.batch.completed == before)
	}
}
