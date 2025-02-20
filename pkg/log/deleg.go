/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package log

import (
	"sync"

	"github.com/go-logr/logr"
)

// loggerPromise knows how to populate a concrete logr.Logger
// with options, given an actual base logger later on down the line.
type loggerPromise struct {
	logger        *delegatingLogSink
	childPromises []*loggerPromise
	promisesLock  sync.Mutex

	name *string
	tags []interface{}
}

func (p *loggerPromise) WithName(l *delegatingLogSink, name string) *loggerPromise {
	res := &loggerPromise{
		logger:       l,
		name:         &name,
		promisesLock: sync.Mutex{},
	}

	p.promisesLock.Lock()
	defer p.promisesLock.Unlock()
	p.childPromises = append(p.childPromises, res)
	return res
}

// WithValues provides a new Logger with the tags appended.
func (p *loggerPromise) WithValues(l *delegatingLogSink, tags ...interface{}) *loggerPromise {
	res := &loggerPromise{
		logger:       l,
		tags:         tags,
		promisesLock: sync.Mutex{},
	}

	p.promisesLock.Lock()
	defer p.promisesLock.Unlock()
	p.childPromises = append(p.childPromises, res)
	return res
}

// Fulfill instantiates the Logger with the provided logSink.
func (p *loggerPromise) Fulfill(parentLogSink logr.LogSink) {
	sink := parentLogSink

	if p.name != nil {
		sink = sink.WithName(*p.name)
	}

	if p.tags != nil {
		sink = sink.WithValues(p.tags...)
	}

	p.logger.lock.Lock()
	p.logger.logSink = sink
	p.logger.promise = nil
	p.logger.lock.Unlock()

	for _, childPromise := range p.childPromises {
		childPromise.Fulfill(sink)
	}
}

// delegatingLogSink is a logr.LogSink that delegates to another logr.LogSink.
// If the underlying promise is not nil, it registers calls to sub-loggers with
// the logging factory to be populated later, and returns a new delegating
// logSink.  It expects to have *some* logr.Logger set at all times (generally
// a no-op logSink before the promises are fulfilled).
type delegatingLogSink struct {
	lock    sync.RWMutex
	logSink logr.LogSink
	promise *loggerPromise
	info    logr.RuntimeInfo
}

// Init implements logr.LogSink.
func (l *delegatingLogSink) Init(info logr.RuntimeInfo) {
	l.lock.Lock()
	defer l.lock.Unlock()
	l.info = info
}

// Enabled tests whether this Logger is enabled.  For example, commandline
// flags might be used to set the logging verbosity and disable some info
// logs.
func (l *delegatingLogSink) Enabled(v int) bool {
	l.lock.RLock()
	defer l.lock.RUnlock()
	return l.logSink.Enabled(v)
}

// Info logs a non-error message with the given key/value pairs as context.
//
// The msg argument should be used to add some constant description to
// the log line.  The key/value pairs can then be used to add additional
// variable information.  The key/value pairs should alternate string
// keys and arbitrary values.
func (l *delegatingLogSink) Info(level int, msg string, keysAndValues ...interface{}) {
	l.lock.RLock()
	defer l.lock.RUnlock()
	l.logSink.Info(level, msg, keysAndValues...)
}

// Error logs an error, with the given message and key/value pairs as context.
// It functions similarly to calling Info with the "error" named value, but may
// have unique behavior, and should be preferred for logging errors (see the
// package documentations for more information).
//
// The msg field should be used to add context to any underlying error,
// while the err field should be used to attach the actual error that
// triggered this log line, if present.
func (l *delegatingLogSink) Error(err error, msg string, keysAndValues ...interface{}) {
	l.lock.RLock()
	defer l.lock.RUnlock()
	l.logSink.Error(err, msg, keysAndValues...)
}

// WithName provides a new Logger with the name appended.
func (l *delegatingLogSink) WithName(name string) logr.LogSink {
	l.lock.RLock()
	defer l.lock.RUnlock()

	if l.promise == nil {
		return l.logSink.WithName(name)
	}

	res := &delegatingLogSink{logSink: l.logSink}
	promise := l.promise.WithName(res, name)
	res.promise = promise

	return res
}

// WithValues provides a new Logger with the tags appended.
func (l *delegatingLogSink) WithValues(tags ...interface{}) logr.LogSink {
	l.lock.RLock()
	defer l.lock.RUnlock()

	if l.promise == nil {
		return l.logSink.WithValues(tags...)
	}

	res := &delegatingLogSink{logSink: l.logSink}
	promise := l.promise.WithValues(res, tags...)
	res.promise = promise

	return res
}

// Fulfill switches the logSink over to use the actual logSink
// provided, instead of the temporary initial one, if this method
// has not been previously called.
func (l *delegatingLogSink) Fulfill(actual logr.LogSink) {
	if l.promise != nil {
		l.promise.Fulfill(actual)
	}
}

// NewDelegatingLogger constructs a new delegatingLogSink which uses
// the given logSink before it's promise is fulfilled.
func NewDelegatingLogger(logSink logr.LogSink) logr.Logger {
	l := &delegatingLogSink{
		logSink: logSink,
		promise: &loggerPromise{promisesLock: sync.Mutex{}},
	}
	l.promise.logger = l
	return logr.New(l)
}

type canFulfill interface {
	Fulfill(actual logr.LogSink)
}
