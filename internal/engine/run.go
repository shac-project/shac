// Copyright 2023 The Shac Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package engine

//go:generate go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.30.0
//go:generate protoc --go_out=. --go_opt=paths=source_relative shac.proto

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
	flag "github.com/spf13/pflag"
	"go.fuchsia.dev/shac-project/shac/internal/sandbox"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"
	"google.golang.org/protobuf/encoding/prototext"
)

func starlarkOptions() *syntax.FileOptions {
	return &syntax.FileOptions{
		// Enable not-yet-standard Starlark features.
		Set:       true,
		While:     true,
		Recursion: true,
	}
}

// Cursor represents a point in a content; generally a source file but it can
// also be a change description.
type Cursor struct {
	Line int
	Col  int

	// Require keyed arguments.
	_ struct{}
}

// DefaultEntryPoint is the default basename of Starlark files to search for and
// run.
const DefaultEntryPoint = "shac.star"

var errEmptyIgnore = errors.New("ignore fields cannot be empty strings")

// Span represents a section in a source file or a change description.
type Span struct {
	// Start is the beginning of the span. If Col is specified, Line must be
	// specified.
	Start Cursor
	// End is the end of the span. If not specified, the span has only one line.
	// If Col is specified, Start.Col must be specified too. It is inclusive.
	// That is, it is impossible to do a 0 width span.
	End Cursor

	// Require keyed arguments.
	_ struct{}
}

// FormatterFiltering specifies whether formatting or non-formatting checks will
// be filtered out.
type FormatterFiltering int

const (
	// AllChecks does not perform any filtering based on whether a check is a
	// formatter or not.
	AllChecks FormatterFiltering = iota
	// OnlyFormatters causes only checks marked with `formatter = True` to be
	// run.
	OnlyFormatters
	// OnlyNonFormatters causes only checks *not* marked with `formatter = True` to
	// be run.
	OnlyNonFormatters
)

// CheckFilter controls which checks are run.
type CheckFilter struct {
	FormatterFiltering FormatterFiltering
	// AllowList specifies checks to run. If non-empty, all other checks will be
	// skipped.
	AllowList []string
	// DenyList specifies checks to skip.
	DenyList []string
}

func (f *CheckFilter) filter(checks []*registeredCheck) ([]*registeredCheck, error) {
	if len(checks) == 0 {
		return checks, nil
	}

	// Keep track of the allowlist elements that correspond to valid checks so
	// we can report any invalid allowlist elements at the end.
	allowList := make(map[string]struct{})
	for _, name := range f.AllowList {
		allowList[name] = struct{}{}
	}
	var allowedAndDenied []string
	denyList := make(map[string]struct{})
	for _, name := range f.DenyList {
		denyList[name] = struct{}{}
		if _, ok := allowList[name]; ok {
			allowedAndDenied = append(allowedAndDenied, name)
		}
	}
	if len(allowedAndDenied) > 0 {
		return nil, fmt.Errorf(
			"checks cannot be both allowed and denied: %s",
			strings.Join(allowedAndDenied, ", "))
	}

	var filtered []*registeredCheck
	for _, check := range checks {
		if len(f.AllowList) != 0 {
			if _, ok := allowList[check.name]; !ok {
				continue
			}
			delete(allowList, check.name)
		}
		if _, ok := denyList[check.name]; ok {
			delete(denyList, check.name)
			continue
		}
		switch f.FormatterFiltering {
		case AllChecks:
		case OnlyFormatters:
			if !check.formatter {
				continue
			}
		case OnlyNonFormatters:
			if check.formatter {
				continue
			}
		default:
			return nil, fmt.Errorf("invalid FormatterFiltering value: %d", f.FormatterFiltering)
		}
		filtered = append(filtered, check)
	}

	if len(allowList) > 0 || len(denyList) > 0 {
		var invalidChecks []string
		for name := range allowList {
			invalidChecks = append(invalidChecks, name)
		}
		for name := range denyList {
			invalidChecks = append(invalidChecks, name)
		}
		var msg string
		if len(invalidChecks) == 1 {
			msg = "check does not exist"
		} else {
			msg = "checks do not exist"
		}
		slices.Sort(invalidChecks)
		return nil, fmt.Errorf("%s: %s", msg, strings.Join(invalidChecks, ", "))
	}

	if len(filtered) == 0 {
		// Fail noisily if all checks are filtered out, it's probably user
		// error.
		return nil, errors.New("no checks to run")
	}
	return filtered, nil
}

