// Copyright (c) 2020 Uber Technologies, Inc.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package exec

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/protoc-gen-go/descriptor"
	"github.com/uber/prototool/internal/breaking"
	"github.com/uber/prototool/internal/cfginit"
	"github.com/uber/prototool/internal/create"
	"github.com/uber/prototool/internal/desc"
	"github.com/uber/prototool/internal/extract"
	"github.com/uber/prototool/internal/file"
	"github.com/uber/prototool/internal/git"
	"github.com/uber/prototool/internal/protoc"
	"github.com/uber/prototool/internal/reflect"
	"github.com/uber/prototool/internal/settings"
	"github.com/uber/prototool/internal/text"
	"github.com/uber/prototool/internal/vars"
	"go.uber.org/multierr"
	"go.uber.org/zap"
)

var jsonpbMarshaler = &jsonpb.Marshaler{}

type runner struct {
	protoSetProvider file.ProtoSetProvider

	workDirPath string
	input       io.Reader
	output      io.Writer

	logger        *zap.Logger
	develMode     bool
	cachePath     string
	configData    string
	protocBinPath string
	protocWKTPath string
	protocURL     string
	errorFormat   string
	json          bool
	walkTimeout   time.Duration
}

func newRunner(workDirPath string, input io.Reader, output io.Writer, options ...RunnerOption) *runner {
	runner := &runner{
		workDirPath: workDirPath,
		input:       input,
		output:      output,
	}
	for _, option := range options {
		option(runner)
	}
	protoSetProviderOptions := []file.ProtoSetProviderOption{
		file.ProtoSetProviderWithLogger(runner.logger),
		file.ProtoSetProviderWithWalkTimeout(runner.walkTimeout),
	}
	if runner.configData != "" {
		protoSetProviderOptions = append(
			protoSetProviderOptions,
			file.ProtoSetProviderWithConfigData(runner.configData),
		)
	}
	if runner.develMode {
		protoSetProviderOptions = append(
			protoSetProviderOptions,
			file.ProtoSetProviderWithDevelMode(),
		)
	}
	runner.protoSetProvider = file.NewProtoSetProvider(protoSetProviderOptions...)
	return runner
}

func (r *runner) cloneForWorkDirPath(workDirPath string) *runner {
	return &runner{
		protoSetProvider: r.protoSetProvider,
		workDirPath:      workDirPath,
		input:            r.input,
		output:           r.output,
		logger:           r.logger,
		cachePath:        r.cachePath,
		configData:       r.configData,
		protocBinPath:    r.protocBinPath,
		protocWKTPath:    r.protocWKTPath,
		protocURL:        r.protocURL,
		errorFormat:      r.errorFormat,
		json:             r.json,
	}
}

func (r *runner) Version() error {
	out := struct {
		Version              string `json:"version,omitempty"`
		DefaultProtocVersion string `json:"default_protoc_version,omitempty"`
		GoVersion            string `json:"go_version,omitempty"`
		GOOS                 string `json:"goos,omitempty"`
		GOARCH               string `json:"goarch,omitempty"`
	}{
		Version:              vars.Version,
		DefaultProtocVersion: vars.DefaultProtocVersion,
		GoVersion:            runtime.Version(),
		GOOS:                 runtime.GOOS,
		GOARCH:               runtime.GOARCH,
	}

	if r.json {
		enc := json.NewEncoder(r.output)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}

	tabWriter := newTabWriter(r.output)
	if _, err := fmt.Fprintf(tabWriter, "Version:\t%s\n", out.Version); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(tabWriter, "Default protoc version:\t%s\n", out.DefaultProtocVersion); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(tabWriter, "Go version:\t%s\n", out.GoVersion); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(tabWriter, "OS/Arch:\t%s/%s\n", out.GOOS, out.GOARCH); err != nil {
		return err
	}
	return tabWriter.Flush()
}

