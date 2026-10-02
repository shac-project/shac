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
	"maps"
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

// maxConcurrency is the maximum number of concurrent checks that will be run.
var maxConcurrency = runtime.NumCPU() + 2

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

	allowList := make(map[string]struct{})
	for _, name := range f.AllowList {
		allowList[name] = struct{}{}
	}
	denyList := make(map[string]struct{})
	for _, name := range f.DenyList {
		denyList[name] = struct{}{}
	}

	var filtered []*registeredCheck
	for _, check := range checks {
		if len(f.AllowList) != 0 {
			if _, ok := allowList[check.name]; !ok {
				continue
			}
		}
		if _, ok := denyList[check.name]; ok {
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

	return filtered, nil
}

// validate validates the filter configuration against the set of discovered
// checks.
func (f *CheckFilter) validate(shacStates []*shacState) error {
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
		return fmt.Errorf(
			"checks cannot be both allowed and denied: %s",
			strings.Join(allowedAndDenied, ", "))
	}

	// Remove all known checks from the allowlist and denylist to validate that
	// there are no invalid checks in either list.
	for _, s := range shacStates {
		for _, check := range s.checks {
			delete(allowList, check.name)
			delete(denyList, check.name)
		}
	}
	if len(allowList) > 0 || len(denyList) > 0 {
		var invalidChecks []string
		invalidChecks = slices.AppendSeq(invalidChecks, maps.Keys(allowList))
		invalidChecks = slices.AppendSeq(invalidChecks, maps.Keys(denyList))
		var msg string
		if len(invalidChecks) == 1 {
			msg = "check does not exist"
		} else {
			msg = "checks do not exist"
		}
		slices.Sort(invalidChecks)
		return fmt.Errorf("%s: %s", msg, strings.Join(invalidChecks, ", "))
	}
	return nil
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
//
// Concurrency contract:
//   - Methods may be called concurrently across different check names.
//   - For any single check name, calls to EmitFinding, EmitArtifact, and CheckCompleted
//     are guaranteed to be called sequentially.
type Report interface {
	// EmitFinding emits a finding by a check for a specific file. This is not a
	// failure by itself, unless level "error" is used.
	EmitFinding(ctx context.Context, check string, level Level, message, root, file string, s Span, replacements []string, props map[string]string) error
	// EmitCommitMessageFinding emits a finding related to a commit message.
	EmitCommitMessageFinding(ctx context.Context, check string, level Level, message string, commitHash string, commitMessage string, s Span, props map[string]string) error
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

// TestReporter exposes callbacks for test execution.
type TestReporter interface {
	// Print is called for print() calls made while loading a test file,
	// outside of any test function.
	Print(ctx context.Context, file string, line int, msg string)
	// TestResult is called once per test function after it completes.
	TestResult(ctx context.Context, name, file string, d time.Duration, err error, prints []string)
}

// Options is the options for Run().
type Options struct {
	// Report gets all the emitted findings and artifacts from the checks.
	//
	// This is the only required argument. It is recommended to use
	// reporting.Get() which returns the right implementation based on the
	// environment (CI, interactive, etc).
	Report Report
	// TestReporter gets the results of test execution.
	TestReporter TestReporter
	// Dir overrides the current working directory, making shac behave as if it
	// was run in the specified directory. It defaults to the current working
	// directory.
	Dir string
	// Files lists specific files or directories to analyze.
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

	Stdin []byte

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

func loadDocument(root, config string, doc *Document) (bool, error) {
	if config == "" {
		config = "shac.textproto"
	}
	absConfig := config
	if !filepath.IsAbs(absConfig) {
		absConfig = filepath.Join(root, absConfig)
	}
	var b []byte
	var err error
	configExists := false
	if b, err = os.ReadFile(absConfig); err == nil {
		configExists = true
		// First parse the config file ignoring unknown fields and check only
		// min_shac_version, so users get an "unsupported version" error if they
		// set fields that are only available in a later version of shac (as
		// long as min_shac_version is set appropriately).
		opts := prototext.UnmarshalOptions{DiscardUnknown: true}
		if err = opts.Unmarshal(b, doc); err != nil {
			return false, err
		}
		if err = doc.CheckVersion(); err != nil {
			return false, err
		}
		// Parse the config file again, failing on any unknown fields.
		opts.DiscardUnknown = false
		if err = opts.Unmarshal(b, doc); err != nil {
			return false, err
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	if err = doc.Validate(); err != nil {
		return false, err
	}
	return configExists, nil
}

// resolveVars returns the config's declared vars with their defaults,
// overridden by overrides. Overriding an undeclared var is an error so typos
// in --var flags don't go unnoticed.
func resolveVars(doc *Document, overrides map[string]string, config string, configExists bool) (map[string]string, error) {
	vars := make(map[string]string, len(doc.Vars))
	for _, v := range doc.Vars {
		vars[v.Name] = v.Default
	}
	for name, value := range overrides {
		if _, ok := vars[name]; !ok {
			if configExists {
				return nil, fmt.Errorf("var not declared in %s: %s", config, name)
			}
			return nil, fmt.Errorf("var must be declared in a %s file: %s", config, name)
		}
		vars[name] = value
	}
	return vars, nil
}

// allowedFindingsProperties returns the set of allowed finding property
// names, or nil if the config doesn't restrict them.
func (doc *Document) allowedFindingsProperties() map[string]bool {
	if doc.AllowedFindingsProperties == nil {
		return nil
	}
	props := make(map[string]bool, len(doc.AllowedFindingsProperties.Properties))
	for _, p := range doc.AllowedFindingsProperties.Properties {
		props[p.Name] = true
	}
	return props
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
	doc := Document{}
	configExists, err := loadDocument(root, config, &doc)
	if err != nil {
		return err
	}

	resolvedPaths, err := resolvePaths(o.Files, root)
	if err != nil {
		return err
	}

	scm, err := getSCM(ctx, root, o.AllFiles)
	if err != nil {
		return err
	}
	scm = &cachingSCM{scm: scm}
	if len(o.Files) > 0 {
		scm = &specifiedFilesOnly{
			paths: resolvedPaths,
			root:  root,
			base:  scm,
		}
	}
	if len(o.Stdin) > 0 && len(o.Files) == 1 {
		relPath := resolvedPaths[0]
		if isDirPath(relPath) {
			return fmt.Errorf("is a directory: %s", o.Files[0])
		}
		// Make a scm that is for just the one in-memory file
		scm = &inMemoryFile{root: root, targetFile: &fileImpl{path: relPath}, data: o.Stdin, base: scm}
	}

	var matcher gitignore.Matcher
	if len(doc.Ignore) > 0 {
		var patterns []gitignore.Pattern
		for _, p := range doc.Ignore {
			if p == "" {
				return errEmptyIgnore
			}
			patterns = append(patterns, gitignore.ParsePattern(p, nil))
		}
		matcher = gitignore.NewMatcher(patterns)
		var exemptPaths []string
		for _, p := range resolvedPaths {
			if p == "" {
				continue
			}
			// Only exempt a directory argument if the directory itself matches
			// an ignore pattern, so passing an unignored parent directory (e.g.
			// "." or "a/") does not exempt ignored subdirectories inside it.
			if !isDirPath(p) || matcher.Match(strings.Split(strings.TrimSuffix(p, "/"), "/"), true) {
				exemptPaths = append(exemptPaths, p)
			}
		}
		scm = &filteredSCM{
			matcher:     matcher,
			exemptPaths: exemptPaths,
			scm:         scm,
		}
	}

	// Always cache the SCM to avoid recomputing the same values multiple times.
	scm = &cachingSCM{scm: scm}

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

	subprocessSem := semaphore.NewWeighted(int64(maxConcurrency))

	var vars map[string]string

	newState := func(scm scmCheckout, subdir string, idx int) (*shacState, error) {
		// Lazy-load vars only once a shac.star file is detected, so that errors
		// about missing shac.star files are prioritized over var validation
		// errors.
		if vars == nil {
			resolved, varsErr := resolveVars(&doc, o.Vars, config, configExists)
			if varsErr != nil {
				return nil, varsErr
			}
			vars = resolved
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
			shacConfig: shacConfig{
				allFiles:                  o.AllFiles,
				allowNetwork:              doc.AllowNetwork,
				env:                       &env,
				filter:                    o.Filter,
				entryPoint:                entryPoint,
				r:                         o.Report,
				root:                      root,
				sandbox:                   sb,
				scm:                       scm,
				subdir:                    subdir,
				subprocessSem:             subprocessSem,
				tmpdir:                    filepath.Join(tmpdir, strconv.Itoa(idx)),
				writableRoot:              doc.WritableRoot,
				vars:                      vars,
				passthroughEnv:            doc.PassthroughEnv,
				allowedFindingsProperties: doc.allowedFindingsProperties(),
			},
		}, nil
	}
	var shacStates []*shacState
	if o.Recurse {
		// Each found shac.star is run in its own interpreter for maximum
		// parallelism.
		// Discover all the main files via the SCM. This enables us to not walk
		// ignored files.
		subdirs, err := shacFileDirs(ctx, scm, entryPoint)
		if err != nil {
			return err
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

	// Parse the starlark files concurrently.
	eg, egCtx := errgroup.WithContext(ctx)
	eg.SetLimit(maxConcurrency)
	for _, s := range shacStates {
		eg.Go(func() error {
			stateCtx := context.WithValue(egCtx, &shacStateCtxKey, s)
			if err := s.parse(stateCtx); err != nil {
				return err
			}
			if len(s.checks) == 0 && !s.printCalled {
				return errors.New("did you forget to call shac.register_check?")
			}
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return err
	}

	if err := o.Filter.validate(shacStates); err != nil {
		return err
	}

	var hasChecksAfterFiltering bool
	var totalChecks int
	for _, s := range shacStates {
		totalChecks += len(s.checks)
		checks, err := s.filter.filter(s.checks)
		if err != nil {
			return err
		}
		s.checks = checks
		if len(s.checks) > 0 {
			hasChecksAfterFiltering = true
		}
	}
	if totalChecks > 0 && !hasChecksAfterFiltering {
		return errors.New("no checks to run")
	}

	// Run all checks concurrently honoring the CPU limit.
	eg, egCtx = errgroup.WithContext(ctx)
	eg.SetLimit(maxConcurrency)

	for _, s := range shacStates {
		shacCtx, err := getCtx(path.Join(s.root, s.subdir), s.vars)
		if err != nil {
			return err
		}
		args := starlark.Tuple{shacCtx}
		args.Freeze()
		for _, check := range s.checks {
			eg.Go(func() error {
				stateCtx := context.WithValue(egCtx, &shacStateCtxKey, s)
				start := time.Now()
				pi := func(th *starlark.Thread, msg string) {
					pos := th.CallFrame(1).Pos
					s.r.Print(stateCtx, check.name, pos.Filename(), int(pos.Line), msg)
				}
				err := check.call(stateCtx, s.env, args, pi)
				if err != nil && stateCtx.Err() != nil {
					// Don't report the check completion if the context was
					// canceled. The error was probably caused by the context
					// being canceled as a side effect of another check failing.
					// Only the original check failure should be reported, not
					// the canceled check failures.
					return stateCtx.Err()
				}
				s.r.CheckCompleted(stateCtx, check.name, start, time.Since(start), check.highestLevel, err)
				return err
			})
		}
	}
	if err := eg.Wait(); err != nil {
		return err
	}

	if err := warnIfNoAffectedFilesInDirs(ctx, o, scm, resolvedPaths); err != nil {
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

// warnIfNoAffectedFilesInDirs logs a warning to stderr when directory arguments
// were passed without --all and none of them contain any affected files, so
// that `shac check <dir>` on a clean directory is not a silent no-op.
func warnIfNoAffectedFilesInDirs(ctx context.Context, o *Options, scm scmCheckout, resolvedPaths []string) error {
	if o.AllFiles || len(o.Stdin) > 0 {
		return nil
	}
	if fc, ok := o.Report.(*findingCollector); ok && (fc.quiet || fc.rerun) {
		return nil
	}
	var numDirs int
	for _, spec := range resolvedPaths {
		if isDirPath(spec) {
			numDirs++
		}
	}
	if numDirs == 0 {
		return nil
	}
	affected, err := scm.affectedFiles(ctx, fileFilter{})
	if err != nil {
		return err
	}
	hasAffected := slices.ContainsFunc(affected, func(af file) bool {
		return slices.ContainsFunc(resolvedPaths, func(spec string) bool {
			return isDirPath(spec) && strings.HasPrefix(af.rootedpath(), spec)
		})
	})
	if !hasAffected {
		noun := "directory"
		if numDirs > 1 {
			noun = "directories"
		}
		fmt.Fprintf(os.Stderr, "WARNING: No affected files in the specified %s; pass --all to analyze all files in the %s\n", noun, noun)
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
	if cfgFi, statErr := os.Stat(filepath.Join(dir, "shac.textproto")); statErr == nil && !cfgFi.IsDir() {
		return strings.ReplaceAll(filepath.Clean(dir), string(os.PathSeparator), "/"), nil
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

// resolvePaths makes all the file and directory paths relative to the project
// root, sorts, and removes duplicates. Directory paths end with "/" (or are ""
// for the project root).
//
// Input paths may be absolute or relative. If relative, they are assumed to be
// relative to the current working directory.
func resolvePaths(files []string, root string) ([]string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	var resolvedPaths []string
	for _, orig := range files {
		f := orig
		if !filepath.IsAbs(f) {
			f = filepath.Join(cwd, f)
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

		f, err = filepath.EvalSymlinks(f)
		if err != nil {
			return nil, err
		}

		rel, err := filepath.Rel(resolvedRoot, f)
		if err != nil {
			return nil, err
		}
		// Validates that the path is within the root directory (i.e.
		// doesn't start with "..").
		if !filepath.IsLocal(rel) {
			return nil, fmt.Errorf("cannot analyze file outside root: %s", orig)
		}

		relPath := filepath.ToSlash(rel)
		if relPath == "." {
			relPath = ""
		} else if fi.IsDir() {
			// Trailing slashes help downstream code identify directories and
			// prevent prefix matching from accidentally matching sibling files
			// with similar names (e.g. matching "foo/bar_test.go" when
			// filtering for directory "foo/bar").
			relPath += "/"
		}
		resolvedPaths = append(resolvedPaths, relPath)
	}

	slices.Sort(resolvedPaths)
	resolvedPaths = slices.Compact(resolvedPaths)

	return resolvedPaths, nil
}

// isDirPath reports whether a root-relative path returned by resolvePaths
// refers to a directory ("" for the root directory, or a "/" suffix for a
// subdirectory).
func isDirPath(p string) bool {
	return p == "" || strings.HasSuffix(p, "/")
}

// matchesPathSpecs reports whether root-relative file path p matches any entry
// in specs (either an exact file match or a file within a directory spec).
func matchesPathSpecs(specs []string, p string) bool {
	for _, spec := range specs {
		if p == spec || (isDirPath(spec) && strings.HasPrefix(p, spec)) {
			return true
		}
	}
	return false
}

type execHandlerFunc func(ctx context.Context, cmd []string, raiseOnFailure bool, okRetcodes []int, tempDir string) (*subprocess, bool, error)

// shacConfig holds immutable configuration for a shacState.
type shacConfig struct {
	env          *starlarkEnv
	r            Report
	allowNetwork bool
	writableRoot bool
	// allFiles is whether shac was run with --all, in which case all files are
	// considered affected.
	allFiles   bool
	entryPoint string
	// root is the root for the root shac.star that was executed. Native path
	// style.
	root string
	// realRoot is the underlying repository root on disk when running a test
	// against a virtualized root directory. Native path style.
	realRoot string
	// extraMounts holds resolved symlink targets outside realRoot (e.g. from a
	// Bazel runfiles tree) that must be mounted into the sandbox during tests.
	extraMounts []sandbox.Mount
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
	// filter controls which checks run. If nil, all checks will run.
	filter         CheckFilter
	passthroughEnv []*PassthroughEnv

	// allowedFindingsProperties is a map of the allowed property names for shac results.
	allowedFindingsProperties map[string]bool

	// Limits the number of concurrent subprocesses launched by ctx.os.exec().
	subprocessSem *semaphore.Weighted

	// execHandler optionally intercepts ctx.os.exec() calls during testing.
	execHandler execHandlerFunc

	// forbidRegisterCheck is set on the state used to load and run a
	// `*_test.star` file, where checks registered by the test file itself
	// would otherwise be silently dropped because only testing.run() ever
	// executes checks.
	forbidRegisterCheck bool
}

// shacState represents a parsing state of one shac.star.
type shacState struct {
	shacConfig

	// checks is the list of registered checks callbacks via
	// shac.register_check().
	//
	// Checks are added serially, so no lock is needed.
	//
	// Checks are executed sequentially after all Starlark code is loaded and not
	// mutated. They run checks and emit results (results and comments).
	checks []*registeredCheck

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
		if evalErr, ok := errors.AsType[*starlark.EvalError](err); ok {
			return &evalError{evalErr}
		}
		return err
	}
	s.doneLoading = true
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
		if evalErr, ok := errors.AsType[*starlark.EvalError](err); ok {
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
