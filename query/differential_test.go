// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package query

import (
	"fmt"
	"math"
	"math/rand"
	"net/url"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"
)

// The differential tests generate random values for a corpus of struct types that covers
// every encoding rule, then require that Values (cached-plan encoder) and legacyValues
// (verbatim upstream copy in legacy_test.go) agree on the url.Values, the error and
// whether the call panics.

// ---- types used by the corpus ----

type dStringer int

func (s dStringer) String() string { return "S<" + strconv.Itoa(int(s)) + ">" }

type dError int

func (e dError) Error() string { return "E<" + strconv.Itoa(int(e)) + ">" }

type dFormatter int

func (f dFormatter) Format(s fmt.State, verb rune) { _, _ = fmt.Fprintf(s, "F<%d/%c>", int(f), verb) }

// dPtrStringer has String on the pointer receiver only, so fmt.Sprint on the value does not use it.
type dPtrStringer int

func (p *dPtrStringer) String() string { return "P<" + strconv.Itoa(int(*p)) + ">" }

type dZeroable struct {
	A int `url:"a"`
	B int `url:"b"`
}

func (z dZeroable) IsZero() bool { return z.A == 0 }

// dPtrZeroable has IsZero on the pointer receiver only, which omitempty does not see on a value.
type dPtrZeroable struct {
	A int `url:"a"`
}

func (z *dPtrZeroable) IsZero() bool { return z.A == 0 }

type dEncVal struct {
	N int
}

func (e dEncVal) EncodeValues(key string, v *url.Values) error {
	v.Add(key+"_val", strconv.Itoa(e.N))
	return nil
}

type dEncPtr struct {
	N int
}

func (e *dEncPtr) EncodeValues(key string, v *url.Values) error {
	if e == nil {
		v.Add(key+"_ptr", "nil")
		return nil
	}
	v.Add(key+"_ptr", strconv.Itoa(e.N))
	return nil
}

type dEncErr struct {
	N int
}

func (e dEncErr) EncodeValues(key string, v *url.Values) error {
	if e.N%2 == 0 {
		return fmt.Errorf("dEncErr %d", e.N)
	}
	v.Add(key, "odd")
	return nil
}

type dNamedString string
type dNamedBool bool
type dNamedInt8 int8
type dNamedUint16 uint16
type dNamedFloat32 float32
type dNamedFloat64 float64
type dNamedTime time.Time

type dInner struct {
	A int       `url:"a"`
	B string    `url:"b,omitempty"`
	T time.Time `url:"t,unix"`
	P *dInner   `url:"p,omitempty"`
}

type dEmbedded struct {
	E1 int      `url:"e1"`
	E2 *dInner  `url:"e2,omitempty"`
	E3 []string `url:"e3,comma"`
}

type dEmbeddedNamed struct {
	N1 int `url:"n1"`
}

// dScalars covers every scalar kind with and without omitempty, plus named types and
// types whose text fmt decides.
type dScalars struct {
	B     bool    `url:"b"`
	BO    bool    `url:"bo,omitempty"`
	BI    bool    `url:"bi,int"`
	BIO   bool    `url:"bio,int,omitempty"`
	I     int     `url:"i"`
	IO    int     `url:"io,omitempty"`
	I8    int8    `url:"i8"`
	I16   int16   `url:"i16,omitempty"`
	I32   int32   `url:"i32"`
	I64   int64   `url:"i64,omitempty"`
	U     uint    `url:"u"`
	U8    uint8   `url:"u8,omitempty"`
	U16   uint16  `url:"u16"`
	U32   uint32  `url:"u32,omitempty"`
	U64   uint64  `url:"u64"`
	UP    uintptr `url:"up,omitempty"`
	F32   float32 `url:"f32"`
	F32O  float32 `url:"f32o,omitempty"`
	F64   float64 `url:"f64"`
	F64O  float64 `url:"f64o,omitempty"`
	C64   complex64
	C128  complex128 `url:"c128,omitempty"`
	S     string     `url:"s"`
	SO    string     `url:"so,omitempty"`
	NoTag int
	Dash  int `url:"-"`
	Empty int `url:",omitempty"`

	NS   dNamedString  `url:"ns"`
	NB   dNamedBool    `url:"nb,int"`
	NI8  dNamedInt8    `url:"ni8"`
	NU16 dNamedUint16  `url:"nu16,omitempty"`
	NF32 dNamedFloat32 `url:"nf32"`
	NF64 dNamedFloat64 `url:"nf64,omitempty"`

	Str  dStringer    `url:"str"`
	StrO dStringer    `url:"stro,omitempty"`
	Err  dError       `url:"err"`
	Fmt  dFormatter   `url:"fmt"`
	PStr dPtrStringer `url:"pstr"`

	unexported int
}

