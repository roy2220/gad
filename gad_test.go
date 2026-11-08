package gad_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	. "github.com/roy2220/gad"
)

func TestStart(t *testing.T) {
	// Decorators run in declaration order: B decorates A first, then D;
	// the root task reads the final value.
	assert.Equal(
		t,
		[]string{
			"A<-",
			"B<-A; A=>(A,B)",
			"D<-(A,B); (A,B)=>((A,B),D)",
			"C<-((A,B),D)",
		},
		StartGraph(t, `
			root:C require A
			B decorate A
			D decorate A
			A
		`),
	)

	// Diamond: a shared dependency is provided exactly once, and both
	// dependents read the same value.
	assert.Equal(
		t,
		[]string{
			"D<-",
			"B<-D",
			"C<-D",
			"A<-B+C",
		},
		StartGraph(t, `
			root:A require B
			A require C
			B require D
			C require D
			D
		`),
	)

	// Multiple root tasks: every root task runs, and a task unreachable from any
	// root task doesn't run (C is absent from the output).
	assert.Equal(
		t,
		[]string{
			"A<-",
			"B<-",
		},
		StartGraph(t, `
			root:A
			root:B
			C
		`),
	)

	// Optional requirement with the value present.
	assert.Equal(
		t,
		[]string{
			"B<-",
			"A<-B",
		},
		StartGraph(t, `
			root:A try_require B
			B
		`),
	)

	// Optional requirement with the value absent: no error, nil result.
	assert.Equal(
		t,
		[]string{
			"A<-nil",
		},
		StartGraph(t, `
			root:A try_require Z
		`),
	)

	// Optional decoration with the value present: same as non-optional.
	assert.Equal(
		t,
		[]string{
			"B<-",
			"E<-B; B=>(B,E)",
			"A<-(B,E)",
		},
		StartGraph(t, `
			root:A require B
			B
			E try_decorate B
		`),
	)

	// Optional decoration with the value absent: the decorator has no
	// dependency edge to anything, so it never runs.
	assert.Equal(
		t,
		[]string{
			"B<-",
			"A<-B",
		},
		StartGraph(t, `
			root:A require B
			B
			E try_decorate Z
		`),
	)

	// A cycle unreachable from any root task is not an error: the resolver
	// only walks reachable tasks, so C and D (which cycle) never run.
	assert.Equal(
		t,
		[]string{
			"B<-",
			"A<-B",
		},
		StartGraph(t, `
			root:A require B
			B
			C require D
			D require C
		`),
	)

	// A missing provision on an unreachable task is not an error either:
	// C is never reached, so its broken requirement never matters.
	assert.Equal(
		t,
		[]string{
			"B<-",
			"A<-B",
		},
		StartGraph(t, `
			root:A require B
			B
			C require Z
		`),
	)

	// Duplicate requirements on an unreachable task are not an error.
	assert.Equal(
		t,
		[]string{
			"B<-",
			"A<-B",
		},
		StartGraph(t, `
			root:A require B
			B
			C require D
			C require D
			D
		`),
	)

	// Duplicate decorations on an unreachable task are not an error.
	assert.Equal(
		t,
		[]string{
			"B<-",
			"A<-B",
		},
		StartGraph(t, `
			root:A require B
			B
			C decorate D
			C decorate D
			D
		`),
	)

	// Duplicate provisions on unreachable tasks are not an error.
	assert.Equal(
		t,
		[]string{
			"B<-",
			"A<-B",
		},
		StartGraph(t, `
			root:A require B
			B
			C provide Z
			D provide Z
		`),
	)
}

