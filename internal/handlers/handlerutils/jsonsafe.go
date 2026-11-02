package handlerutils

import (
	"reflect"
	"time"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

var timeType = reflect.TypeOf(time.Time{})

// jsonSafeDepthLimit stops the walk on pathological nesting. json.Marshal has already failed on
// such a payload, so giving up just keeps the 500 it would have had.
const jsonSafeDepthLimit = 64

// withJSONSafeTimes returns a copy of payload in which every time.Time that encoding/json cannot
// encode is clamped (models.JSONSafeTime), and whether anything was clamped. The payload itself is
// never modified: it may be shared with a cache or with the caller. Only the values along the path
// to a clamped time are copied.
//
// It runs only after json.Marshal has failed, so a normal response pays nothing for it.
func withJSONSafeTimes(payload any) (any, bool) {
	v, changed := jsonSafeCopy(reflect.ValueOf(payload), 0)
	if !changed {
		return payload, false
	}
	return v.Interface(), true
}

func jsonSafeCopy(v reflect.Value, depth int) (reflect.Value, bool) {
	if !v.IsValid() || depth > jsonSafeDepthLimit {
		return v, false
	}
	switch v.Kind() {
	case reflect.Struct:
		if v.Type() == timeType {
			t := v.Interface().(time.Time)
			if safe := models.JSONSafeTime(t); !safe.Equal(t) {
				return reflect.ValueOf(safe), true
			}
			return v, false
		}
		var out reflect.Value
		for i := 0; i < v.NumField(); i++ {
			if !v.Type().Field(i).IsExported() {
				continue
			}
			field, changed := jsonSafeCopy(v.Field(i), depth+1)
			if !changed {
				continue
			}
			if !out.IsValid() {
				out = reflect.New(v.Type()).Elem()
				out.Set(v)
			}
			out.Field(i).Set(field)
		}
		if !out.IsValid() {
			return v, false
		}
		return out, true
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return v, false
		}
		elem, changed := jsonSafeCopy(v.Elem(), depth+1)
		if !changed {
			return v, false
		}
		if v.Kind() == reflect.Pointer {
			p := reflect.New(elem.Type())
			p.Elem().Set(elem)
			return p, true
		}
		out := reflect.New(v.Type()).Elem()
		out.Set(elem)
		return out, true
	case reflect.Slice, reflect.Array:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return v, false // []byte and friends encode as base64; no times inside
		}
		var out reflect.Value
		for i := 0; i < v.Len(); i++ {
			elem, changed := jsonSafeCopy(v.Index(i), depth+1)
			if !changed {
				continue
			}
			if !out.IsValid() {
				if v.Kind() == reflect.Slice {
					out = reflect.MakeSlice(v.Type(), v.Len(), v.Len())
					reflect.Copy(out, v)
				} else {
					out = reflect.New(v.Type()).Elem()
					out.Set(v)
				}
			}
			out.Index(i).Set(elem)
		}
		if !out.IsValid() {
			return v, false
		}
		return out, true
	case reflect.Map:
		var out reflect.Value
		iter := v.MapRange()
		for iter.Next() {
			elem, changed := jsonSafeCopy(iter.Value(), depth+1)
			if !changed {
				continue
			}
			if !out.IsValid() {
				out = reflect.MakeMapWithSize(v.Type(), v.Len())
				copyIter := v.MapRange()
				for copyIter.Next() {
					out.SetMapIndex(copyIter.Key(), copyIter.Value())
				}
			}
			out.SetMapIndex(iter.Key(), elem)
		}
		if !out.IsValid() {
			return v, false
		}
		return out, true
	}
	return v, false
}
