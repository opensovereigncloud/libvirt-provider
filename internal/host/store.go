// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package host

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/ironcore-dev/libvirt-provider/api"
	"github.com/ironcore-dev/libvirt-provider/internal/store"
	utilssync "github.com/ironcore-dev/libvirt-provider/internal/sync"
	"github.com/ironcore-dev/libvirt-provider/internal/utils"
	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/util/sets"
)

const perm = 0777
const suffixSwpExtension = ".swp"

type Options[E api.Object] struct {
	//TODO
	Dir            string
	NewFunc        func() E
	CreateStrategy CreateStrategy[E]
}

func NewStore[E api.Object](opts Options[E]) (*Store[E], error) {
	if opts.NewFunc == nil {
		return nil, fmt.Errorf("must specify opts.NewFunc")
	}

	if err := os.MkdirAll(opts.Dir, perm); err != nil {
		return nil, fmt.Errorf("error creating store directory: %w", err)
	}

	return &Store[E]{
		dir: opts.Dir,

		idMu: utilssync.NewMutexMap[string](),

		newFunc:        opts.NewFunc,
		createStrategy: opts.CreateStrategy,

		watches: sets.New[*watch[E]](),
	}, nil
}

type Store[E api.Object] struct {
	dir string

	idMu *utilssync.MutexMap[string]

	newFunc        func() E
	createStrategy CreateStrategy[E]

	watchesMu sync.RWMutex
	watches   sets.Set[*watch[E]]
}

type CreateStrategy[E api.Object] interface {
	PrepareForCreate(obj E)
}

func (s *Store[E]) Create(_ context.Context, obj E) (E, error) {
	s.idMu.Lock(obj.GetID())
	defer s.idMu.Unlock(obj.GetID())

	_, err := s.get(obj.GetID())
	switch {
	case err == nil:
		return utils.Zero[E](), fmt.Errorf("object with id %q %w", obj.GetID(), store.ErrAlreadyExists)
	case errors.Is(err, store.ErrNotFound):
	default:
		return utils.Zero[E](), fmt.Errorf("failed to get object with id %q %w", obj.GetID(), err)
	}

	if s.createStrategy != nil {
		s.createStrategy.PrepareForCreate(obj)
	}

	obj.SetCreatedAt(time.Now())
	obj.IncrementResourceVersion()

	obj, err = s.set(obj)
	if err != nil {
		return utils.Zero[E](), err
	}

	s.enqueue(store.WatchEvent[E]{
		Type:   store.WatchEventTypeCreated,
		Object: obj,
	})

	return obj, nil
}

func (s *Store[E]) Get(_ context.Context, id string) (E, error) {
	s.idMu.Lock(id)
	defer s.idMu.Unlock(id)

	object, err := s.get(id)
	if err != nil {
		return utils.Zero[E](), fmt.Errorf("failed to read object: %w", err)
	}

	return object, nil
}

func (s *Store[E]) Update(_ context.Context, obj E) (E, error) {
	s.idMu.Lock(obj.GetID())
	defer s.idMu.Unlock(obj.GetID())

	oldObj, err := s.get(obj.GetID())
	if err != nil {
		return utils.Zero[E](), err
	}

	if obj.GetDeletedAt() != nil && len(obj.GetFinalizers()) == 0 {
		if err := s.delete(obj.GetID()); err != nil {
			return utils.Zero[E](), fmt.Errorf("failed to delete object metadata: %w", err)
		}
		return obj, nil
	}

	if oldObj.GetResourceVersion() != obj.GetResourceVersion() {
		return utils.Zero[E](), fmt.Errorf("failed to update object: %w", store.ErrResourceVersionNotLatest)
	}

	if reflect.DeepEqual(oldObj, obj) {
		return obj, nil
	}

	obj.IncrementResourceVersion()

	obj, err = s.set(obj)
	if err != nil {
		return utils.Zero[E](), err
	}

	s.enqueue(store.WatchEvent[E]{
		Type:   store.WatchEventTypeUpdated,
		Object: obj,
	})

	return obj, nil
}