func TestStartErrors(t *testing.T) {
	// No root task.
	assert.ErrorIs(t,
		StartGraphForError(t, `
			A
		`),
		ErrNoRootTask,
	)

	// Required requirement without a provision.
	assert.ErrorIs(t,
		StartGraphForError(t, `
			root:A require Z
		`),
		ErrNoProvision,
	)

	// Same task requiring the same value twice.
	assert.ErrorIs(t,
		StartGraphForError(t, `
			root:A require B
			A require B
			B
		`),
		ErrDuplicateRequirement,
	)

	// Same task decorating the same value twice.
	assert.ErrorIs(t,
		StartGraphForError(t, `
			root:A decorate B
			A decorate B
			B
		`),
		ErrDuplicateDecoration,
	)

	// Two tasks providing the same value.
	assert.ErrorIs(t,
		StartGraphForError(t, `
			root:A provide Z
			B provide Z
		`),
		ErrDuplicateProvision,
	)

	// Duplicate provision with one provider unreachable: the error still
	// surfaces through the reachable one.
	assert.ErrorIs(t,
		StartGraphForError(t, `
			root:A require B
			B provide Z
			C provide Z
		`),
		ErrDuplicateProvision,
	)

	// Two tasks depending on each other.
	assert.ErrorIs(t,
		StartGraphForError(t, `
			root:A require B
			B require A
		`),
		ErrCircularDependencies,
	)

	// Degenerate cycle: a task requiring its own value.
	assert.ErrorIs(t,
		StartGraphForError(t, `
			root:A require A
		`),
		ErrCircularDependencies,
	)

	// Degenerate cycle: a task decorating its own value.
	assert.ErrorIs(t,
		StartGraphForError(t, `
			root:A decorate A
		`),
		ErrCircularDependencies,
	)

	// Three-task cycle: A -> B -> C -> A.
	assert.ErrorIs(t,
		StartGraphForError(t, `
			root:A require B
			B require C
			C decorate A
		`),
		ErrCircularDependencies,
	)

	// Cycle behind a root task: the cycle (B, C, D) doesn't contain the root task,
	// but is still reachable from it, so it is an error.
	assert.ErrorIs(t,
		StartGraphForError(t, `
			root:A require B
			B require C
			C require D
			D require B
		`),
		ErrCircularDependencies,
	)

	// Decorator inside a cycle: A depends on B, and B, as a decorator of
	// A's value, depends back on A.
	assert.ErrorIs(t,
		StartGraphForError(t, `
			A require B
			root:B decorate A
		`),
		ErrCircularDependencies,
	)

	// Cycle closed by a decorator chain: A -> B -> C, and C decorates B,
	// closing the loop B -> C -> B.
	assert.ErrorIs(t,
		StartGraphForError(t, `
			root:A require B
			B require C
			C decorate B
		`),
		ErrCircularDependencies,
	)
}

func TestStop(t *testing.T) {
	var s []int
	var g G
	for n := 1; n <= 6; n++ {
		g.RootTask(func(g *G) error {
			var nn int
			g.Provide(&nn, strconv.Itoa(n))
			if !g.Ready() {
				return nil
			}
			nn = n
			s = append(s, nn)
			g.Cleanup(func() {
				i := len(s) - 1
				require.Equal(t, nn, s[i])
				s = s[:i]
				if nn >= 2 && nn <= 5 {
					panic(fmt.Sprintf("boom %v", nn))
				}
			})
			return g.Context().Err()
		})
	}
	err := g.Start(t.Context())
	require.NoError(t, err)
	assert.Len(t, s, 6)
	assert.PanicsWithValue(t, "boom 5", func() {
		g.Stop()
	})
	assert.Len(t, s, 0)
}