func (r *runner) Init(args []string, uncomment bool, document bool) error {
	if len(args) > 1 {
		return errors.New("must provide one arg dirPath")
	}
	// TODO(pedge): cleanup
	dirPath := r.workDirPath
	if len(args) == 1 {
		dirPath = args[0]
		if err := os.MkdirAll(dirPath, 0755); err != nil {
			return err
		}
	}
	filePath := filepath.Join(dirPath, settings.DefaultConfigFilename)
	if _, err := os.Stat(filePath); err == nil {
		return fmt.Errorf("%s already exists", filePath)
	}
	data, err := cfginit.Generate(vars.DefaultProtocVersion, uncomment, document)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(filePath, data, 0644)
}

func (r *runner) Create(args []string, pkg string) error {
	return r.newCreateHandler(pkg).Create(args...)
}

func (r *runner) CacheUpdate(args []string) error {
	meta, err := r.getMeta(args)
	if err != nil {
		return err
	}
	d, err := r.newDownloader(meta.ProtoSet.Config)
	if err != nil {
		return err
	}
	_, err = d.Download()
	return err
}

func (r *runner) CacheDelete() error {
	meta, err := r.getMeta(nil)
	if err != nil {
		return err
	}
	// TODO: do not need config for delete, refactor
	d, err := r.newDownloader(meta.ProtoSet.Config)
	if err != nil {
		return err
	}
	return d.Delete()
}

func (r *runner) Files(args []string) error {
	meta, err := r.getMeta(args)
	if err != nil {
		return err
	}
	var allFiles []string
	for dirPath, files := range meta.ProtoSet.DirPathToFiles {
		// skip those files not under the directory
		if !strings.HasPrefix(dirPath, meta.ProtoSet.DirPath) {
			continue
		}
		for _, file := range files {
			allFiles = append(allFiles, file.DisplayPath)
		}
	}
	sort.Strings(allFiles)
	for _, file := range allFiles {
		if err := r.println(file); err != nil {
			return err
		}
	}
	return nil
}

func (r *runner) Compile(args []string, dryRun bool) error {
	meta, err := r.getMeta(args)
	if err != nil {
		return err
	}
	r.printAffectedFiles(meta)
	_, err = r.compile(false, false, dryRun, meta)
	return err
}

func (r *runner) Gen(args []string, dryRun bool) error {
	meta, err := r.getMeta(args)
	if err != nil {
		return err
	}
	r.printAffectedFiles(meta)
	_, err = r.compile(true, false, dryRun, meta)
	return err
}

func (r *runner) compile(doGen bool, doFileDescriptorSet bool, dryRun bool, meta *meta) (protoc.FileDescriptorSets, error) {
	if dryRun {
		doFileDescriptorSet = false
	}
	compiler, err := r.newCompiler(doGen, doFileDescriptorSet, false, false, false)
	if err != nil {
		return nil, err
	}
	if dryRun {
		return nil, r.doProtocCommands(compiler, meta)
	}
	return r.doCompile(compiler, meta)
}

func (r *runner) compileFullControl(includeImports bool, includeSourceInfo bool, meta *meta) (protoc.FileDescriptorSets, error) {
	compiler, err := r.newCompiler(false, false, true, includeImports, includeSourceInfo)
	if err != nil {
		return nil, err
	}
	return r.doCompile(compiler, meta)
}

func (r *runner) doCompile(compiler protoc.Compiler, meta *meta) (protoc.FileDescriptorSets, error) {
	compileResult, err := compiler.Compile(meta.ProtoSet)
	if err != nil {
		return nil, err
	}
	if err := r.printFailures("", meta, compileResult.Failures...); err != nil {
		return nil, err
	}
	if len(compileResult.Failures) > 0 {
		return nil, newExitErrorf(255, "")
	}
	return compileResult.FileDescriptorSets, nil
}

func (r *runner) doProtocCommands(compiler protoc.Compiler, meta *meta) error {
	commands, err := compiler.ProtocCommands(meta.ProtoSet)
	if err != nil {
		return err
	}
	for _, command := range commands {
		if err := r.println(command); err != nil {
			return err
		}
	}
	return nil
}