// Level is one of "notice", "warning" or "error".
//
// A check is only considered failed if it emits at least one finding with
// level "error".
type Level string

var _ flag.Value = (*Level)(nil)

// Valid Level values.
const (
	Notice  Level = "notice"
	Warning Level = "warning"
	Error   Level = "error"
	Nothing Level = ""
)

func (l *Level) Set(value string) error {
	*l = Level(value)
	if !l.isValid() {
		return fmt.Errorf("invalid level value %q", l)
	}
	return nil
}

func (l *Level) String() string {
	return string(*l)
}

func (l *Level) Type() string {
	return "level"
}

func (l Level) isValid() bool {
	switch l {
	case Notice, Warning, Error:
		return true
	default:
		return false
	}
}

// Report exposes callbacks that the engine calls for everything generated by
// the starlark code.
type Report interface {
	// EmitFinding emits a finding by a check for a specific file. This is not a
	// failure by itself, unless level "error" is used.
	EmitFinding(ctx context.Context, check string, level Level, message, root, file string, s Span, replacements []string) error
	// EmitArtifact emits an artifact by a check.
	//
	// Only one of root or content can be specified. If root is specified, it is
	// a file on disk. The file may disappear after this function is called. If
	// root is not specified, content is the artifact. Either way, file is the
	// display name of the artifact.
	//
	// content must not be modified.
	EmitArtifact(ctx context.Context, check, root, file string, content []byte) error
	// CheckCompleted is called when a check is completed.
	//
	// It is called with the start time, wall clock duration, the highest level emitted and an error
	// if an abnormal error occurred.
	CheckCompleted(ctx context.Context, check string, start time.Time, d time.Duration, r Level, err error)
	// Print is called when print() starlark function is called.
	Print(ctx context.Context, check, file string, line int, message string)
}

// Options is the options for Run().
type Options struct {
	// Report gets all the emitted findings and artifacts from the checks.
	//
	// This is the only required argument. It is recommended to use
	// reporting.Get() which returns the right implementation based on the
	// environment (CI, interactive, etc).
	Report Report
	// Dir overrides the current working directory, making shac behave as if it
	// was run in the specified directory. It defaults to the current working
	// directory.
	Dir string
	// Files lists specific files to analyze.
	Files []string
	// AllFiles tells to consider all files as affected.
	AllFiles bool
	// Recurse tells the engine to run all Main files found in subdirectories.
	Recurse bool
	// Filter controls which checks run.
	Filter CheckFilter
	// Vars contains the user-specified runtime variables and their values.
	Vars map[string]string
	// EntryPoint is the main source file to run. Defaults to shac.star.
	EntryPoint string

	// config is the configuration file. Defaults to shac.textproto. Only used in
	// unit tests.
	config string
}

// Run loads a main shac.star file from a root directory and runs it.
func Run(ctx context.Context, o *Options) error {
	tmpdir, err := os.MkdirTemp("", "shac")
	if err != nil {
		return err
	}
	err = runInner(ctx, o, tmpdir)
	if err2 := os.RemoveAll(tmpdir); err == nil {
		err = err2
	}
	return err
}