func TestContext(t *testing.T) {
	var g G
	g.RootTask(func(g *G) error {
		if !g.Ready() {
			return nil
		}
		return g.Context().Err()
	})

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := g.Start(ctx)
	assert.ErrorIs(t, err, ErrTaskFailed)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestMisuse(t *testing.T) {
	t.Run("nil taskFunc", func(t *testing.T) {
		var g G
		assert.PanicsWithValue(t, "gad: nil taskFunc", func() { g.RootTask(nil) })
		assert.PanicsWithValue(t, "gad: nil taskFunc", func() { g.Task(nil) })
	})

	t.Run("recursive RootTask/Task call", func(t *testing.T) {
		var g G
		assert.PanicsWithValue(t, "gad: recursive RootTask/Task call", func() {
			g.RootTask(func(g *G) error {
				g.Task(func(*G) error { return nil })
				return nil
			})
		})
	})

	t.Run("missing Ready call", func(t *testing.T) {
		var g G
		assert.PanicsWithValue(t, "gad: missing Ready call", func() {
			g.RootTask(func(*G) error { return nil })
		})
	})

	t.Run("duplicate Ready call", func(t *testing.T) {
		var g G
		assert.PanicsWithValue(t, "gad: duplicate Ready call", func() {
			g.RootTask(func(g *G) error {
				g.Ready()
				g.Ready()
				return nil
			})
		})
	})

	t.Run("task error during declaration", func(t *testing.T) {
		var g G
		assert.PanicsWithValue(t, "gad: taskFunc should never return error during declaration, got: boom", func() {
			g.RootTask(func(g *G) error {
				g.Ready()
				return errors.New("boom")
			})
		})
	})

	t.Run("RootTask during startup", func(t *testing.T) {
		var g G
		g.RootTask(func(g *G) error {
			if !g.Ready() {
				return nil
			}
			g.RootTask(func(*G) error { return nil })
			return nil
		})
		assert.PanicsWithValue(t, "gad: RootTask should never be called during startup", func() {
			_ = g.Start(t.Context())
		})
	})

	t.Run("Task during startup", func(t *testing.T) {
		var g G
		g.RootTask(func(g *G) error {
			if !g.Ready() {
				return nil
			}
			g.Task(func(*G) error { return nil })
			return nil
		})
		assert.PanicsWithValue(t, "gad: Task should never be called during startup", func() {
			_ = g.Start(t.Context())
		})
	})

	t.Run("Require outside declaration/startup", func(t *testing.T) {
		var g G
		assert.PanicsWithValue(t, "gad: Require must be called during declaration/startup", func() {
			g.Require[string]("x")
		})
	})

	t.Run("Decorate outside declaration/startup", func(t *testing.T) {
		var g G
		assert.PanicsWithValue(t, "gad: Decorate must be called during declaration/startup", func() {
			g.Decorate[string]("x")
		})
	})

	t.Run("Provide outside declaration/startup", func(t *testing.T) {
		var g G
		assert.PanicsWithValue(t, "gad: Provide must be called during declaration/startup", func() {
			var s string
			g.Provide(&s, "x")
		})
	})

	t.Run("Ready outside declaration/startup", func(t *testing.T) {
		var g G
		assert.PanicsWithValue(t, "gad: Ready must be called during declaration/startup", func() {
			g.Ready()
		})
	})

	t.Run("Require after Ready", func(t *testing.T) {
		var g G
		assert.PanicsWithValue(t, "gad: Require must be called before Ready", func() {
			g.RootTask(func(g *G) error {
				g.Ready()
				g.Require[string]("x")
				return nil
			})
		})
	})

	t.Run("Provide after Ready", func(t *testing.T) {
		var g G
		assert.PanicsWithValue(t, "gad: Provide must be called before Ready", func() {
			g.RootTask(func(g *G) error {
				g.Ready()
				var s string
				g.Provide(&s, "x")
				return nil
			})
		})
	})

	t.Run("Cleanup outside startup", func(t *testing.T) {
		var g G
		assert.PanicsWithValue(t, "gad: Cleanup must be called during startup", func() {
			g.Cleanup(func() {})
		})
	})

	t.Run("nil cleanup", func(t *testing.T) {
		var g G
		g.RootTask(func(g *G) error {
			if !g.Ready() {
				return nil
			}
			g.Cleanup(nil)
			return nil
		})
		assert.PanicsWithValue(t, "gad: nil cleanup", func() {
			_ = g.Start(t.Context())
		})
	})

	t.Run("Context outside startup", func(t *testing.T) {
		var g G
		assert.PanicsWithValue(t, "gad: Context must be called during startup", func() {
			g.Context()
		})
	})

	t.Run("Stop inside task", func(t *testing.T) {
		var g G
		assert.PanicsWithValue(t, "gad: Stop should never be called during declaration/startup", func() {
			g.RootTask(func(g *G) error {
				g.Ready()
				g.Stop()
				return nil
			})
		})
	})

	t.Run("Start inside task", func(t *testing.T) {
		var g G
		assert.PanicsWithValue(t, "gad: Start should never be called during declaration", func() {
			g.RootTask(func(g *G) error {
				g.Ready()
				_ = g.Start(t.Context())
				return nil
			})
		})
	})

	t.Run("Start during startup", func(t *testing.T) {
		var g G
		g.RootTask(func(g *G) error {
			if !g.Ready() {
				return nil
			}
			_ = g.Start(t.Context())
			return nil
		})
		assert.PanicsWithValue(t, "gad: recursive Start call", func() {
			_ = g.Start(t.Context())
		})
	})

	t.Run("Start without Stop", func(t *testing.T) {
		var g G
		g.RootTask(func(g *G) error {
			if !g.Ready() {
				return nil
			}
			g.Cleanup(func() {})
			return nil
		})
		require.NoError(t, g.Start(t.Context()))
		assert.PanicsWithValue(t, "gad: missing Stop call", func() {
			_ = g.Start(t.Context())
		})
		g.Stop()
	})
}

func StartGraph(t *testing.T, text string) []string {
	output, err := startGraph(t, text)
	require.NoError(t, err)
	return output
}

func StartGraphForError(t *testing.T, text string) error {
	_, err := startGraph(t, text)
	return err
}

func startGraph(t *testing.T, text string) ([]string, error) {
	isRootTask := map[string]bool{}
	taskNames := make([]string, 0, 100)
	taskName2Ops := map[string][]Op{}

	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)

		require.GreaterOrEqual(t, len(fields), 1)
		taskName := fields[0]
		if strings.HasPrefix(taskName, "root:") {
			taskName = taskName[5:]
			isRootTask[taskName] = true
		}
		if !slices.Contains(taskNames, taskName) {
			taskNames = append(taskNames, taskName)
		}
		if len(fields) == 1 {
			continue
		}

		require.GreaterOrEqual(t, len(fields), 3)
		target := fields[2]
		var op Op
		switch fields[1] {
		case "require":
			op = &RequireOp{
				Target: target,
			}
		case "try_require":
			op = &TryRequireOp{
				Target: target,
			}
		case "decorate":
			op = &DecorateOp{
				TaskName: taskName,
				Target:   target,
			}
		case "try_decorate":
			op = &TryDecorateOp{
				TaskName: taskName,
				Target:   target,
			}
		case "provide":
			op = &ProvideOp{
				Target: target,
			}
		default:
			t.Fatal("unreachable")
		}
		taskName2Ops[taskName] = append(taskName2Ops[taskName], op)
	}

	var g G
	var output []string
	for _, taskName := range taskNames {
		task := func(g *G) error {
			ops := taskName2Ops[taskName]
			var s string
			g.Provide(&s, taskName)

			for _, op := range ops {
				op.ExecuteBeforeReady(g)
			}

			if !g.Ready() {
				return nil
			}

			s = taskName

			var b bytes.Buffer
			b.WriteString(taskName)
			b.WriteString("<-")
			for i, op := range ops {
				r := op.ExecuteAfterReady(g)
				if r == "" {
					continue
				}
				if i >= 1 {
					b.WriteByte('+')
				}
				b.WriteString(r)
			}
			output = append(output, b.String())

			return nil
		}
		if isRootTask[taskName] {
			g.RootTask(task)
		} else {
			g.Task(task)
		}
	}

	err := g.Start(t.Context())
	t.Cleanup(g.Stop)
	return output, err
}

