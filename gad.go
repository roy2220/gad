// Package gad executes tasks in dependency order. Each task declares the
// values it requires, provides and decorates by (type, name); gad resolves the
// dependency graph and executes the tasks accordingly.
package gad

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"slices"
)

const locationFormat = "%s:%d"

// G holds a task graph and its startup state. It is not safe for concurrent use.
type G struct {
	tasks         []task
	isDeclaring   bool
	isStarting    bool
	readyCount    int
	context       context.Context
	valueRegistry map[key]any
	cleanups      []func()
}

type task struct {
	Location     string
	IsRoot       bool
	Func         func(*G) error
	Requirements []requirement
	Provisions   []provision

	// for resolution only
	PendingErr error
	Color      color
}

type color int

const (
	white color = 0
	grey  color = 1
	black color = 2
)

func (n *task) SetPendingErr(err error) {
	if n.PendingErr == nil {
		n.PendingErr = err
	}
}

type requirement struct {
	Location     string
	IsDecoration bool
	IsOptional   bool
	ValueName    string
	ValueType    reflect.Type

	// for resolution only
	Provision     *provision
	PrevDecorator *task
}

type provision struct {
	Location  string
	ValueName string
	ValueType reflect.Type

	// for resolution only
	Task       *task
	Decorators []*task
}

type key struct {
	ValueType reflect.Type
	ValueName string
}

// RootTask declares a root task. Root tasks are always run, along with every
// task they transitively depend on. It may be called multiple times to declare
// multiple root tasks.
//
// The taskFunc is called twice: once during declaration to collect its
// requirements, provisions and decorations, and once during startup to execute.
// It must call Ready exactly once to separate the two parts.
func (g *G) RootTask(taskFunc func(*G) error) {
	g.task(taskFunc, true, "RootTask")
}

// Task declares a non-root task. It runs only if a root task depends on it,
// directly or indirectly. See RootTask for the calling convention.
func (g *G) Task(taskFunc func(*G) error) {
	g.task(taskFunc, false, "Task")
}

func (g *G) task(taskFunc func(*G) error, isRoot bool, methodName string) {
	if taskFunc == nil {
		panic("gad: nil taskFunc")
	}
	if g.isStarting {
		panic("gad: " + methodName + " should never be called during startup")
	}
	if g.isDeclaring {
		panic("gad: recursive RootTask/Task call")
	}

	g.isDeclaring = true
	ok := false
	defer func(l int) {
		g.isDeclaring = false
		if !ok {
			clear(g.tasks[l:])
			g.tasks = g.tasks[:l]
		}
	}(len(g.tasks))

	_, filePath, lineNumber, _ := runtime.Caller(2)

	g.tasks = append(g.tasks, task{
		Location: fmt.Sprintf(locationFormat, filePath, lineNumber),
		IsRoot:   isRoot,
		Func:     taskFunc,
	})

	g.readyCount = 0
	err := taskFunc(g)
	if g.readyCount == 0 {
		panic("gad: missing Ready call")
	}
	if err != nil {
		panic("gad: taskFunc should never return error during declaration, got: " + err.Error())
	}

	ok = true
}

// Require declares a dependency on a value, identified by its type and
// valueName, and returns a shallow copy of it. During declaration it returns
// the zero value. It must be called before Ready.
func (g *G) Require[T any](valueName string) T {
	valuePtr := g.requireOrDecorate[T](valueName, false, false, "Require")
	if g.isDeclaring {
		var zero T
		return zero
	} else {
		return *valuePtr
	}
}

// TryRequire is like Require but allows the value to be absent. It returns nil
// if no task provides the value, or during declaration; otherwise it returns a
// pointer to a shallow copy of the value.
func (g *G) TryRequire[T any](valueName string) *T {
	valuePtr := g.requireOrDecorate[T](valueName, false, true, "TryRequire")
	if valuePtr != nil {
		valueCopy := *valuePtr
		valuePtr = &valueCopy
	}
	return valuePtr
}