func (r *runner) All(args []string, disableFormat, disableLint, fixFlag bool) error {
	meta, err := r.getMeta(args)
	if err != nil {
		return err
	}

	r.printAffectedFiles(meta)
	if _, err := r.compile(false, false, false, meta); err != nil {
		return err
	}

	return nil
}

func (r *runner) BreakDescriptorSet(args []string, outputPath string) error {
	if outputPath == "" {
		return newExitErrorf(255, "must set output-path")
	}
	return r.DescriptorSet(args, true, false, outputPath, false)
}

func (r *runner) DescriptorSet(args []string, includeImports bool, includeSourceInfo bool, outputPath string, tmp bool) (retErr error) {
	if outputPath != "" && tmp {
		return newExitErrorf(255, "can only set one of output-path, tmp")
	}
	meta, err := r.getMeta(args)
	if err != nil {
		return err
	}
	r.printAffectedFiles(meta)
	fileDescriptorSets, err := r.compileFullControl(includeImports, includeSourceInfo, meta)
	if err != nil {
		return err
	}
	fileDescriptorSet, err := desc.MergeFileDescriptorSets(fileDescriptorSets.Unwrap())
	if err != nil {
		return err
	}
	var data []byte
	if r.json {
		buffer := bytes.NewBuffer(nil)
		err = jsonpbMarshaler.Marshal(buffer, fileDescriptorSet)
		data = buffer.Bytes()
	} else {
		data, err = proto.Marshal(fileDescriptorSet)
	}
	if err != nil {
		return err
	}
	if outputPath == "" && !tmp {
		_, err := r.output.Write(data)
		return err
	}
	var file *os.File
	if outputPath != "" {
		file, err = os.Create(outputPath)
	} else { // if tmp
		file, err = ioutil.TempFile("", "prototool")
	}
	if err != nil {
		return err
	}
	defer func() {
		retErr = multierr.Append(retErr, file.Close())
	}()
	if _, err := file.Write(data); err != nil {
		return err
	}
	if tmp {
		if err := r.println(file.Name()); err != nil {
			return err
		}
	}
	return nil
}

func (r *runner) InspectPackages(args []string) error {
	packageSet, _, err := r.getPackageSetAndConfig(args)
	if err != nil {
		return err
	}
	if packageSet == nil {
		return nil
	}
	return r.printPackageNames(packageSet.PackageNameToPackage())
}

func (r *runner) InspectPackageDeps(args []string, name string) error {
	pkg, err := r.getPackage(args, name)
	if err != nil {
		return err
	}
	return r.printPackageNames(pkg.DependencyNameToDependency())
}

func (r *runner) InspectPackageImporters(args []string, name string) error {
	pkg, err := r.getPackage(args, name)
	if err != nil {
		return err
	}
	return r.printPackageNames(pkg.ImporterNameToImporter())
}