// dPointers covers pointer handling: nil, set, multi-level, to time, to Encoder types.
type dPointers struct {
	PI   *int          `url:"pi"`
	PIO  *int          `url:"pio,omitempty"`
	PPI  **int         `url:"ppi"`
	PS   *string       `url:"ps"`
	PB   *bool         `url:"pb,int"`
	PF   *float64      `url:"pf,omitempty"`
	PT   *time.Time    `url:"pt"`
	PTO  *time.Time    `url:"pto,omitempty"`
	PTU  *time.Time    `url:"ptu,unixmilli"`
	PStr *dStringer    `url:"pstr"`
	PPS  *dPtrStringer `url:"pps"`
	PIn  *dInner       `url:"pin"`
	PInO *dInner       `url:"pino,omitempty"`
	PEV  *dEncVal      `url:"pev"`
	PEP  *dEncPtr      `url:"pep"`
	PEPO *dEncPtr      `url:"pepo,omitempty"`
}

// dTimes covers every time option and layout on values and pointers.
type dTimes struct {
	T   time.Time    `url:"t"`
	TO  time.Time    `url:"to,omitempty"`
	TU  time.Time    `url:"tu,unix"`
	TM  time.Time    `url:"tm,unixmilli"`
	TN  time.Time    `url:"tn,unixnano"`
	TL  time.Time    `url:"tl" layout:"2006-01-02"`
	TLU time.Time    `url:"tlu,unix" layout:"2006-01-02"`
	PT  *time.Time   `url:"pt,omitempty" layout:"15:04:05"`
	NT  dNamedTime   `url:"nt"`
	ST  []time.Time  `url:"st,comma,unix"`
	AT  [2]time.Time `url:"at"`
}

// dSlices covers slices and arrays with every delimiter option and element type.
type dSlices struct {
	S      []string      `url:"s"`
	SO     []string      `url:"so,omitempty"`
	SC     []string      `url:"sc,comma"`
	SSp    []string      `url:"ssp,space"`
	SSemi  []string      `url:"ssemi,semicolon"`
	SB     []string      `url:"sb,brackets"`
	SN     []string      `url:"sn,numbered"`
	SD     []string      `url:"sd" del:"!"`
	SDC    []string      `url:"sdc,comma" del:"!"`
	SI     []int         `url:"si"`
	SBI    []bool        `url:"sbi,int,comma"`
	SF     []float32     `url:"sf,comma"`
	SP     []*int        `url:"sp"`
	SPC    []*int        `url:"spc,comma"`
	SStr   []dStringer   `url:"sstr"`
	SIn    []dInner      `url:"sin"`
	SPIn   []*dInner     `url:"spin,comma"`
	SEnc   []dEncVal     `url:"senc"`
	SIf    []interface{} `url:"sif"`
	SS     [][]string    `url:"ss,comma"`
	A      [3]int        `url:"a"`
	AO     [0]int        `url:"ao"`
	AC     [2]string     `url:"ac,semicolon"`
	Bytes  []byte        `url:"bytes"`
	BytesO []byte        `url:"byteso,omitempty"`
	BytesC []byte        `url:"bytesc,comma"`
}

