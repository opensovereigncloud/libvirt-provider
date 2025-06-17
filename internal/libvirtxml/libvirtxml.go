// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package libvirtxml

import (
	"encoding/binary"
	"encoding/xml"
	"errors"
	"fmt"
	"hash"
	"hash/fnv"
	"math"
	"os"
	"reflect"
	"slices"

	"github.com/ironcore-dev/libvirt-provider/internal/osutils"
	"libvirt.org/go/libvirtxml"
)

var (
	ErrOpenOverrideTemplate   = errors.New("failed to open override domain template XML")
	ErrDecodeOverrideTemplate = errors.New("failed to decode override domain template XML")
)

// LoadOverrideDomainXML loads the override domain XML and returns nil if path is empty.
func LoadOverrideDomainXML(path string) (*libvirtxml.Domain, error) {
	if path == "" {
		return nil, nil
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrOpenOverrideTemplate, err)
	}

	defer osutils.CloseWithErrorLogging(file, fmt.Sprintf("failed to close file %s", path), nil)

	var domain libvirtxml.Domain
	decoder := xml.NewDecoder(file)
	err = decoder.Decode(&domain)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrDecodeOverrideTemplate, err)
	}

	return &domain, nil
}

// MergeDomains merges overridedomain and generatedDomain, prioritizing overridedomain values.
// The merge approach is better than direct application (`finalXML = overrideXML`) because:
// 1. Prevents Data Loss – Retains fields from generated XML that are missing in override XML.
// 2. Preserves Nested Structures – Updates only specific fields instead of replacing entire structs.
// 3. Handles Slices Correctly – Prevents overwriting lists and ensures unique entries are retained.
// 4. Avoids Overwriting with Nil – Ensures valid data isn’t lost due to nil pointers in override XML.
// 5. Flexible & Extendable – Supports partial updates and future merging rules, unlike rigid direct application.
func MergeDomains(overridedomain, generateddomain *libvirtxml.Domain) *libvirtxml.Domain {
	if overridedomain == nil {
		return generateddomain
	}
	if generateddomain == nil {
		return overridedomain
	}

	finalDomain := &libvirtxml.Domain{}
	mergeStructs(reflect.ValueOf(finalDomain).Elem(), reflect.ValueOf(overridedomain).Elem(), reflect.ValueOf(generateddomain).Elem())
	return finalDomain
}

// mergeStructs merges struct fields dynamically.
func mergeStructs(final, override, generated reflect.Value) {
	for i := range final.NumField() {
		overrideField := override.Field(i)
		generatedField := generated.Field(i)
		finalField := final.Field(i)

		if !overrideField.IsValid() || !generatedField.IsValid() || !finalField.CanSet() {
			continue
		}

		switch finalField.Kind() {
		case reflect.Ptr:
			if overrideField.IsNil() && !generatedField.IsNil() {
				finalField.Set(generatedField)
			} else if !overrideField.IsNil() && generatedField.IsNil() {
				finalField.Set(overrideField)
			} else {
				if overrideField.Elem().Kind() == reflect.Struct {
					if finalField.IsNil() {
						finalField.Set(reflect.New(overrideField.Type().Elem()))
					}
					mergeStructs(finalField.Elem(), overrideField.Elem(), generatedField.Elem())
				} else {
					finalField.Set(overrideField)
				}
			}

		case reflect.Slice:
			if overrideField.IsNil() {
				finalField.Set(generatedField)
			} else if finalField.Type().Elem().Kind() == reflect.Struct {
				finalField.Set(mergeStructSlices(overrideField, generatedField))
			} else {
				finalField.Set(mergeSlices(overrideField, generatedField))
			}

		case reflect.Struct:
			mergeStructs(finalField, overrideField, generatedField)

		default:
			if !overrideField.IsZero() {
				finalField.Set(overrideField)
			} else {
				finalField.Set(generatedField)
			}
		}
	}
}