func (r *runner) BreakCheck(args []string, gitBranch string, descriptorSetPath string) error {
	if gitBranch != "" && descriptorSetPath != "" {
		return newExitErrorf(255, "can only set one of git-branch, descriptor-set-path")
	}

	toPackageSet, config, err := r.getPackageSetAndConfig(args)
	if err != nil {
		return err
	}

	var fromPackageSet *extract.PackageSet
	if descriptorSetPath != "" {
		fromPackageSet, err = r.getPackageSetForDescriptorSetPath(descriptorSetPath)
		if err != nil {
			return err
		}
	} else {
		relDirPath := "."
		// we check length 0 or 1 in cmd, similar to other commands
		if len(args) == 1 {
			relDirPath = args[0]
		}
		if filepath.IsAbs(relDirPath) {
			return fmt.Errorf("input argument must be relative directory path: %s", relDirPath)
		}

		absDirPath, err := file.AbsClean(relDirPath)
		if err != nil {
			return err
		}
		absWorkDirPath, err := file.AbsClean(r.workDirPath)
		if err != nil {
			return err
		}
		if !strings.HasPrefix(absDirPath, absWorkDirPath) {
			return fmt.Errorf("input directory must be within working directory: %s", relDirPath)
		}

		// this will purposefully fail if we are not at a git repository
		cloneDirPath, err := git.TemporaryClone(r.logger, r.workDirPath, gitBranch)
		if err != nil {
			return err
		}
		defer func() {
			r.logger.Sugar().Debugf("removing %s", cloneDirPath)
			_ = os.RemoveAll(cloneDirPath)
		}()

		fromPackageSet, _, err = r.cloneForWorkDirPath(cloneDirPath).getPackageSetAndConfigForRelDirPath(relDirPath)
		if err != nil {
			return err
		}
	}

	failures, err := r.newBreakingRunner().Run(config.Break, fromPackageSet, toPackageSet)
	if err != nil {
		return err
	}
	if len(failures) > 0 {
		if err := r.printFailuresForErrorFormat("message", "", nil, failures...); err != nil {
			return err
		}
		return newExitErrorf(255, "")
	}
	return nil
}

func (r *runner) getPackageSetAndConfig(args []string) (*extract.PackageSet, settings.Config, error) {
	meta, err := r.getMeta(args)
	if err != nil {
		return nil, settings.Config{}, err
	}
	r.printAffectedFiles(meta)
	fileDescriptorSets, err := r.compile(false, true, false, meta)
	if err != nil {
		return nil, settings.Config{}, err
	}
	packageSet, err := r.getPackageSetForFileDescriptorSets(fileDescriptorSets.Unwrap()...)
	if err != nil {
		return nil, settings.Config{}, err
	}
	var config settings.Config
	if meta != nil && meta.ProtoSet != nil {
		config = meta.ProtoSet.Config
	}
	return packageSet, config, nil
}

func (r *runner) getPackageSetForDescriptorSetPath(descriptorSetPath string) (*extract.PackageSet, error) {
	data, err := ioutil.ReadFile(descriptorSetPath)
	if err != nil {
		return nil, err
	}
	fileDescriptorSet := &descriptor.FileDescriptorSet{}
	if err := proto.Unmarshal(data, fileDescriptorSet); err != nil {
		return nil, err
	}
	return r.getPackageSetForFileDescriptorSets(fileDescriptorSet)
}

func (r *runner) getPackageSetForFileDescriptorSets(fileDescriptorSets ...*descriptor.FileDescriptorSet) (*extract.PackageSet, error) {
	reflectPackageSet, err := reflect.NewPackageSet(fileDescriptorSets...)
	if err != nil {
		return nil, err
	}
	return extract.NewPackageSet(reflectPackageSet)
}

func (r *runner) getPackage(args []string, name string) (*extract.Package, error) {
	if name == "" {
		return nil, newExitErrorf(255, "must set name")
	}
	packageSet, _, err := r.getPackageSetAndConfig(args)
	if err != nil {
		return nil, err
	}
	if packageSet == nil {
		return nil, fmt.Errorf("package not found: %s", name)
	}
	pkg, ok := packageSet.PackageNameToPackage()[name]
	if !ok {
		return nil, fmt.Errorf("package not found: %s", name)
	}
	return pkg, nil
}

func (r *runner) printPackageNames(m map[string]*extract.Package) error {
	for _, packageName := range extractSortPackageNames(m) {
		if err := r.println(packageName); err != nil {
			return err
		}
	}
	return nil
}

// we require a relative path (or no path) to be passed
// this is largely because getMeta has special handling for "."
func (r *runner) getPackageSetAndConfigForRelDirPath(relDirPath string) (*extract.PackageSet, settings.Config, error) {
	dirPath := r.workDirPath
	if relDirPath != "" && relDirPath != "." {
		dirPath = filepath.Join(dirPath, relDirPath)
	}
	return r.getPackageSetAndConfig([]string{dirPath})
}