// dNested covers nested structs (scoped names), embedded structs, pointers to embedded
// structs, embedded structs with a tag name, and duplicate names.
type dNested struct {
	dEmbedded
	*dEmbeddedNamed `url:"named"`
	dScalarsEmbed
	In   dInner       `url:"in"`
	InO  dInner       `url:"ino,omitempty"`
	PIn  *dInner      `url:"pin"`
	Z    dZeroable    `url:"z,omitempty"`
	ZS   dZeroable    `url:"zs"`
	PZ   dPtrZeroable `url:"pz,omitempty"`
	Dup1 int          `url:"dup"`
	Dup2 string       `url:"dup"`
	Deep struct {
		L1 struct {
			L2 struct {
				V int `url:"v"`
			} `url:"l2"`
			S []string `url:"s,brackets"`
		} `url:"l1"`
	} `url:"deep"`
}

type dScalarsEmbed struct {
	SE int `url:"se,omitempty"`
}

// dEmbeddedPtr embeds a pointer to a struct, which is promoted only when non-nil.
type dEmbeddedPtr struct {
	*dEmbedded
	X int `url:"x"`
}

// dInterfaces covers interface fields holding scalars, pointers, slices, structs,
// Encoders and nil.
type dInterfaces struct {
	I   interface{}  `url:"i"`
	IO  interface{}  `url:"io,omitempty"`
	IC  interface{}  `url:"ic,comma"`
	II  interface{}  `url:"ii,int"`
	IU  interface{}  `url:"iu,unix"`
	IS  fmt.Stringer `url:"is"`
	ISO fmt.Stringer `url:"iso,omitempty"`
}

// dEncoders covers Encoder implementations on value and pointer receivers, and errors.
type dEncoders struct {
	V   dEncVal  `url:"v"`
	VO  dEncVal  `url:"vo,omitempty"`
	P   dEncPtr  `url:"p"`
	PP  *dEncPtr `url:"pp"`
	PV  *dEncVal `url:"pv"`
	Err *dEncErr `url:"err,omitempty"`
}

// dRecursive is a recursive type.
type dRecursive struct {
	V    int          `url:"v"`
	Next *dRecursive  `url:"next,omitempty"`
	Kids []dRecursive `url:"kids"`
}

// dMaps: maps are not encoded specially, they go through fmt.
type dMaps struct {
	M  map[string]int  `url:"m"`
	MO map[string]int  `url:"mo,omitempty"`
	PM *map[string]int `url:"pm"`
}

// dEverything nests the whole corpus.
type dEverything struct {
	dScalars
	Pointers   dPointers    `url:"ptr"`
	Times      dTimes       `url:"times"`
	Slices     dSlices      `url:"slices"`
	Nested     dNested      `url:"nested"`
	Interfaces dInterfaces  `url:"ifaces"`
	Encoders   dEncoders    `url:"enc"`
	Recursive  dRecursive   `url:"rec"`
	Maps       dMaps        `url:"maps"`
	EmbPtr     dEmbeddedPtr `url:"embptr"`
}

var corpus = []interface{}{
	dScalars{}, dPointers{}, dTimes{}, dSlices{}, dNested{}, dEmbeddedPtr{},
	dInterfaces{}, dEncoders{}, dRecursive{}, dMaps{}, dEverything{},
	dInner{}, dEmbedded{}, dZeroable{},
}

// ---- random value generation ----

var (
	sampleStrings = []string{"", "a", "hello world", "a&b=c", "ü/é?", "0", "-1", " ", "x,y;z", "[]", "\n"}
	sampleFloats  = []float64{0, 1, -1, 0.1, 0.5, 1e21, 1e20, 1e-7, 1e-4, 1e-5, 123456789.123, 100000, 1e6, 5e-324,
		math.MaxFloat32, math.MaxFloat64, math.SmallestNonzeroFloat32, math.NaN(), math.Inf(1), math.Inf(-1), 3.14159, -2.5e10}
)

