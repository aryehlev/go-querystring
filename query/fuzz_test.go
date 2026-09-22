// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build go1.18
// +build go1.18

package query

import (
	"encoding/binary"
	"math/rand"
	"testing"
)

// FuzzValues drives the differential corpus from fuzzer-chosen seeds. Run with
//
//	go test -run=^$ -fuzz=FuzzValues ./query
//
// The seed corpus alone (run as a normal test) covers every corpus type with a few seeds.
func FuzzValues(f *testing.F) {
	for i := 0; i < len(corpus); i++ {
		for _, seed := range []int64{0, 1, 42, 1 << 40} {
			f.Add(uint8(i), seed)
		}
	}

	f.Fuzz(func(t *testing.T, which uint8, seed int64) {
		proto := corpus[int(which)%len(corpus)]
		r := rand.New(rand.NewSource(seed)) //nolint:gosec // deterministic from the fuzz input
		assertSame(t, randomValue(r, proto), seed)
	})
}

// FuzzValues_Bytes lets the fuzzer shape the random stream directly, so mutations of the
// input change individual choices rather than replacing the whole value.
func FuzzValues_Bytes(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9})
	f.Add(make([]byte, 64))

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) < 9 {
			return
		}
		proto := corpus[int(data[0])%len(corpus)]
		seed := int64(binary.LittleEndian.Uint64(data[1:9])) //nolint:gosec // any seed is fine
		r := rand.New(rand.NewSource(seed))                  //nolint:gosec // deterministic from the fuzz input
		assertSame(t, randomValue(r, proto), seed)
	})
}