func (r *runner) newBreakingRunner() breaking.Runner {
	runnerOptions := []breaking.RunnerOption{
		breaking.RunnerWithLogger(r.logger),
	}
	return breaking.NewRunner(runnerOptions...)
}

func (r *runner) newDownloader(config settings.Config) (protoc.Downloader, error) {
	downloaderOptions := []protoc.DownloaderOption{
		protoc.DownloaderWithLogger(r.logger),
	}
	if r.cachePath != "" {
		downloaderOptions = append(
			downloaderOptions,
			protoc.DownloaderWithCachePath(r.cachePath),
		)
	}
	if r.protocBinPath != "" {
		downloaderOptions = append(
			downloaderOptions,
			protoc.DownloaderWithProtocBinPath(r.protocBinPath),
		)
	}
	if r.protocWKTPath != "" {
		downloaderOptions = append(
			downloaderOptions,
			protoc.DownloaderWithProtocWKTPath(r.protocWKTPath),
		)
	}
	if r.protocURL != "" {
		downloaderOptions = append(
			downloaderOptions,
			protoc.DownloaderWithProtocURL(r.protocURL),
		)
	}
	return protoc.NewDownloader(config, downloaderOptions...)
}

func (r *runner) newCompiler(
	doGen bool,
	doFileDescriptorSet bool,
	doFileDescriptorSetFullControl bool,
	includeImports bool,
	includeSourceInfo bool,
) (protoc.Compiler, error) {
	if doFileDescriptorSet && doFileDescriptorSetFullControl {
		return nil, fmt.Errorf("cannot set doFileDescriptorSet and doFileDescriptorSetFullControl")
	}
	if !doFileDescriptorSetFullControl {
		if includeImports || includeSourceInfo {
			return nil, fmt.Errorf("cannot set includeImports or includeSourceInfo without doFileDescriptorSetFullControl")
		}
	}
	compilerOptions := []protoc.CompilerOption{
		protoc.CompilerWithLogger(r.logger),
	}
	if r.cachePath != "" {
		compilerOptions = append(
			compilerOptions,
			protoc.CompilerWithCachePath(r.cachePath),
		)
	}
	if r.protocBinPath != "" {
		compilerOptions = append(
			compilerOptions,
			protoc.CompilerWithProtocBinPath(r.protocBinPath),
		)
	}
	if r.protocWKTPath != "" {
		compilerOptions = append(
			compilerOptions,
			protoc.CompilerWithProtocWKTPath(r.protocWKTPath),
		)
	}
	if r.protocURL != "" {
		compilerOptions = append(
			compilerOptions,
			protoc.CompilerWithProtocURL(r.protocURL),
		)
	}
	if doGen {
		compilerOptions = append(
			compilerOptions,
			protoc.CompilerWithGen(),
		)
	}
	if doFileDescriptorSet {
		compilerOptions = append(
			compilerOptions,
			protoc.CompilerWithFileDescriptorSet(),
		)
	}
	if doFileDescriptorSetFullControl {
		compilerOptions = append(
			compilerOptions,
			protoc.CompilerWithFileDescriptorSetFullControl(includeImports, includeSourceInfo),
		)
	}
	return protoc.NewCompiler(compilerOptions...), nil
}

func (r *runner) newCreateHandler(pkg string) create.Handler {
	handlerOptions := []create.HandlerOption{create.HandlerWithLogger(r.logger)}
	if pkg != "" {
		handlerOptions = append(handlerOptions, create.HandlerWithPackage(pkg))
	}
	if r.develMode {
		handlerOptions = append(handlerOptions, create.HandlerWithDevelMode())
	}
	if r.configData != "" {
		handlerOptions = append(
			handlerOptions,
			create.HandlerWithConfigData(r.configData),
		)
	}
	return create.NewHandler(handlerOptions...)
}

type meta struct {
	ProtoSet *file.ProtoSet
	// this will be empty if not in dir mode
	// if in dir mode, this will be the single filename that we want to return errors for
	SingleFilename string
}

