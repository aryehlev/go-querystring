// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package query implements encoding of structs into URL query parameters.
//
// As a simple example:
//
//	type Options struct {
//		Query   string `url:"q"`
//		ShowAll bool   `url:"all"`
//		Page    int    `url:"page"`
//	}
//
//	opt := Options{ "foo", true, 2 }
//	v, _ := query.Values(opt)
//	fmt.Print(v.Encode()) // will output: "q=foo&all=true&page=2"
//
// The exact mapping between Go values and url.Values is described in the
// documentation for the Values() function.
package query

import (
	"fmt"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"
)

var timeType = reflect.TypeOf(time.Time{})

var encoderType = reflect.TypeOf(new(Encoder)).Elem()

// Encoder is an interface implemented by any type that wishes to encode
// itself into URL values in a non-standard way.
type Encoder interface {
	EncodeValues(key string, v *url.Values) error
}

// Values returns the url.Values encoding of v.
//
// Values expects to be passed a struct, and traverses it recursively using the
// following encoding rules.
//
// Each exported struct field is encoded as a URL parameter unless
//
//   - the field's tag is "-", or
//   - the field is empty and its tag specifies the "omitempty" option
//
// The empty values are false, 0, any nil pointer or interface value, any array,
// slice, map, or string of length zero, and any type (such as time.Time) that
// returns true for IsZero().
//
// The URL parameter name defaults to the struct field name but can be
// specified in the struct field's tag value.  The "url" key in the struct
// field's tag value is the key name, followed by an optional comma and
// options.  For example:
//
//	// Field is ignored by this package.
//	Field int `url:"-"`
//
//	// Field appears as URL parameter "myName".
//	Field int `url:"myName"`
//
//	// Field appears as URL parameter "myName" and the field is omitted if
//	// its value is empty.
//	Field int `url:"myName,omitempty"`
//
//	// Field appears as URL parameter "Field" (the default), but the field
//	// is skipped if empty.  Note the leading comma.
//	Field int `url:",omitempty"`
//
// For encoding individual field values, the following type-dependent rules
// apply:
//
// Boolean values default to encoding as the strings "true" or "false".
// Including the "int" option signals that the field should be encoded as the
// strings "1" or "0".
//
// time.Time values default to encoding as RFC3339 timestamps.  Including the
// "unix" option signals that the field should be encoded as a Unix time (see
// time.Unix()).  The "unixmilli" and "unixnano" options will encode the number
// of milliseconds and nanoseconds, respectively, since January 1, 1970 (see
// time.UnixNano()).  Including the "layout" struct tag (separate from the
// "url" tag) will use the value of the "layout" tag as a layout passed to
// time.Format.  For example:
//
//	// Encode a time.Time as YYYY-MM-DD
//	Field time.Time `layout:"2006-01-02"`
//
// Slice and Array values default to encoding as multiple URL values of the
// same name.  Including the "comma" option signals that the field should be
// encoded as a single comma-delimited value.  Including the "space" option
// similarly encodes the value as a single space-delimited string. Including
// the "semicolon" option will encode the value as a semicolon-delimited string.
// Including the "brackets" option signals that the multiple URL values should
// have "[]" appended to the value name. "numbered" will append a number to
// the end of each incidence of the value name, example:
// name0=value0&name1=value1, etc.  Including the "del" struct tag (separate
// from the "url" tag) will use the value of the "del" tag as the delimiter.
// For example:
//
//	// Encode a slice of bools as ints ("1" for true, "0" for false),
//	// separated by exclamation points "!".
//	Field []bool `url:",int" del:"!"`
//
// Anonymous struct fields are usually encoded as if their inner exported
// fields were fields in the outer struct, subject to the standard Go
// visibility rules.  An anonymous struct field with a name given in its URL
// tag is treated as having that name, rather than being anonymous.
//
// Non-nil pointer values are encoded as the value pointed to.
//
// Nested structs have their fields processed recursively and are encoded
// including parent fields in value names for scoping. For example,
//
//	"user[name]=acme&user[addr][postcode]=1234&user[addr][city]=SFO"
//
// All other values are encoded using their default string representation.
//
// Multiple fields that encode to the same URL parameter name will be included
// as multiple URL values of the same name.
func Values(v interface{}) (url.Values, error) {
	values := make(url.Values)

	if v == nil {
		return values, nil
	}

	val := reflect.ValueOf(v)
	for val.Kind() == reflect.Ptr {
		if val.IsNil() {
			return values, nil
		}
		val = val.Elem()
	}

	if val.Kind() != reflect.Struct {
		return nil, fmt.Errorf("query: Values() expects struct input. Got %v", val.Kind())
	}

	err := reflectValue(values, val, "")
	return values, err
}

// The encoding rules above depend only on a field's type and struct tag, never on its
// value, so they are resolved once per struct type into a structPlan and cached. Each
// Values call then walks the plan instead of re-reading and re-parsing every tag, and
// formats scalars with strconv instead of boxing them through fmt.Sprint.

// structPlan is the per-type result of resolving struct tags.
type structPlan struct {
	fields []fieldPlan
}