func runInner(ctx context.Context, o *Options, tmpdir string) error {
	root, err := resolveRoot(ctx, o.Dir)
	if err != nil {
		return err
	}
	entryPoint := o.EntryPoint
	if entryPoint == "" {
		entryPoint = DefaultEntryPoint
	}
	if filepath.IsAbs(entryPoint) {
		return errors.New("entrypoint file must not be an absolute path")
	}
	config := o.config
	if config == "" {
		config = "shac.textproto"
	}
	absConfig := config
	if !filepath.IsAbs(absConfig) {
		absConfig = filepath.Join(root, absConfig)
	}
	var b []byte
	doc := Document{}
	configExists := false
	if b, err = os.ReadFile(absConfig); err == nil {
		configExists = true
		// First parse the config file ignoring unknown fields and check only
		// min_shac_version, so users get an "unsupported version" error if they
		// set fields that are only available in a later version of shac (as
		// long as min_shac_version is set appropriately).
		opts := prototext.UnmarshalOptions{DiscardUnknown: true}
		if err = opts.Unmarshal(b, &doc); err != nil {
			return err
		}
		if err = doc.CheckVersion(); err != nil {
			return err
		}
		// Parse the config file again, failing on any unknown fields.
		opts.DiscardUnknown = false
		if err = opts.Unmarshal(b, &doc); err != nil {
			return err
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err = doc.Validate(); err != nil {
		return err
	}

	var scm scmCheckout
	if len(o.Files) > 0 {
		var files []file
		files, err = normalizeFiles(o.Files, root)
		if err != nil {
			return err
		}
		scm = &specifiedFilesOnly{files: files, root: root}
	} else {
		scm, err = getSCM(ctx, root, o.AllFiles)
		if err != nil {
			return err
		}
		if len(doc.Ignore) > 0 {
			var patterns []gitignore.Pattern
			for _, p := range doc.Ignore {
				if p == "" {
					return errEmptyIgnore
				}
				patterns = append(patterns, gitignore.ParsePattern(p, nil))
			}
			scm = &filteredSCM{
				matcher: gitignore.NewMatcher(patterns),
				scm:     scm,
			}
		}
		scm = &cachingSCM{scm: scm}
	}

	pkgMgr := NewPackageManager(tmpdir)
	packages, err := pkgMgr.RetrievePackages(ctx, root, &doc)
	if err != nil {
		return err
	}

	sb, err := sandbox.New(tmpdir)
	if err != nil {
		return err
	}
	env := starlarkEnv{
		globals:  getPredeclared(),
		sources:  map[string]*loadedSource{},
		packages: packages,
		opts:     starlarkOptions(),
	}

	subprocessSem := semaphore.NewWeighted(int64(runtime.NumCPU()) + 2)

	var vars map[string]string

	newState := func(scm scmCheckout, subdir string, idx int) (*shacState, error) {
		// Lazy-load vars only once a shac.star file is detected, so that errors
		// about missing shac.star files are prioritized over var validation
		// errors.
		if vars == nil {
			vars = make(map[string]string)
			for _, v := range doc.Vars {
				vars[v.Name] = v.Default
			}
			for name, value := range o.Vars {
				if _, ok := vars[name]; !ok {
					if configExists {
						return nil, fmt.Errorf("var not declared in %s: %s", config, name)
					}
					return nil, fmt.Errorf("var must be declared in a %s file: %s", config, name)
				}
				vars[name] = value
			}
		}

		if subdir != "" {
			normalized := subdir + "/"
			if subdir == "." {
				subdir = ""
				normalized = ""
			}
			scm = &subdirSCM{s: scm, subdir: normalized}
		}
		return &shacState{
			allowNetwork:   doc.AllowNetwork,
			env:            &env,
			filter:         o.Filter,
			entryPoint:     entryPoint,
			r:              o.Report,
			root:           root,
			sandbox:        sb,
			scm:            scm,
			subdir:         subdir,
			subprocessSem:  subprocessSem,
			tmpdir:         filepath.Join(tmpdir, strconv.Itoa(idx)),
			writableRoot:   doc.WritableRoot,
			vars:           vars,
			passthroughEnv: doc.PassthroughEnv,
		}, nil
	}
	var shacStates []*shacState
	if o.Recurse {
		// Each found shac.star is run in its own interpreter for maximum
		// parallelism.
		// Discover all the main files via the SCM. This enables us to not walk
		// ignored files.
		var subdirs []string
		// If the scm provides a method to return the directories containing
		// shac.star files, use that instead of calling `allFiles`, which may
		// not return all shac.star files that should be considered, e.g.
		// because files were specified on the command line.
		//
		// This is also an optimization to avoid doing a `git ls-files` just to
		// discover shac.star files when files to analyze are specified on the
		// command line, since `git ls-files` is slow on large repositories.
		if v, ok := scm.(overridesShacFileDirs); ok {
			subdirs, err = v.shacFileDirs(entryPoint)
			if err != nil {
				return err
			}
		} else {
			files, err := scm.allFiles(ctx, false)
			if err != nil {
				return err
			}
			for _, f := range files {
				n := f.rootedpath()
				if filepath.Base(n) == entryPoint {
					subdir := strings.ReplaceAll(filepath.Dir(n), "\\", "/")
					subdirs = append(subdirs, subdir)
				}
			}
		}
		if len(subdirs) == 0 {
			return fmt.Errorf("no %s files found in %s", entryPoint, root)
		}
		for i, s := range subdirs {
			state, err := newState(scm, s, i)
			if err != nil {
				return err
			}
			shacStates = append(shacStates, state)
		}
	} else {
		if _, err := os.Stat(filepath.Join(root, entryPoint)); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("no %s file in repository root: %s", entryPoint, root)
			}
			return err
		}
		state, err := newState(scm, "", 0)
		if err != nil {
			return err
		}
		shacStates = append(shacStates, state)
	}

	// Parse the starlark files. Run everything from our errgroup.
	eg, ctx := errgroup.WithContext(ctx)
	eg.SetLimit(runtime.NumCPU() + 2)
	// Make it so each shac can submit at least one item.
	ch := make(chan func() error, len(shacStates))
	done := make(chan struct{})
	for _, s := range shacStates {
		s := s
		eg.Go(func() error {
			err := s.parseAndBuffer(ctx, ch)
			done <- struct{}{}
			return err
		})
	}
	count := len(shacStates)
	for loop := true; loop; {
		select {
		case cb := <-ch:
			if cb == nil {
				loop = false
			} else {
				// Actually run the check.
				eg.Go(cb)
			}
		case <-done:
			count--
			if count == 0 {
				// All shac.star processing is done, we can now send a nil to the
				// channel to tell it to stop.
				// Since we are pushing from the same loop that we are pulling, this is
				// blocking. Instead of making the channel buffered, which would slow
				// it down, use a one time goroutine. It's kind of a gross hack but
				// it'll work just fine.
				go func() {
					ch <- nil
				}()
			}
		}
	}
	if err := eg.Wait(); err != nil {
		return err
	}
	// If any check failed, return an error.
	for _, s := range shacStates {
		for i := range s.checks {
			if s.checks[i].highestLevel == Error {
				return ErrCheckFailed
			}
		}
	}
	return nil
}