// Decorate is like Require but returns a pointer to the value itself rather
// than a shallow copy, so modifications are visible to later readers.
// Decorations of the same value run sequentially in declaration order, after
// its Provide and before any Require. During declaration it returns nil.
func (g *G) Decorate[T any](valueName string) *T {
	return g.requireOrDecorate[T](valueName, true, false, "Decorate")
}

// TryDecorate is like Decorate but allows the value to be absent. It returns
// nil if no task provides the value, or during declaration.
func (g *G) TryDecorate[T any](valueName string) *T {
	return g.requireOrDecorate[T](valueName, true, true, "TryDecorate")
}

func (g *G) requireOrDecorate[T any](valueName string, isDecoration, isOptional bool, methodName string) *T {
	if !(g.isDeclaring || g.isStarting) {
		panic("gad: " + methodName + " must be called during declaration/startup")
	}
	if !(g.readyCount == 0) {
		panic("gad: " + methodName + " must be called before Ready")
	}

	if g.isDeclaring {
		_, filePath, lineNumber, _ := runtime.Caller(2)

		task := &g.tasks[len(g.tasks)-1]
		task.Requirements = append(task.Requirements, requirement{
			Location:     fmt.Sprintf(locationFormat, filePath, lineNumber),
			IsDecoration: isDecoration,
			IsOptional:   isOptional,
			ValueName:    valueName,
			ValueType:    reflect.TypeFor[T](),
		})
		return nil
	} else {
		key := key{reflect.TypeFor[T](), valueName}
		var valuePtr *T
		if isOptional {
			if p, ok := g.valueRegistry[key]; ok {
				valuePtr = p.(*T)
			}
		} else {
			valuePtr = g.valueRegistry[key].(*T)
		}
		return valuePtr
	}
}

// Provide declares the original source of a value, identified by its type
// and valueName. valuePtr must point to a variable set by the task after
// Ready. A value can have at most one source, otherwise Start returns
// ErrDuplicateProvision. Provide must be called before Ready.
func (g *G) Provide[T any](valuePtr *T, valueName string) {
	if valuePtr == nil {
		panic("gad: nil valuePtr")
	}
	if !(g.isDeclaring || g.isStarting) {
		panic("gad: Provide must be called during declaration/startup")
	}
	if !(g.readyCount == 0) {
		panic("gad: Provide must be called before Ready")
	}

	if g.isDeclaring {
		_, filePath, lineNumber, _ := runtime.Caller(1)

		task := &g.tasks[len(g.tasks)-1]
		task.Provisions = append(task.Provisions, provision{
			Location:  fmt.Sprintf(locationFormat, filePath, lineNumber),
			ValueName: valueName,
			ValueType: reflect.TypeFor[T](),
		})
	} else {
		key := key{reflect.TypeFor[T](), valueName}
		g.valueRegistry[key] = valuePtr
	}
}

// Ready separates declaration from execution. It returns false during
// declaration, when values declared by Require, Decorate and Provide are not
// yet ready to use, and true during startup. It must be called exactly once per
// task. Typically:
//
//	if !g.Ready() {
//		return nil
//	}
func (g *G) Ready() bool {
	if !(g.isDeclaring || g.isStarting) {
		panic("gad: Ready must be called during declaration/startup")
	}
	g.readyCount++
	if g.readyCount >= 2 {
		panic("gad: duplicate Ready call")
	}
	return g.isStarting
}

// Errors returned by Start, usually wrapped with location details; test them
// with errors.Is.
var (
	// ErrNoRootTask: no root task has been declared.
	ErrNoRootTask = errors.New("gad: no root task")
	// ErrDuplicateProvision: two tasks provide the same (type, name).
	ErrDuplicateProvision = errors.New("gad: duplicate provision")
	// ErrNoProvision: a mandatory requirement or decoration has no
	// provision.
	ErrNoProvision = errors.New("gad: no provision")
	// ErrDuplicateRequirement: a task declares Require or TryRequire more
	// than once for the same (type, name).
	ErrDuplicateRequirement = errors.New("gad: duplicate requirement")
	// ErrDuplicateDecoration: a task declares Decorate or TryDecorate more
	// than once for the same (type, name).
	ErrDuplicateDecoration = errors.New("gad: duplicate decoration")
	// ErrCircularDependencies: the dependency graph contains a cycle.
	ErrCircularDependencies = errors.New("gad: circular dependencies")
	// ErrTaskFailed: a taskFunc returned an error during startup.
	ErrTaskFailed = errors.New("gad: task failed")
)