type filler struct {
	r     *rand.Rand
	depth int
}

func (f *filler) str() string {
	if f.r.Intn(4) == 0 {
		return sampleStrings[f.r.Intn(len(sampleStrings))]
	}
	n := f.r.Intn(6)
	b := make([]byte, n)
	for i := range b {
		b[i] = "abcxyz09 &=+%"[f.r.Intn(13)]
	}
	return string(b)
}

func (f *filler) float() float64 {
	switch f.r.Intn(3) {
	case 0:
		return sampleFloats[f.r.Intn(len(sampleFloats))]
	case 1:
		return f.r.NormFloat64() * math.Pow(10, float64(f.r.Intn(30)-15))
	default:
		return float64(f.r.Intn(2000) - 1000)
	}
}

func (f *filler) time() time.Time {
	if f.r.Intn(5) == 0 {
		return time.Time{}
	}
	sec := f.r.Int63n(4e9) - 1e9
	nsec := f.r.Int63n(1e9)
	if f.r.Intn(2) == 0 {
		nsec = 0
	}
	t := time.Unix(sec, nsec)
	switch f.r.Intn(3) {
	case 0:
		return t.UTC()
	case 1:
		return t.In(time.FixedZone("X", -5*3600))
	default:
		return t
	}
}

// interfaceValues are the concrete values an interface{} field can hold.
func (f *filler) iface() interface{} {
	n := f.r.Intn(200)
	s := f.str()
	t := f.time()
	switch f.r.Intn(16) {
	case 0:
		return nil
	case 1:
		return n
	case 2:
		return s
	case 3:
		return &n
	case 4:
		return []string{s, f.str()}
	case 5:
		return []int{}
	case 6:
		return dInner{A: n, B: s, T: t}
	case 7:
		return &dInner{A: n}
	case 8:
		return dEncVal{N: n}
	case 9:
		return &dEncPtr{N: n}
	case 10:
		return dStringer(n)
	case 11:
		return t
	case 12:
		return &t
	case 13:
		return f.float()
	case 14:
		return n%2 == 0
	default:
		var p *int
		return p
	}
}