// resolveRoot resolves an appropriate root directory from which to load shac
// checks and analyze files.
func resolveRoot(ctx context.Context, dir string) (string, error) {
	if dir == "" {
		dir = "."
	}

	fi, err := os.Stat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("no such directory: %s", dir)
	} else if err != nil {
		return "", err
	} else if !fi.IsDir() {
		return "", fmt.Errorf("not a directory: %s", dir)
	}

	dir, err = filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	root, err := runGitCmd(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			log.Printf("git not detected on $PATH")
			return dir, nil
		} else if strings.Contains(err.Error(), "not a git repository") {
			log.Printf("current working directory is not a git repository")
			return dir, nil
		}
		// Any other error is fatal.
		return "", err
	}
	// root will have normal Windows path but git returns a POSIX style path
	// that may be incorrect. Clean it up.
	root = strings.ReplaceAll(filepath.Clean(root), string(os.PathSeparator), "/")
	return root, nil
}

// normalizeFiles makes all the file paths relative to the project root, sorts,
// and removes duplicates.
//
// Input paths may be absolute or relative. If relative, they are assumed to be
// relative to the current working directory.
func normalizeFiles(files []string, root string) ([]file, error) {
	var cwd string
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	var relativized []string
	for _, orig := range files {
		f := orig
		if !filepath.IsAbs(f) {
			f = filepath.Join(cwd, f)
		}
		var rel string
		rel, err = filepath.Rel(root, f)
		if err != nil {
			return nil, err
		}
		// Validates that the path is within the root directory (i.e.
		// doesn't start with "..").
		if !filepath.IsLocal(rel) {
			return nil, fmt.Errorf("cannot analyze file outside root: %s", orig)
		}
		fi, err := os.Stat(f)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				// Make the error message more concise and use the original
				// user-specified path rather than the normalized absolute path.
				return nil, fmt.Errorf("no such file: %s", orig)
			}
			return nil, err
		}
		// TODO(olivernewman): Support analyzing directories. This will require
		// doing a filesystem traversal that respects the scm, so `shac check .`
		// still ignores git-ignored files.
		if fi.IsDir() {
			return nil, fmt.Errorf("is a directory: %s", orig)
		}
		relativized = append(relativized, rel)
	}

	slices.Sort(relativized)
	relativized = slices.Compact(relativized)

	var res []file
	for _, f := range relativized {
		res = append(res, &fileImpl{path: filepath.ToSlash(f)})
	}
	return res, nil
}