// Start resolves the graph and executes the reachable tasks in dependency
// order. It stops at the first task failure.
//
// If Start succeeds, the cleanups registered during startup are kept, and Stop
// must be called before the next Start. If Start fails or a task panics, Start
// calls the registered cleanups before exiting, so the caller need not call
// Stop.
func (g *G) Start(ctx context.Context) error {
	if g.isDeclaring {
		panic("gad: Start should never be called during declaration")
	}
	if g.isStarting {
		panic("gad: recursive Start call")
	}
	if g.cleanups != nil {
		panic("gad: missing Stop call")
	}

	orderedTasks, err := g.resolve()
	if err != nil {
		return err
	}

	g.isStarting = true
	g.context = ctx
	g.valueRegistry = map[key]any{}
	g.cleanups = []func(){}
	ok := false
	defer func() {
		g.isStarting = false
		g.context = nil
		g.valueRegistry = nil
		if !ok {
			g.stop()
		}
	}()

	for _, task := range orderedTasks {
		g.readyCount = 0
		if err := task.Func(g); err != nil {
			return fmt.Errorf("%w; location=%q: %w", ErrTaskFailed, task.Location, err)
		}
	}
	ok = true
	return nil
}

func (g *G) resolve() ([]*task, error) {
	defer func() {
		for i := range g.tasks {
			task := &g.tasks[i]
			task.PendingErr = nil
			task.Color = white
			for j := range task.Requirements {
				requirement := &task.Requirements[j]
				requirement.Provision = nil
				requirement.PrevDecorator = nil
			}
			for j := range task.Provisions {
				provision := &task.Provisions[j]
				provision.Task = nil
				provision.Decorators = nil
			}
		}
	}()

	// phase 0
	var queue [2][]*task
	for i := range g.tasks {
		task := &g.tasks[i]
		if task.IsRoot {
			queue[0] = append(queue[0], task)
		}
	}
	if len(queue[0]) == 0 {
		return nil, ErrNoRootTask
	}

	// phase 1
	{
		key2Provision := make(map[key]*provision)
		for i := range g.tasks {
			task := &g.tasks[i]
			for j := range task.Provisions {
				provision := &task.Provisions[j]
				provision.Task = task
				key := key{provision.ValueType, provision.ValueName}
				if priorProvision, ok := key2Provision[key]; ok {
					err := fmt.Errorf(
						"%w; valueType=%q valueName=%q location1=%q location2=%q",
						ErrDuplicateProvision,
						priorProvision.ValueType,
						priorProvision.ValueName,
						priorProvision.Location,
						provision.Location,
					)
					priorProvision.Task.SetPendingErr(err)
					task.SetPendingErr(err)
				} else {
					key2Provision[key] = provision
				}
			}
		}
		key2Requirement := make(map[key]*requirement)
		for i := range g.tasks {
			task := &g.tasks[i]
			for _, isDecoration := range [...]bool{false, true} {
				clear(key2Requirement)
				for j := range task.Requirements {
					requirement := &task.Requirements[j]
					if requirement.IsDecoration != isDecoration {
						continue
					}
					key := key{requirement.ValueType, requirement.ValueName}
					if priorRequirement, ok := key2Requirement[key]; ok {
						var err error
						if isDecoration {
							err = ErrDuplicateDecoration
						} else {
							err = ErrDuplicateRequirement
						}
						task.SetPendingErr(fmt.Errorf(
							"%w; valueType=%q valueName=%q location1=%q location2=%q",
							err,
							priorRequirement.ValueType,
							priorRequirement.ValueName,
							priorRequirement.Location,
							requirement.Location,
						))
					} else {
						key2Requirement[key] = requirement
					}
					provision, ok := key2Provision[key]
					if !ok && !requirement.IsOptional {
						task.SetPendingErr(fmt.Errorf(
							"%w; valueType=%q valueName=%q location=%q",
							ErrNoProvision,
							requirement.ValueType,
							requirement.ValueName,
							requirement.Location,
						))
					}
					requirement.Provision = provision
				}
			}
		}
		for _, isDecoration := range [...]bool{true, false} {
			// first pass: isDecoration=true
			// second pass: isDecoration=false
			// the order matters
			for i := range g.tasks {
				task := &g.tasks[i]
				for j := range task.Requirements {
					requirement := &task.Requirements[j]
					if requirement.IsDecoration != isDecoration {
						continue
					}
					provision := requirement.Provision
					if provision == nil {
						continue
					}
					if n := len(provision.Decorators); n >= 1 {
						requirement.PrevDecorator = provision.Decorators[n-1]
					}
					if isDecoration {
						provision.Decorators = append(provision.Decorators, task)
					}
				}
			}
		}
	}

	// phase 2
	orderedTasks := make([]*task, 0, len(g.tasks))
	{
		var walkPath []string
		var walk func(task *task, fromLocation string) error
		walk = func(task *task, fromLocation string) error {
			if task.PendingErr != nil {
				return task.PendingErr
			}
			if task.Color == black {
				return nil
			}
			defer func(l int) { walkPath = walkPath[:l] }(len(walkPath))
			if fromLocation != "" {
				walkPath = append(walkPath, fromLocation)
			}
			walkPath = append(walkPath, task.Location)
			if task.Color == grey {
				return fmt.Errorf("%w; walkPath=%q", ErrCircularDependencies, walkPath)
			}

			task.Color = grey
			for j := range task.Requirements {
				requirement := &task.Requirements[j]
				provision := requirement.Provision
				if provision == nil {
					continue
				}
				var err error
				if requirement.PrevDecorator != nil {
					err = walk(requirement.PrevDecorator, requirement.Location)
				} else {
					err = walk(provision.Task, requirement.Location)
				}
				if err != nil {
					return err
				}
			}
			for j := range task.Provisions {
				provision := &task.Provisions[j]
				queue[1] = append(queue[1], provision.Decorators...)
			}
			orderedTasks = append(orderedTasks, task)
			task.Color = black
			return nil
		}

		for len(queue[0]) >= 1 {
			for _, task := range queue[0] {
				if err := walk(task, ""); err != nil {
					return nil, err
				}
			}
			queue[0] = queue[0][:0]
			queue[0], queue[1] = queue[1], queue[0]
		}
	}
	return orderedTasks, nil
}

// Cleanup registers a function to be called by Stop, in reverse order of
// registration. It must be called during startup, after Ready.
func (g *G) Cleanup(cleanup func()) {
	if cleanup == nil {
		panic("gad: nil cleanup")
	}
	if !g.isStarting {
		panic("gad: Cleanup must be called during startup")
	}
	if !(g.readyCount >= 1) {
		panic("gad: Cleanup must be called after Ready")
	}

	g.cleanups = append(g.cleanups, cleanup)
}

// Stop calls the registered cleanup functions in reverse order and clears them.
// It must not be called during declaration or startup.
func (g *G) Stop() {
	if g.isDeclaring || g.isStarting {
		panic("gad: Stop should never be called during declaration/startup")
	}

	g.stop()
}

func (g *G) stop() {
	cleanups := g.cleanups
	g.cleanups = nil
	for _, cleanup := range slices.Backward(cleanups) {
		cleanup()
	}
}

// Context returns the context passed to Start. It must be called during
// startup, after Ready.
func (g *G) Context() context.Context {
	if !g.isStarting {
		panic("gad: Context must be called during startup")
	}
	if !(g.readyCount >= 1) {
		panic("gad: Context must be called after Ready")
	}

	return g.context
}