// fieldPlan is everything reflectValue needs to know about one struct field that does not
// depend on the field's value.
type fieldPlan struct {
	index int
	name  string // tag name, or the Go field name when the tag has none

	// embedded is set for an anonymous field with no tag name. Whether its fields are
	// promoted still depends on the value: a nil pointer to a struct is encoded as a
	// regular field named after the Go field.
	embedded bool

	// url tag options
	omitEmpty bool
	boolInt   bool
	comma     bool
	space     bool
	semicolon bool
	brackets  bool
	numbered  bool
	unix      bool
	unixMilli bool
	unixNano  bool

	del    string // "del" struct tag
	layout string // "layout" struct tag

	// info describes the field's static type. It is nil for interface-typed fields, whose
	// concrete type is only known at encode time.
	info *typeInfo
}

// typeInfo caches the type-level questions the encoder asks about a value.
type typeInfo struct {
	implementsEncoder     bool // t implements Encoder
	elemImplementsEncoder bool // t is a pointer type whose element type implements Encoder
	zeroable              bool // t has an IsZero() bool method

	// base is what remains after dereferencing every pointer level of t.
	baseKind       reflect.Kind
	baseIsTime     bool // base == time.Time
	baseUsesSprint bool // base implements fmt.Formatter, fmt.Stringer or error, so fmt decides its text
}

type zeroable interface {
	IsZero() bool
}

var (
	stringerType  = reflect.TypeOf((*fmt.Stringer)(nil)).Elem()
	errorType     = reflect.TypeOf((*error)(nil)).Elem()
	formatterType = reflect.TypeOf((*fmt.Formatter)(nil)).Elem()
	zeroableType  = reflect.TypeOf((*zeroable)(nil)).Elem()
)

var (
	plans     sync.Map // reflect.Type -> *structPlan
	typeInfos sync.Map // reflect.Type -> *typeInfo
)

// planFor returns the cached structPlan for the struct type t, building it on first use.
// Nested and embedded struct types are resolved lazily at encode time, so recursive
// types do not recurse here.
func planFor(t reflect.Type) *structPlan {
	if p, ok := plans.Load(t); ok {
		return p.(*structPlan)
	}

	p := &structPlan{}
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if sf.PkgPath != "" && !sf.Anonymous { // unexported
			continue
		}

		tag := sf.Tag.Get("url")
		if tag == "-" {
			continue
		}
		name, opts := parseTag(tag)

		f := fieldPlan{
			index:     i,
			name:      name,
			embedded:  name == "" && sf.Anonymous,
			omitEmpty: opts.Contains("omitempty"),
			boolInt:   opts.Contains("int"),
			comma:     opts.Contains("comma"),
			space:     opts.Contains("space"),
			semicolon: opts.Contains("semicolon"),
			brackets:  opts.Contains("brackets"),
			numbered:  opts.Contains("numbered"),
			unix:      opts.Contains("unix"),
			unixMilli: opts.Contains("unixmilli"),
			unixNano:  opts.Contains("unixnano"),
			del:       sf.Tag.Get("del"),
			layout:    sf.Tag.Get("layout"),
		}
		if f.name == "" {
			f.name = sf.Name
		}
		if sf.Type.Kind() != reflect.Interface {
			f.info = typeInfoFor(sf.Type)
		}

		p.fields = append(p.fields, f)
	}

	actual, _ := plans.LoadOrStore(t, p)
	return actual.(*structPlan)
}

// typeInfoFor returns the cached typeInfo for t, building it on first use.
func typeInfoFor(t reflect.Type) *typeInfo {
	if ti, ok := typeInfos.Load(t); ok {
		return ti.(*typeInfo)
	}

	ti := &typeInfo{
		implementsEncoder:     t.Implements(encoderType),
		elemImplementsEncoder: t.Kind() == reflect.Ptr && t.Elem().Implements(encoderType),
		zeroable:              t.Implements(zeroableType),
	}

	base := t
	for base.Kind() == reflect.Ptr {
		base = base.Elem()
	}
	ti.baseKind = base.Kind()
	ti.baseIsTime = base == timeType
	ti.baseUsesSprint = base.Implements(formatterType) || base.Implements(stringerType) || base.Implements(errorType)

	actual, _ := typeInfos.LoadOrStore(t, ti)
	return actual.(*typeInfo)
}

// reflectValue populates the values parameter from the struct fields in val.
// Embedded structs are followed recursively (using the rules defined in the
// Values function documentation) breadth-first.
func reflectValue(values url.Values, val reflect.Value, scope string) error {
	var embedded []reflect.Value

	plan := planFor(val.Type())
	for i := range plan.fields {
		f := &plan.fields[i]
		sv := val.Field(f.index)

		if f.embedded {
			v := reflect.Indirect(sv)
			if v.IsValid() && v.Kind() == reflect.Struct {
				// save embedded struct for later processing
				embedded = append(embedded, v)
				continue
			}
		}

		name := f.name
		if scope != "" {
			name = scope + "[" + name + "]"
		}

		if err := encodeField(values, sv, name, f); err != nil {
			return err
		}
	}

	for _, v := range embedded {
		if err := reflectValue(values, v, scope); err != nil {
			return err
		}
	}

	return nil
}