// shacState represents a parsing state of one shac.star.
type shacState struct {
	env          *starlarkEnv
	r            Report
	allowNetwork bool
	writableRoot bool
	entryPoint   string
	// root is the root for the root shac.star that was executed. Native path
	// style.
	root string
	// vars is the map of runtime variables and their values.
	vars map[string]string
	// subdir is the relative directory in which this shac.star is located.
	// Only set when Options.Recurse is set to true. POSIX path style.
	subdir string
	tmpdir string
	// scm is a filtered view of runState.scm.
	scm scmCheckout
	// sandbox is the object that can be used for sandboxing subprocesses.
	sandbox sandbox.Sandbox
	// checks is the list of registered checks callbacks via
	// shac.register_check().
	//
	// Checks are added serially, so no lock is needed.
	//
	// Checks are executed sequentially after all Starlark code is loaded and not
	// mutated. They run checks and emit results (results and comments).
	checks []*registeredCheck
	// filter controls which checks run. If nil, all checks will run.
	filter         CheckFilter
	passthroughEnv []*PassthroughEnv

	// Limits the number of concurrent subprocesses launched by ctx.os.exec().
	subprocessSem *semaphore.Weighted

	// Set when fail() is called. This happens only during the first phase, thus
	// no mutex is needed.
	failErr *failure

	// Set when the first phase of starlark interpretation is complete. This
	// complete the serial part, after which execution becomes concurrent.
	doneLoading bool

	mu          sync.Mutex
	printCalled bool
	tmpdirIndex int
}

// ctxShacState pulls out *runState from the context.
//
// Panics if not there.
func ctxShacState(ctx context.Context) *shacState {
	return ctx.Value(&shacStateCtxKey).(*shacState)
}

var shacStateCtxKey = "shac.shacState"

// parseAndBuffer parses and run a single shac.star file, then buffer all its checks.
func (s *shacState) parseAndBuffer(ctx context.Context, ch chan<- func() error) error {
	ctx = context.WithValue(ctx, &shacStateCtxKey, s)
	if err := s.parse(ctx); err != nil {
		return err
	}
	if len(s.checks) == 0 && !s.printCalled {
		return errors.New("did you forget to call shac.register_check?")
	}
	// Last phase where checks are called.
	return s.bufferAllChecks(ctx, ch)
}

// parse parses a single shac.star file.
func (s *shacState) parse(ctx context.Context) error {
	pi := func(th *starlark.Thread, msg string) {
		// Detect if print() was called while loading. Calling either print() or
		// shac.register_check() makes a shac.star valid.
		s.mu.Lock()
		s.printCalled = true
		s.mu.Unlock()
		pos := th.CallFrame(1).Pos
		s.r.Print(ctx, "", pos.Filename(), int(pos.Line), msg)
	}
	p := path.Join(s.subdir, s.entryPoint)
	if _, err := s.env.load(ctx, sourceKey{orig: p, pkg: "__main__", relpath: p}, pi); err != nil {
		var evalErr *starlark.EvalError
		if errors.As(err, &evalErr) {
			return &evalError{evalErr}
		}
		return err
	}
	s.doneLoading = true
	return nil
}

