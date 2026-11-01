# gad

[![Go Reference](https://pkg.go.dev/badge/github.com/roy2220/gad.svg)](https://pkg.go.dev/github.com/roy2220/gad)
[![Coverage](./.badges/coverage.svg)](#)

A tiny dependency wiring framework for Go application startup.

Wiring dependencies by hand is tedious, and the popular DI frameworks felt too
heavy for my taste, so _gad_ stays small: a dozen methods, no reflection magic,
no code generation. The core concept is the task: each task is one function that
begins by declaring the values it requires, provides and decorates, then does
its work. _gad_ executes the tasks in dependency order.

Its API is built on generic methods, so it requires **Go 1.27** or later.

_gad_ is **DAG** (directed acyclic graph) spelled backwards.

> [!NOTE]
> The source code is handcrafted by a human. AI only assisted with docs and tests. No AI slop.

## Quick Start

```go
package main

import (
    "context"
    "fmt"

    "github.com/roy2220/gad"
)

type HotDog struct{ Sausage *Sausage; Bun *Bun }
type Sausage struct{ Flavor string }
type Bun struct{ Size string }

func NewHotDog(s *Sausage, b *Bun) *HotDog { return &HotDog{Sausage: s, Bun: b} }
func (h *HotDog) String() string           { return h.Sausage.Flavor + " sausage in a " + h.Bun.Size + " bun" }
func (h *HotDog) Close()                   { fmt.Println("recycle hot dog") }

func NewSausage(flavor string) *Sausage { return &Sausage{Flavor: flavor} }
func (s *Sausage) Close()               { fmt.Println("recycle sausage") }

func NewBun(size string) *Bun { return &Bun{Size: size} }
func (b *Bun) Close()         { fmt.Println("recycle bun") }

func main() {
    var g gad.G

    // The root task is declared first on purpose: declaration order doesn't matter.
    g.RootTask(func(g *gad.G) error {
        h := g.Require[*HotDog]("")

        if !g.Ready() {
            return nil
        }

        fmt.Println("got hot dog: " + h.String())
        return nil
    })

    g.Task(func(g *gad.G) error {
        s := g.Require[*Sausage]("")
        b := g.Require[*Bun]("")

        var h *HotDog
        g.Provide(&h, "")

        if !g.Ready() {
            return nil
        }

        h = NewHotDog(s, b)
        g.Cleanup(h.Close)
        return nil
    })

    g.Task(func(g *gad.G) error {
        var s *Sausage
        g.Provide(&s, "")

        if !g.Ready() {
            return nil
        }

        s = NewSausage("spicy")
        g.Cleanup(s.Close)
        return nil
    })

    g.Task(func(g *gad.G) error {
        var b *Bun
        g.Provide(&b, "")

        if !g.Ready() {
            return nil
        }

        b = NewBun("large")
        g.Cleanup(b.Close)
        return nil
    })

    g.Task(func(g *gad.G) error {
        s := g.Decorate[*Sausage]("")
        b := g.Decorate[*Bun]("")

        if !g.Ready() {
            return nil
        }

        (*s).Flavor = "super-" + (*s).Flavor // partial modification
        *b = &Bun{Size: "small"}             // complete rewrite
        return nil
    })

    if err := g.Start(context.Background()); err != nil {
        panic(err)
    }
    defer g.Stop()

    // Output:
    // got hot dog: super-spicy sausage in a small bun
    // recycle hot dog
    // recycle bun
    // recycle sausage
}
```

_gad_ works out that sausage and bun come first, then the hot dog, and on
shutdown `Stop` runs the cleanups in reverse order.

Declaration order doesn't matter, except among the decorators of the same value
(see **Decorate**).

## How it works

`Ready()` is the boundary between declaring and doing. _gad_ calls every task
twice:

1. **Declaration**: `Ready()` returns false, so the task returns right after its
`Require`, `Provide` and `Decorate` calls. _gad_ collects them and builds the
graph.

2. **Startup**: tasks are called again in dependency order. `Ready()` returns
true and the task runs to the end.

Three rules follow from this:

- Call `Ready()` exactly once per task.

- Avoid side effects before `Ready()`; that code runs twice.

- Use required or decorated values only after `Ready()`; during declaration they
are zero values or nil.

## Philosophy

Keep _gad_ out of `NewXXX` and `XXX.Close`; _gad_ only works in the glue layer
(the tasks) around them.

Let `NewXXX` and `XXX.Close` stay plain and framework-agnostic, so you are free
to replace the wiring framework without touching them.

## Features

**Named values.** A value is identified by its type and a name, so several
values of the same type can coexist. Use the empty name `""` when there is only
one value of that type, as in the Quick Start.

```go
var primary, replica *sql.DB
g.Provide(&primary, "primary")
g.Provide(&replica, "replica")
```

**Only what's needed.** Root tasks always run. Other tasks run only if a root
task depends on them, directly or indirectly, so unused dependencies are never
initialized.

**Decorate.** Several tasks can modify the same value in a well-defined order:
the original `Provide`, then each `Decorate` in declaration order, then all
`Require` readers. `Require` returns a shallow copy of the value, while
`Decorate` returns a pointer to the value itself, so its changes are visible to
everything that runs later. A task that decorates a value runs whenever the task
providing that value runs, even if nothing requires the value.

```go
g.Task(func(g *gad.G) error {
    args := g.Decorate[[]string]("args")
    if !g.Ready() {
        return nil
    }
    *args = append(*args, "--verbose=true")
    return nil
})
```

**Optional values.** `TryRequire` and `TryDecorate` return nil when no task
provides the value.

**Cleanup.** Register cleanups with `Cleanup`, after `Ready()`, next to the code
that acquires the resource. `Stop` runs them in reverse order. If `Start` fails,
it runs the registered cleanups itself, so call `defer g.Stop()` only after
`Start` succeeds.

**Readable errors.** Duplicate provisions, missing provisions and circular
dependencies are reported with source locations (the circular case includes the
full path). Use `errors.Is` with `ErrDuplicateProvision`, `ErrNoProvision`,
`ErrCircularDependencies` and friends.

## License

MIT