// encodeField adds the URL values for one struct field.
func encodeField(values url.Values, sv reflect.Value, name string, f *fieldPlan) error {
	info := f.info

	if f.omitEmpty && isEmpty(sv, info) {
		return nil
	}

	// unwrap interface values so the concrete type's Encoder is used
	if sv.Kind() == reflect.Interface && !sv.IsNil() {
		sv = sv.Elem()
	}
	if info == nil {
		info = typeInfoFor(sv.Type())
	}

	if info.implementsEncoder {
		// if sv is a nil pointer and the custom encoder is defined on a non-pointer
		// method receiver, set sv to the zero value of the underlying type
		if sv.Kind() == reflect.Ptr && sv.IsNil() && info.elemImplementsEncoder {
			sv = reflect.New(sv.Type().Elem())
		}

		m := sv.Interface().(Encoder)
		return m.EncodeValues(name, &values)
	}

	// recursively dereference pointers. break on nil pointers
	for sv.Kind() == reflect.Ptr {
		if sv.IsNil() {
			break
		}
		sv = sv.Elem()
	}

	if sv.Kind() == reflect.Slice || sv.Kind() == reflect.Array {
		if sv.Len() == 0 {
			// skip if slice or array is empty
			return nil
		}

		var del string
		if f.comma {
			del = ","
		} else if f.space {
			del = " "
		} else if f.semicolon {
			del = ";"
		} else if f.brackets {
			name = name + "[]"
		} else {
			del = f.del
		}

		elemInfo := typeInfoFor(sv.Type().Elem())

		if del != "" {
			s := new(strings.Builder)
			for i := 0; i < sv.Len(); i++ {
				if i > 0 {
					s.WriteString(del)
				}
				s.WriteString(formatValue(sv.Index(i), f, elemInfo))
			}
			values.Add(name, s.String())
		} else {
			for i := 0; i < sv.Len(); i++ {
				k := name
				if f.numbered {
					k = name + strconv.Itoa(i)
				}
				values.Add(k, formatValue(sv.Index(i), f, elemInfo))
			}
		}
		return nil
	}

	if sv.Type() == timeType {
		values.Add(name, formatValue(sv, f, info))
		return nil
	}

	if sv.Kind() == reflect.Struct {
		return reflectValue(values, sv, name)
	}

	values.Add(name, formatValue(sv, f, info))
	return nil
}

// formatValue returns the string representation of v. info describes v's static type,
// before any pointer dereferencing.
func formatValue(v reflect.Value, f *fieldPlan, info *typeInfo) string {
	for v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return ""
		}
		v = v.Elem()
	}

	if info.baseKind == reflect.Bool && f.boolInt {
		if v.Bool() {
			return "1"
		}
		return "0"
	}

	if info.baseIsTime {
		t := v.Interface().(time.Time)
		if f.unix {
			return strconv.FormatInt(t.Unix(), 10)
		}
		if f.unixMilli {
			return strconv.FormatInt((t.UnixNano() / 1e6), 10)
		}
		if f.unixNano {
			return strconv.FormatInt(t.UnixNano(), 10)
		}
		if f.layout != "" {
			return t.Format(f.layout)
		}
		return t.Format(time.RFC3339)
	}

	if !info.baseUsesSprint {
		// These are exactly the representations fmt.Sprint would produce for the plain kinds.
		switch info.baseKind {
		case reflect.String:
			return v.String()
		case reflect.Bool:
			return strconv.FormatBool(v.Bool())
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			return strconv.FormatInt(v.Int(), 10)
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
			return strconv.FormatUint(v.Uint(), 10)
		case reflect.Float32:
			return strconv.FormatFloat(v.Float(), 'g', -1, 32)
		case reflect.Float64:
			return strconv.FormatFloat(v.Float(), 'g', -1, 64)
		}
	}

	return fmt.Sprint(v.Interface())
}

// isEmpty checks if a value should be considered empty for the purposes of omitting
// fields with the "omitempty" option. info describes v's type, or is nil when it is
// not known statically.
func isEmpty(v reflect.Value, info *typeInfo) bool {
	switch v.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return v.Len() == 0
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Interface, reflect.Ptr:
		return v.IsNil()
	}

	if info == nil {
		info = typeInfoFor(v.Type())
	}
	if info.zeroable {
		return v.Interface().(zeroable).IsZero()
	}

	return false
}

// isEmptyValue checks if a value should be considered empty for the purposes
// of omitting fields with the "omitempty" option.
func isEmptyValue(v reflect.Value) bool {
	return isEmpty(v, nil)
}

// tagOptions is the string following a comma in a struct field's "url" tag, or
// the empty string. It does not include the leading comma.
type tagOptions []string

// parseTag splits a struct field's url tag into its name and comma-separated
// options.
func parseTag(tag string) (string, tagOptions) {
	s := strings.Split(tag, ",")
	return s[0], s[1:]
}

// Contains checks whether the tagOptions contains the specified option.
func (o tagOptions) Contains(option string) bool {
	for _, s := range o {
		if s == option {
			return true
		}
	}
	return false
}