type Op interface {
	ExecuteBeforeReady(g *G)
	ExecuteAfterReady(g *G) string
}

type RequireOp struct {
	Target string
	Result string
}

func (o *RequireOp) ExecuteBeforeReady(g *G) {
	o.Result = g.Require[string](o.Target)
}

func (o *RequireOp) ExecuteAfterReady(g *G) string {
	return o.Result
}

type TryRequireOp struct {
	Target string
	Result *string
}

func (o *TryRequireOp) ExecuteBeforeReady(g *G) {
	o.Result = g.TryRequire[string](o.Target)
}

func (o *TryRequireOp) ExecuteAfterReady(g *G) string {
	if o.Result == nil {
		return "nil"
	} else {
		return *o.Result
	}
}

type DecorateOp struct {
	TaskName string
	Target   string
	Result   *string
}

func (o *DecorateOp) ExecuteBeforeReady(g *G) {
	o.Result = g.Decorate[string](o.Target)
}

func (o *DecorateOp) ExecuteAfterReady(g *G) string {
	r := *o.Result
	*o.Result = fmt.Sprintf("(%s,%s)", r, o.TaskName)
	return fmt.Sprintf("%s; %s=>(%s,%s)", r, r, r, o.TaskName)
}

type TryDecorateOp struct {
	TaskName string
	Target   string
	Result   *string
}

func (o *TryDecorateOp) ExecuteBeforeReady(g *G) {
	o.Result = g.TryDecorate[string](o.Target)
}

func (o *TryDecorateOp) ExecuteAfterReady(g *G) string {
	if o.Result == nil {
		return "nil"
	} else {
		r := *o.Result
		*o.Result = fmt.Sprintf("(%s,%s)", r, o.TaskName)
		return fmt.Sprintf("%s; %s=>(%s,%s)", r, r, r, o.TaskName)
	}
}

type ProvideOp struct {
	Target string
}

func (o *ProvideOp) ExecuteBeforeReady(g *G) {
	var s string
	g.Provide(&s, o.Target)
}

func (o *ProvideOp) ExecuteAfterReady(g *G) string {
	return ""
}