func (s *Store[E]) Delete(_ context.Context, id string) error {
	s.idMu.Lock(id)
	defer s.idMu.Unlock(id)

	obj, err := s.get(id)
	if err != nil {
		return err
	}

	if len(obj.GetFinalizers()) == 0 {
		return s.delete(id)
	}

	if obj.GetDeletedAt() != nil {
		return nil
	}

	now := time.Now()
	obj.SetDeletedAt(&now)
	obj.IncrementResourceVersion()

	if _, err := s.set(obj); err != nil {
		return fmt.Errorf("failed to set object metadata: %w", err)
	}

	s.enqueue(store.WatchEvent[E]{
		Type:   store.WatchEventTypeDeleted,
		Object: obj,
	})

	return nil
}

func (s *Store[E]) List(ctx context.Context) ([]E, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("failed to list objects: %w", err)
	}

	var objs []E
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		if strings.HasSuffix(entry.Name(), suffixSwpExtension) {
			continue
		}

		object, err := s.Get(ctx, entry.Name())
		if err != nil {
			return nil, fmt.Errorf("failed to read object: %w", err)
		}

		objs = append(objs, object)
	}

	return objs, nil
}

func (s *Store[E]) Watch(_ context.Context) (store.Watch[E], error) {
	//TODO make configurable
	const bufferSize = 10
	s.watchesMu.Lock()
	defer s.watchesMu.Unlock()

	w := &watch[E]{
		store:  s,
		events: make(chan store.WatchEvent[E], bufferSize),
	}

	s.watches.Insert(w)

	return w, nil
}

func (s *Store[E]) CleanupSwapFiles() []error {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return []error{fmt.Errorf("cleanup: failed to list objects: %w", err)}
	}

	errs := []error{}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), suffixSwpExtension) {
			continue
		}

		if entry.Type().IsRegular() {
			err = os.Remove(filepath.Join(s.dir, entry.Name()))
			if err != nil {
				errs = append(errs, err)
			}
		}
	}

	return errs
}

func (s *Store[E]) get(id string) (E, error) {
	fd, err := os.Open(filepath.Join(s.dir, id))
	if err != nil {
		if !os.IsNotExist(err) {
			return utils.Zero[E](), fmt.Errorf("failed to read file: %w", err)
		}

		return utils.Zero[E](), fmt.Errorf("object with id %q %w", id, store.ErrNotFound)
	}

	defer func() {
		// Todo: improve logging of error
		_ = fd.Close()
	}()

	obj := s.newFunc()
	decoder := yaml.NewDecoder(fd)
	err = decoder.Decode(obj)
	if err != nil {
		return utils.Zero[E](), fmt.Errorf("failed to unmarshal object from file %s: %w", id, err)
	}

	return obj, err
}

func (s *Store[E]) set(obj E) (result E, err error) {
	filePath := filepath.Join(s.dir, obj.GetID())
	swpFilePath := filePath + suffixSwpExtension
	result = utils.Zero[E]()

	fd, err := os.OpenFile(swpFilePath, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return
	}

	defer func() {
		locErr := fd.Close()
		if err == nil && locErr != nil {
			err = locErr
		}
	}()

	encoder := yaml.NewEncoder(fd)
	err = encoder.Encode(obj)
	if err != nil {
		err = fmt.Errorf("failed to marshal obj: %w", err)
		return
	}

	err = fd.Sync()
	if err != nil {
		err = fmt.Errorf("failed flush data for file %s: %w", swpFilePath, err)
		return
	}

	err = os.Rename(swpFilePath, filePath)
	if err != nil {
		return
	}

	result = obj
	return
}

func (s *Store[E]) delete(id string) error {
	if err := os.Remove(filepath.Join(s.dir, id)); err != nil {
		return fmt.Errorf("failed to delete object from store: %w", err)
	}

	return nil
}

func (s *Store[E]) watchHandlers() []*watch[E] {
	s.watchesMu.RLock()
	defer s.watchesMu.RUnlock()

	return s.watches.UnsortedList()
}

func (s *Store[E]) enqueue(evt store.WatchEvent[E]) {
	for _, handler := range s.watchHandlers() {
		select {
		case handler.events <- evt:
		default:
		}
	}
}