// bufferAllChecks adds all the checks to the channel for execution.
func (s *shacState) bufferAllChecks(ctx context.Context, ch chan<- func() error) error {
	shacCtx, err := getCtx(path.Join(s.root, s.subdir), s.vars)
	if err != nil {
		return err
	}
	args := starlark.Tuple{shacCtx}
	args.Freeze()
	checks, err := s.filter.filter(s.checks)
	if err != nil {
		return err
	}
	for _, check := range checks {
		check := check
		ch <- func() error {
			start := time.Now()
			pi := func(th *starlark.Thread, msg string) {
				pos := th.CallFrame(1).Pos
				s.r.Print(ctx, check.name, pos.Filename(), int(pos.Line), msg)
			}
			err := check.call(ctx, s.env, args, pi)
			if err != nil && ctx.Err() != nil {
				// Don't report the check completion if the context was
				// canceled. The error was probably caused by the context being
				// canceled as a side effect of another check failing. Only the
				// original check failure should be reported, not the canceled
				// check failures.
				return ctx.Err()
			}
			s.r.CheckCompleted(ctx, check.name, start, time.Since(start), check.highestLevel, err)
			return err
		}
	}
	return nil
}

func (s *shacState) newTempDir() (string, error) {
	var err error
	s.mu.Lock()
	i := s.tmpdirIndex
	s.tmpdirIndex++
	if i == 0 {
		// First use, lazy create the temporary directory.
		err = os.Mkdir(s.tmpdir, 0o700)
	}
	s.mu.Unlock()
	if err != nil {
		return "", err
	}
	if i >= 1000000 {
		return "", errors.New("too many temporary directories requested")
	}
	p := filepath.Join(s.tmpdir, strconv.Itoa(i))
	if err = os.Mkdir(p, 0o700); err != nil {
		return "", err
	}
	return p, nil
}

// registeredCheck represents one check that has been registered by
// shac.register_check().
type registeredCheck struct {
	*check
	failErr      *failure // set when fail() is called from within the check, an abnormal failure.
	highestLevel Level    // highest level emitted by EmitFinding.
	subprocesses []*subprocess
}

var checkCtxKey = "shac.check"

// ctxCheck pulls out *registeredCheck from the context.
//
// Returns nil when not run inside a check.
func ctxCheck(ctx context.Context) *registeredCheck {
	c, _ := ctx.Value(&checkCtxKey).(*registeredCheck)
	return c
}

// call calls the check callback and returns an error if an abnormal error happened.
//
// A "normal" error will still have this function return nil.
func (c *registeredCheck) call(ctx context.Context, env *starlarkEnv, args starlark.Tuple, pi printImpl) error {
	ctx = context.WithValue(ctx, &checkCtxKey, c)
	th := env.thread(ctx, c.name, pi)
	if r, err := starlark.Call(th, c.impl, args, c.kwargs); err != nil {
		if c.failErr != nil {
			// fail() was called, return this error since this is an abnormal failure.
			return c.failErr
		}
		var evalErr *starlark.EvalError
		if errors.As(err, &evalErr) {
			return &evalError{evalErr}
		}
		// The vast majority of errors should be caught by the above checks, if
		// we hit this point there's likely a bug in shac or in starlark-go.
		return err
	} else if r != starlark.None {
		return fmt.Errorf("check %q returned an object of type %s, expected None", c.name, r.Type())
	}
	var err error
	for _, proc := range c.subprocesses {
		if !proc.waitCalled {
			if err == nil {
				err = fmt.Errorf("wait() was not called on %s", proc.String())
			}
			_ = proc.cleanup()
		}
	}
	return err
}
