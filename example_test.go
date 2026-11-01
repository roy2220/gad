package gad_test

import (
	"context"
	"fmt"

	"github.com/roy2220/gad"
)

type HotDog struct {
	Sausage *Sausage
	Bun     *Bun
}
type Sausage struct{ Flavor string }
type Bun struct{ Size string }

func NewHotDog(s *Sausage, b *Bun) *HotDog { return &HotDog{Sausage: s, Bun: b} }
func (h *HotDog) String() string           { return h.Sausage.Flavor + " sausage in a " + h.Bun.Size + " bun" }
func (h *HotDog) Close()                   { fmt.Println("recycle hot dog") }

func NewSausage(flavor string) *Sausage { return &Sausage{Flavor: flavor} }
func (s *Sausage) Close()               { fmt.Println("recycle sausage") }

func NewBun(size string) *Bun { return &Bun{Size: size} }
func (b *Bun) Close()         { fmt.Println("recycle bun") }

func ExampleG() {
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
