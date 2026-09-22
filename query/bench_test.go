// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package query

import (
	"math/rand"
	"reflect"
	"testing"
	"time"
)

// Each benchmark runs the cached-plan encoder (Values) and the upstream implementation
// (legacyValues) on the same input, so the comparison is visible in one output.

type benchSmall struct {
	Query   string `url:"q"`
	ShowAll bool   `url:"all"`
	Page    int    `url:"page"`
}

// benchWide resembles a reporting record: many scalar fields, mostly omitempty, a third set.
type benchWide struct {
	F00 string     `url:"f00,omitempty"`
	F01 string     `url:"f01,omitempty"`
	F02 string     `url:"f02,omitempty"`
	F03 string     `url:"f03,omitempty"`
	F04 string     `url:"f04,omitempty"`
	F05 string     `url:"f05,omitempty"`
	F06 string     `url:"f06,omitempty"`
	F07 string     `url:"f07,omitempty"`
	F08 string     `url:"f08,omitempty"`
	F09 string     `url:"f09,omitempty"`
	F10 int        `url:"f10,omitempty"`
	F11 int        `url:"f11,omitempty"`
	F12 int        `url:"f12,omitempty"`
	F13 int64      `url:"f13,omitempty"`
	F14 int64      `url:"f14,omitempty"`
	F15 float64    `url:"f15,omitempty"`
	F16 float64    `url:"f16,omitempty"`
	F17 float32    `url:"f17,omitempty"`
	F18 bool       `url:"f18,omitempty"`
	F19 bool       `url:"f19,omitempty"`
	F20 string     `url:"f20,omitempty"`
	F21 string     `url:"f21,omitempty"`
	F22 string     `url:"f22,omitempty"`
	F23 string     `url:"f23,omitempty"`
	F24 string     `url:"f24,omitempty"`
	F25 string     `url:"f25,omitempty"`
	F26 string     `url:"f26,omitempty"`
	F27 string     `url:"f27,omitempty"`
	F28 string     `url:"f28,omitempty"`
	F29 string     `url:"f29,omitempty"`
	F30 int        `url:"f30,omitempty"`
	F31 int        `url:"f31,omitempty"`
	F32 int        `url:"f32,omitempty"`
	F33 int64      `url:"f33,omitempty"`
	F34 int64      `url:"f34,omitempty"`
	F35 float64    `url:"f35,omitempty"`
	F36 float64    `url:"f36,omitempty"`
	F37 float32    `url:"f37,omitempty"`
	F38 bool       `url:"f38,omitempty"`
	F39 bool       `url:"f39,omitempty"`
	F40 string     `url:"f40"`
	F41 string     `url:"f41"`
	F42 string     `url:"f42"`
	F43 string     `url:"f43"`
	F44 int        `url:"f44"`
	F45 int        `url:"f45"`
	F46 *int       `url:"f46,omitempty"`
	F47 *string    `url:"f47,omitempty"`
	F48 *time.Time `url:"f48,omitempty"`
	F49 []byte     `url:"f49,omitempty"`
}

type benchNested struct {
	benchSmall
	User struct {
		Name string `url:"name"`
		Addr struct {
			Postcode string `url:"postcode"`
			City     string `url:"city"`
		} `url:"addr"`
	} `url:"user"`
	Tags  []string  `url:"tags,comma"`
	IDs   []int     `url:"ids"`
	Since time.Time `url:"since,unix"`
	Limit *int      `url:"limit,omitempty"`
}

func benchInputs() (benchSmall, benchWide, benchNested) {
	small := benchSmall{Query: "foo bar", ShowAll: true, Page: 2}

	wide := benchWide{
		F00: "a", F03: "b", F06: "c", F09: "d", F12: 3, F15: 1.5, F18: true, F21: "e", F24: "f", F27: "g",
		F30: 9, F33: 12345, F36: 0.25, F39: true, F40: "w", F41: "x", F42: "y", F43: "z", F44: 1, F45: 2,
		F49: []byte{1, 2},
	}
	n := 7
	wide.F46 = &n

	var nested benchNested
	nested.Query = "q"
	nested.Page = 3
	nested.User.Name = "acme"
	nested.User.Addr.Postcode = "1234"
	nested.User.Addr.City = "SFO"
	nested.Tags = []string{"a", "b", "c"}
	nested.IDs = []int{1, 2, 3, 4}
	nested.Since = time.Unix(1700000000, 0)
	nested.Limit = &n

	return small, wide, nested
}

func benchmarkBoth(b *testing.B, input interface{}) {
	b.Run("cached-plan", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := Values(input); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("upstream", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := legacyValues(input); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkValues_Small(b *testing.B) {
	small, _, _ := benchInputs()
	benchmarkBoth(b, small)
}

func BenchmarkValues_Wide(b *testing.B) {
	_, wide, _ := benchInputs()
	benchmarkBoth(b, wide)
}

func BenchmarkValues_Nested(b *testing.B) {
	_, _, nested := benchInputs()
	benchmarkBoth(b, nested)
}

func BenchmarkValues_Everything(b *testing.B) {
	r := rand.New(rand.NewSource(3)) //nolint:gosec // deterministic benchmark input
	var v dEverything
	(&filler{r: r}).fill(reflect.ValueOf(&v).Elem())
	// dEverything holds an Encoder that errors on even N; make the benchmark input valid.
	v.Encoders.Err = nil
	if _, err := Values(v); err != nil {
		b.Skip("random input not encodable:", err)
	}
	benchmarkBoth(b, v)
}
