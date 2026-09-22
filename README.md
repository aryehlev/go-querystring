# go-querystring #

[![Go Reference](https://pkg.go.dev/badge/github.com/aryehlev/go-querystring/query.svg)](https://pkg.go.dev/github.com/aryehlev/go-querystring/query)
[![Test Status](https://github.com/aryehlev/go-querystring/workflows/tests/badge.svg)](https://github.com/aryehlev/go-querystring/actions?query=workflow%3Atests)

go-querystring is a Go library for encoding structs into URL query parameters.

This is a fork of [google/go-querystring](https://github.com/google/go-querystring)
with a faster encoder. The API and the encoding rules are unchanged; see
[Performance](#performance) below. Import it as `github.com/aryehlev/go-querystring/query`.

## Usage ##

```go
import "github.com/aryehlev/go-querystring/query"
```

go-querystring is designed to assist in scenarios where you want to construct a
URL using a struct that represents the URL query parameters.  You might do this
to enforce the type safety of your parameters, for example, as is done in the
[go-github][] library.

The query package exports a single `Values()` function.  A simple example:

```go
type Options struct {
  Query   string `url:"q"`
  ShowAll bool   `url:"all"`
  Page    int    `url:"page"`
}

opt := Options{ "foo", true, 2 }
v, _ := query.Values(opt)
fmt.Print(v.Encode()) // will output: "q=foo&all=true&page=2"
```

See the [package godocs][] for complete documentation on supported types and
formatting options.

[go-github]: https://github.com/google/go-github/commit/994f6f8405f052a117d2d0b500054341048fbb08
[package godocs]: https://pkg.go.dev/github.com/aryehlev/go-querystring/query

## Performance ##

The encoding rules depend only on a field's type and struct tag, never on its value.
This fork resolves them once per struct type into a cached plan, so a call to
`Values` walks the plan instead of re-reading and re-parsing every tag, and formats
scalars with `strconv` instead of `fmt.Sprint`. Output is byte-for-byte identical to
upstream: `query/legacy_test.go` keeps a verbatim copy of the upstream encoder and the
differential and fuzz tests in `query/differential_test.go` and `query/fuzz_test.go`
compare the two over random values for every encoding rule.

Benchmarks in `query/bench_test.go`, Apple M-series, Go 1.27:

| input | upstream | this fork |
|---|---|---|
| 3 fields | 460 ns, 11 allocs | 185 ns, 8 allocs |
| 50 scalar fields, mostly `omitempty` | 6.5 µs, 89 allocs | 2.3 µs, 83 allocs |
| nested and embedded structs, slices, time | 2.7 µs, 53 allocs | 1.0 µs, 36 allocs |

## Alternatives ##

If you are looking for a library that can both encode and decode query strings,
you might consider one of these alternatives:

 - https://github.com/gorilla/schema
 - https://github.com/pasztorpisti/qs
 - https://github.com/hetiansu5/urlquery
 - https://github.com/ggicci/httpin (decoder only)