func (r *runner) getMeta(args []string) (*meta, error) {
	// TODO: does not fit in with workDirPath paradigm
	fileOrDir := "."
	if len(args) == 1 {
		fileOrDir = args[0]
	}
	fileInfo, err := os.Stat(fileOrDir)
	if err != nil {
		return nil, err
	}
	if fileInfo.Mode().IsDir() {
		protoSet, err := r.protoSetProvider.GetForDir(r.workDirPath, fileOrDir)
		if err != nil {
			return nil, err
		}
		return &meta{
			ProtoSet: protoSet,
		}, nil
	}
	// TODO: allow symlinks?
	if fileInfo.Mode().IsRegular() {
		protoSet, err := r.protoSetProvider.GetForDir(r.workDirPath, filepath.Dir(fileOrDir))
		if err != nil {
			return nil, err
		}
		return &meta{
			ProtoSet:       protoSet,
			SingleFilename: fileOrDir,
		}, nil
	}
	return nil, fmt.Errorf("%s is not a directory or a regular file", fileOrDir)
}

// TODO: we filter failures in dir mode in printFailures but above we count any failure
// as an error with a non-zero exit code, seems inconsistent, this needs refactoring

// filename is optional
// meta is optional
// if set, it will update the Failures to have this filename
// will be sorted
func (r *runner) printFailures(filename string, meta *meta, failures ...*text.Failure) error {
	return r.printFailuresForErrorFormat(r.errorFormat, filename, meta, failures...)
}

func (r *runner) printFailuresForErrorFormat(errorFormat string, filename string, meta *meta, failures ...*text.Failure) error {
	for _, failure := range failures {
		if filename != "" {
			failure.Filename = filename
		}
	}
	failureFields, err := text.ParseColonSeparatedFailureFields(errorFormat)
	if err != nil {
		return err
	}
	text.SortFailures(failures)
	bufWriter := bufio.NewWriter(r.output)
	for _, failure := range failures {
		shouldPrint := false
		if meta != nil {
			if meta.SingleFilename == "" || meta.SingleFilename == failure.Filename {
				shouldPrint = true
			} else if meta.SingleFilename != "" {
				// TODO: the compiler may not return the rel path due to logic in bestFilePath
				absSingleFilename, err := file.AbsClean(meta.SingleFilename)
				if err != nil {
					return err
				}
				absFailureFilename, err := file.AbsClean(failure.Filename)
				if err != nil {
					return err
				}
				if absSingleFilename == absFailureFilename {
					shouldPrint = true
				}
			}
		} else {
			shouldPrint = true
		}
		if shouldPrint {
			if r.json {
				data, err := json.Marshal(failure)
				if err != nil {
					return err
				}
				if _, err := fmt.Fprintln(bufWriter, string(data)); err != nil {
					return err
				}
			} else if err := failure.Fprintln(bufWriter, failureFields...); err != nil {
				return err
			}
		}
	}
	return bufWriter.Flush()
}

func (r *runner) printAffectedFiles(meta *meta) {
	for dirPath, files := range meta.ProtoSet.DirPathToFiles {
		// skip those files not under the directory
		if !strings.HasPrefix(dirPath, meta.ProtoSet.DirPath) {
			continue
		}
		for _, file := range files {
			r.logger.Debug("using file", zap.String("file", file.DisplayPath))
		}
	}
}

func (r *runner) println(s string) error {
	if s == "" {
		return nil
	}
	_, err := fmt.Fprintln(r.output, s)
	return err
}

func newExitErrorf(code int, format string, args ...interface{}) *ExitError {
	return &ExitError{
		Code:    code,
		Message: fmt.Sprintf(format, args...),
	}
}

func newTabWriter(writer io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(writer, 0, 0, 2, ' ', 0)
}

func extractSortPackageNames(m map[string]*extract.Package) []string {
	s := make([]string, 0, len(m))
	for key := range m {
		if key != "" {
			s = append(s, key)
		}
	}
	sort.Strings(s)
	return s
}