func (f *filler) fill(v reflect.Value) {
	if !v.CanSet() {
		return
	}
	if v.Type() == timeType {
		v.Set(reflect.ValueOf(f.time()))
		return
	}

	switch v.Kind() {
	case reflect.Bool:
		v.SetBool(f.r.Intn(2) == 0)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		switch f.r.Intn(4) {
		case 0:
			v.SetInt(0)
		case 1:
			v.SetInt(int64(f.r.Intn(300) - 150))
		default:
			v.SetInt(f.r.Int63() - (1 << 62))
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		switch f.r.Intn(3) {
		case 0:
			v.SetUint(0)
		case 1:
			v.SetUint(uint64(f.r.Intn(300))) //nolint:gosec // non-negative
		default:
			v.SetUint(uint64(f.r.Int63())) //nolint:gosec // non-negative
		}
	case reflect.Float32, reflect.Float64:
		v.SetFloat(f.float())
	case reflect.Complex64, reflect.Complex128:
		v.SetComplex(complex(f.float(), f.float()))
	case reflect.String:
		v.SetString(f.str())
	case reflect.Ptr:
		if f.depth > 4 || f.r.Intn(3) == 0 {
			v.Set(reflect.Zero(v.Type()))
			return
		}
		p := reflect.New(v.Type().Elem())
		f.depth++
		f.fill(p.Elem())
		f.depth--
		v.Set(p)
	case reflect.Slice:
		if f.depth > 4 || f.r.Intn(4) == 0 {
			if f.r.Intn(2) == 0 {
				v.Set(reflect.Zero(v.Type()))
			} else {
				v.Set(reflect.MakeSlice(v.Type(), 0, 0))
			}
			return
		}
		n := 1 + f.r.Intn(3)
		s := reflect.MakeSlice(v.Type(), n, n)
		f.depth++
		for i := 0; i < n; i++ {
			f.fill(s.Index(i))
		}
		f.depth--
		v.Set(s)
	case reflect.Array:
		for i := 0; i < v.Len(); i++ {
			f.fill(v.Index(i))
		}
	case reflect.Map:
		if f.r.Intn(2) == 0 {
			v.Set(reflect.Zero(v.Type()))
			return
		}
		m := reflect.MakeMap(v.Type())
		if f.r.Intn(3) != 0 {
			k := reflect.New(v.Type().Key()).Elem()
			e := reflect.New(v.Type().Elem()).Elem()
			f.fill(k)
			f.fill(e)
			m.SetMapIndex(k, e)
		}
		v.Set(m)
	case reflect.Struct:
		if f.depth > 6 {
			return
		}
		f.depth++
		for i := 0; i < v.NumField(); i++ {
			f.fill(v.Field(i))
		}
		f.depth--
	case reflect.Interface:
		val := f.iface()
		if val == nil {
			v.Set(reflect.Zero(v.Type()))
			return
		}
		rv := reflect.ValueOf(val)
		if rv.Type().Implements(v.Type()) {
			v.Set(rv)
		}
	}
}

// randomValue returns a filled copy of the corpus entry's type, as a value or a pointer.
func randomValue(r *rand.Rand, proto interface{}) interface{} {
	p := reflect.New(reflect.TypeOf(proto))
	f := &filler{r: r}
	f.fill(p.Elem())
	if r.Intn(3) == 0 {
		return p.Interface()
	}
	return p.Elem().Interface()
}

type outcome struct {
	values   url.Values
	err      string
	panicked bool
	panicMsg string
}

func run(fn func(interface{}) (url.Values, error), v interface{}) (o outcome) {
	defer func() {
		if r := recover(); r != nil {
			o.panicked = true
			o.panicMsg = fmt.Sprint(r)
		}
	}()
	values, err := fn(v)
	o.values = values
	if err != nil {
		o.err = err.Error()
	}
	return o
}

func assertSame(t *testing.T, v interface{}, seed int64) {
	t.Helper()
	want := run(legacyValues, v)
	got := run(Values, v)

	if want.panicked != got.panicked {
		t.Fatalf("seed %d, %T: legacy panicked=%v (%s), new panicked=%v (%s)", seed, v, want.panicked, want.panicMsg, got.panicked, got.panicMsg)
	}
	if want.panicked {
		return
	}
	if want.err != got.err {
		t.Fatalf("seed %d, %T: legacy err %q, new err %q", seed, v, want.err, got.err)
	}
	if !reflect.DeepEqual(want.values, got.values) {
		t.Fatalf("seed %d, %T:\nlegacy: %v\nnew:    %v\ninput: %+v", seed, v, want.values, got.values, v)
	}
}

func TestDifferential_Corpus(t *testing.T) {
	const iterations = 1500

	for _, proto := range corpus {
		proto := proto
		t.Run(fmt.Sprintf("%T", proto), func(t *testing.T) {
			for seed := int64(0); seed < iterations; seed++ {
				r := rand.New(rand.NewSource(seed)) //nolint:gosec // deterministic test data
				assertSame(t, randomValue(r, proto), seed)
			}
		})
	}
}

func TestDifferential_ZeroValues(t *testing.T) {
	_ = dScalars{}.unexported // present only to check that unexported fields are skipped

	for _, proto := range corpus {
		assertSame(t, proto, -1)
		p := reflect.New(reflect.TypeOf(proto)).Interface()
		assertSame(t, p, -2)
	}
}

func TestDifferential_TopLevelInputs(t *testing.T) {
	var nilPtr *dScalars
	var nilIface interface{}
	n := 3
	inputs := []interface{}{
		nil, nilPtr, nilIface, 42, "str", []int{1}, &n, map[string]int{"a": 1},
		struct{}{}, &struct{}{}, struct{ unexported int }{1},
		func() interface{} { p := &dInner{A: 1}; return &p }(), // pointer to pointer
	}
	for i, in := range inputs {
		assertSame(t, in, int64(-100-i))
	}
}

// TestDifferential_NilInterfaceEncoder pins the upstream behavior for a nil interface
// field whose interface type embeds Encoder: both implementations panic the same way.
func TestDifferential_NilInterfaceEncoder(t *testing.T) {
	type encIface interface {
		Encoder
	}
	type s struct {
		E encIface `url:"e"`
	}
	assertSame(t, s{}, -200)
	assertSame(t, s{E: dEncVal{N: 1}}, -201)
}

func TestDifferential_FloatFormatting(t *testing.T) {
	type s struct {
		F32 float32   `url:"f32"`
		F64 float64   `url:"f64"`
		S32 []float32 `url:"s32,comma"`
		S64 []float64 `url:"s64"`
	}
	for i, f := range sampleFloats {
		assertSame(t, s{F32: float32(f), F64: f, S32: []float32{float32(f), 1}, S64: []float64{f, -f}}, int64(-300-i))
	}
	r := rand.New(rand.NewSource(7)) //nolint:gosec // deterministic test data
	for i := 0; i < 20000; i++ {
		f := math.Float64frombits(r.Uint64())
		g := math.Float32frombits(r.Uint32())
		assertSame(t, s{F32: g, F64: f, S32: []float32{g}, S64: []float64{f}}, int64(-400-i))
	}
}

func TestDifferential_IntegerFormatting(t *testing.T) {
	type s struct {
		I8  int8    `url:"i8"`
		I64 int64   `url:"i64"`
		U8  uint8   `url:"u8"`
		U64 uint64  `url:"u64"`
		UP  uintptr `url:"up"`
	}
	edges := []int64{0, 1, -1, math.MaxInt8, math.MinInt8, math.MaxInt64, math.MinInt64}
	for i, e := range edges {
		assertSame(t, s{I8: int8(e), I64: e, U8: uint8(e), U64: uint64(e), UP: uintptr(e)}, int64(-500-i)) //nolint:gosec // wraparound is the point
	}
	assertSame(t, s{U64: math.MaxUint64, UP: ^uintptr(0)}, -600)
}

// TestValues_Concurrent runs the encoder from many goroutines over every corpus type at
// once, so the plan and type caches are exercised under the race detector.
func TestValues_Concurrent(t *testing.T) {
	const goroutines = 16
	const perGoroutine = 200

	var wg sync.WaitGroup
	errs := make(chan string, goroutines)

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			r := rand.New(rand.NewSource(int64(g))) //nolint:gosec // deterministic test data
			for i := 0; i < perGoroutine; i++ {
				proto := corpus[r.Intn(len(corpus))]
				v := randomValue(r, proto)
				want := run(legacyValues, v)
				got := run(Values, v)
				if want.panicked != got.panicked || want.err != got.err || (!want.panicked && !reflect.DeepEqual(want.values, got.values)) {
					errs <- fmt.Sprintf("goroutine %d iteration %d %T: mismatch", g, i, v)
					return
				}
			}
		}(g)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}

