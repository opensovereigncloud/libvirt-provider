// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package apinetwatcher

import (
	"fmt"
	"sync"
)

type Event[T any] struct {
	Type   string
	Object T
}

type Handler[T any] interface {
	Handle(Event[T])
}

type HandlerFunc[T any] func(Event[T])

func (f HandlerFunc[T]) Handle(evt Event[T]) {
	f(evt)
}

type EventEmitter[T any] interface {
	AddHandler(string, Handler[T]) error
	Fire(Event[T])
}

type emitter[T any] struct {
	mu       sync.RWMutex
	handlers map[string]Handler[T]
}

func NewEventEmitter[T any]() EventEmitter[T] {
	return &emitter[T]{
		handlers: make(map[string]Handler[T]),
	}
}

func (e *emitter[T]) AddHandler(name string, h Handler[T]) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if _, exists := e.handlers[name]; exists {
		return fmt.Errorf("handler with name %q already exists", name)
	}

	e.handlers[name] = h
	return nil
}

func (e *emitter[T]) Fire(evt Event[T]) {
	e.mu.RLock()

	// The map is copied under RLock to avoid potential race conditions if handlers are added concurrently.
	handlers := make([]Handler[T], 0, len(e.handlers))
	for _, h := range e.handlers {
		handlers = append(handlers, h)
	}

	e.mu.RUnlock()

	for _, h := range handlers {
		go h.Handle(evt)
	}
}
