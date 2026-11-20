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

func NewHotDog(sausage *Sausage, bun *Bun) *HotDog { return &HotDog{Sausage: sausage, Bun: bun} }
func (h *HotDog) String() string                   { return h.Sausage.Flavor + " sausage in a " + h.Bun.Size + " bun" }
func (h *HotDog) Close()                           { fmt.Println("recycle hot dog") }

func NewSausage(flavor string) *Sausage { return &Sausage{Flavor: flavor} }
func (s *Sausage) Close()               { fmt.Println("recycle sausage") }

func NewBun(size string) *Bun { return &Bun{Size: size} }
func (b *Bun) Close()         { fmt.Println("recycle bun") }

func ExampleG() {
	var g gad.G

	// The root task is declared first on purpose: declaration order doesn't matter.
	g.RootTask(func(g *gad.G) error {
		hotDog := g.Require[*HotDog]("")

		if !g.Ready() {
			return nil
		}

		fmt.Println("got hot dog: " + hotDog.String())
		return nil
	})

	g.Task(func(g *gad.G) error {
		sausage := g.Require[*Sausage]("")
		bun := g.Require[*Bun]("")

		var hotDog *HotDog
		g.Provide(&hotDog, "")

		if !g.Ready() {
			return nil
		}

		hotDog = NewHotDog(sausage, bun)
		g.Cleanup(hotDog.Close)
		return nil
	})

	g.Task(func(g *gad.G) error {
		var sausage *Sausage
		g.Provide(&sausage, "")

		if !g.Ready() {
			return nil
		}

		sausage = NewSausage("spicy")
		g.Cleanup(sausage.Close)
		return nil
	})

	g.Task(func(g *gad.G) error {
		var bun *Bun
		g.Provide(&bun, "")

		if !g.Ready() {
			return nil
		}

		bun = NewBun("large")
		g.Cleanup(bun.Close)
		return nil
	})

	g.Task(func(g *gad.G) error {
		sausagePtr := g.Decorate[*Sausage]("")
		bunPtr := g.Decorate[*Bun]("")

		if !g.Ready() {
			return nil
		}

		(*sausagePtr).Flavor = "super-" + (*sausagePtr).Flavor // partial modification
		*bunPtr = &Bun{Size: "small"}                          // complete rewrite
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
