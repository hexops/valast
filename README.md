# valast - convert Go values to their AST <a href="https://hexops.com"><img align="right" alt="Hexops logo" src="https://raw.githubusercontent.com/hexops/media/master/readme.svg"></img></a>

<a href="https://pkg.go.dev/github.com/hexops/valast"><img src="https://pkg.go.dev/badge/badge/github.com/hexops/valast.svg" alt="Go Reference" align="right"></a>
  
[![Go CI](https://github.com/hexops/valast/workflows/Go%20CI/badge.svg)](https://github.com/hexops/valast/actions) [![codecov](https://codecov.io/gh/hexops/valast/branch/main/graph/badge.svg?token=Iw1FdYk0m8)](https://codecov.io/gh/hexops/valast)

Valast converts Go values at runtime into their `go/ast` equivalent, e.g.:

```Go
x := &foo.Bar{
    a: "hello world!",
    B: 1.234,
}
fmt.Println(valast.String(x))
```

Prints string:

```Go
&foo.Bar{a: "hello world!", B: 1.234}
```

## What is this useful for?

This can be useful for debugging and testing, you may think of it as a more comprehensive and configurable version of the `fmt` package's `%+v` and `%#v` formatting directives.

## Alternatives

The following are alternatives to valast:

- [github.com/davecgh/go-spew](https://github.com/davecgh/go-spew) ([may be inactive](https://github.com/davecgh/go-spew/issues/128), produces a Go-like syntax but not exactly Go syntax)
- [github.com/shurcooL/go-goon](https://github.com/shurcooL/go-goon) (based on go-spew, produces valid Go syntax but not via a `go/ast`, [produces less idiomatic/terse results](https://github.com/shurcooL/go-goon/issues/11))

You may also wish to look at [autogold](https://github.com/hexops/autogold) and [go-cmp](https://github.com/google/go-cmp), which aim to solve the "compare Go values in a test" problem.