// mergeSlices merges slices dynamically, keeping only unique values.
// Note: This implementation does not properly handle pointers to slices (*[]T),
// but since there is no such case in the libvirtxml Domain struct at present,
// this is not currently an issue. Future modifications may need to address this.
func mergeSlices(override, generated reflect.Value) reflect.Value {
	merged := reflect.MakeSlice(override.Type(), 0, override.Len()+generated.Len())
	unique := make(map[any]struct{})

	for i := range override.Len() {
		v := override.Index(i)
		if _, exists := unique[v.Interface()]; !exists {
			unique[v.Interface()] = struct{}{}
			merged = reflect.Append(merged, v)
		}
	}

	for i := range generated.Len() {
		v := generated.Index(i)
		if _, exists := unique[v.Interface()]; !exists {
			unique[v.Interface()] = struct{}{}
			merged = reflect.Append(merged, v)
		}
	}

	return merged
}

// mergeStructSlices merges slices of struct dynamically, keeping only unique values.
func mergeStructSlices(override, generated reflect.Value) reflect.Value {
	merged := reflect.MakeSlice(override.Type(), 0, override.Len()+generated.Len())
	uniqueStructs := make(map[uint64]struct{})

	// helper function to add unique structs to the merged slice.
	addUnique := func(item reflect.Value) {
		if !item.IsValid() {
			return
		}
		key := generateHashKey(item)
		if _, exists := uniqueStructs[key]; !exists {
			uniqueStructs[key] = struct{}{}
			merged = reflect.Append(merged, item)
		}
	}

	for i := range override.Len() {
		addUnique(override.Index(i))
	}
	for i := range generated.Len() {
		addUnique(generated.Index(i))
	}
	return merged
}

// generateHashKey generates a unique hash for a struct using FNV hashing.
func generateHashKey(item reflect.Value) uint64 {
	h := fnv.New64a()

	// helper function to recursively hash struct fields.
	var hashFields func(reflect.Value)
	hashFields = func(v reflect.Value) {
		if !v.IsValid() {
			return
		}
		if v.Kind() == reflect.Ptr {
			if v.IsNil() {
				return
			}
			v = v.Elem()
		}
		switch v.Kind() {
		case reflect.Struct:
			for i := range v.NumField() {
				field := v.Field(i)
				// skip unexported fields, as attempting to access an unexported field can cause panic.
				if field.CanInterface() {
					hashFields(field)
				}
			}
		case reflect.Slice, reflect.Array:
			for i := range v.Len() {
				hashFields(v.Index(i))
			}
		case reflect.Map:
			keys := v.MapKeys()
			// sort map keys before processing to ensure consistent hashing.
			slices.SortFunc(keys, func(i, j reflect.Value) int {
				return int(generateHashKey(i) - generateHashKey(j))
			})
			for _, key := range keys {
				hashFields(key)
				hashFields(v.MapIndex(key))
			}
		default:
			writeHash(h, v)
		}
	}
	hashFields(item)
	return h.Sum64()
}

// writeHash performs type-specific hashing for primitive types
func writeHash(h hash.Hash64, v reflect.Value) {
	switch v.Kind() {
	case reflect.String:
		h.Write([]byte(v.String()))

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		b := make([]byte, 8)
		binary.LittleEndian.PutUint64(b, uint64(v.Int()))
		h.Write(b)

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		b := make([]byte, 8)
		binary.LittleEndian.PutUint64(b, v.Uint())
		h.Write(b)

	case reflect.Float32, reflect.Float64:
		b := make([]byte, 8)
		binary.LittleEndian.PutUint64(b, math.Float64bits(v.Float()))
		h.Write(b)

	case reflect.Bool:
		var b byte
		if v.Bool() {
			b = 1
		}
		h.Write([]byte{b})

	default:
		// fallback for other types
		h.Write(fmt.Appendf(nil, "%v", v.Interface()))
	}
}