func TestPlanCache(t *testing.T) {
	typ := reflect.TypeOf(dEverything{})
	p1 := planFor(typ)
	p2 := planFor(typ)
	if p1 != p2 {
		t.Fatal("planFor returned different plans for the same type")
	}

	// Running the encoder must not add entries for types it has already seen.
	count := func() int {
		n := 0
		plans.Range(func(_, _ interface{}) bool { n++; return true })
		return n
	}
	if _, err := Values(dEverything{}); err != nil {
		t.Fatal(err)
	}
	before := count()
	for i := 0; i < 10; i++ {
		if _, err := Values(dEverything{}); err != nil {
			t.Fatal(err)
		}
	}
	if after := count(); after != before {
		t.Fatalf("plan cache grew from %d to %d on repeated calls", before, after)
	}
}

func TestFieldPlan_Tags(t *testing.T) {
	type s struct {
		A int `url:"a,omitempty,int,comma,space,semicolon,brackets,numbered,unix,unixmilli,unixnano" del:"!" layout:"x"`
		B int `url:",omitempty"`
		C int `url:"-"`
		D int `url:"-,"`
		E int `url:"e"`
		F int `url:""`
		G int `url:"g,"`
		d int
		dEmbeddedNamed
		*dEmbedded `url:"emb"`
	}
	p := planFor(reflect.TypeOf(s{}))

	names := make([]string, len(p.fields))
	for i, f := range p.fields {
		names[i] = f.name
	}
	want := []string{"a", "B", "-", "e", "F", "g", "dEmbeddedNamed", "emb"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("field names: got %v, want %v", names, want)
	}

	a := p.fields[0]
	allOptions := a.omitEmpty && a.boolInt && a.comma && a.space && a.semicolon && a.brackets && a.numbered && a.unix && a.unixMilli && a.unixNano
	if !allOptions {
		t.Fatalf("options not all parsed: %+v", a)
	}
	_ = s{}.d // unexported: present only to check that planFor skips it
	if a.del != "!" || a.layout != "x" {
		t.Fatalf("del/layout not parsed: %+v", a)
	}
	if !p.fields[6].embedded || p.fields[7].embedded {
		t.Fatalf("embedded flags: %+v %+v", p.fields[6], p.fields[7])
	}
}

func TestTypeInfo(t *testing.T) {
	cases := []struct {
		typ  reflect.Type
		want typeInfo
	}{
		{reflect.TypeOf(0), typeInfo{baseKind: reflect.Int}},
		{reflect.TypeOf((**string)(nil)), typeInfo{baseKind: reflect.String}},
		{reflect.TypeOf(time.Time{}), typeInfo{baseKind: reflect.Struct, baseIsTime: true, baseUsesSprint: true, zeroable: true}},
		{reflect.TypeOf((*time.Time)(nil)), typeInfo{baseKind: reflect.Struct, baseIsTime: true, baseUsesSprint: true, zeroable: true}},
		{reflect.TypeOf(dStringer(0)), typeInfo{baseKind: reflect.Int, baseUsesSprint: true}},
		{reflect.TypeOf(dError(0)), typeInfo{baseKind: reflect.Int, baseUsesSprint: true}},
		{reflect.TypeOf(dFormatter(0)), typeInfo{baseKind: reflect.Int, baseUsesSprint: true}},
		{reflect.TypeOf(dPtrStringer(0)), typeInfo{baseKind: reflect.Int}},
		{reflect.TypeOf((*dPtrStringer)(nil)), typeInfo{baseKind: reflect.Int}},
		{reflect.TypeOf(dZeroable{}), typeInfo{baseKind: reflect.Struct, zeroable: true}},
		{reflect.TypeOf(dPtrZeroable{}), typeInfo{baseKind: reflect.Struct}},
		{reflect.TypeOf(dEncVal{}), typeInfo{baseKind: reflect.Struct, implementsEncoder: true}},
		{reflect.TypeOf((*dEncVal)(nil)), typeInfo{baseKind: reflect.Struct, implementsEncoder: true, elemImplementsEncoder: true}},
		{reflect.TypeOf(dEncPtr{}), typeInfo{baseKind: reflect.Struct}},
		{reflect.TypeOf((*dEncPtr)(nil)), typeInfo{baseKind: reflect.Struct, implementsEncoder: true}},
		{reflect.TypeOf((*Encoder)(nil)).Elem(), typeInfo{baseKind: reflect.Interface, implementsEncoder: true}},
	}
	for _, c := range cases {
		got := *typeInfoFor(c.typ)
		if got != c.want {
			t.Errorf("%v: got %+v, want %+v", c.typ, got, c.want)
		}
	}
}
