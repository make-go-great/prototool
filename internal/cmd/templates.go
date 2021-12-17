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

package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	wordwrap "github.com/mitchellh/go-wordwrap"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/uber/prototool/internal/exec"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const wordWrapLength uint = 80

var (
	allCmdTemplate = &cmdTemplate{
		Use:   "all [dirOrFile]",
		Short: "Compile, then format and overwrite, then re-compile and generate, then lint, stopping if any step fails.",
		Args:  cobra.MaximumNArgs(1),
		Run: func(runner exec.Runner, args []string, flags *flags) error {
			return runner.All(args, flags.disableFormat, flags.disableLint, flags.fix)
		},
		BindFlags: func(flagSet *pflag.FlagSet, flags *flags) {
			flags.bindCachePath(flagSet)
			flags.bindConfigData(flagSet)
			flags.bindDisableFormat(flagSet)
			flags.bindDisableLint(flagSet)
			flags.bindErrorFormat(flagSet)
			flags.bindJSON(flagSet)
			flags.bindFix(flagSet)
			flags.bindProtocURL(flagSet)
			flags.bindProtocBinPath(flagSet)
			flags.bindProtocWKTPath(flagSet)
			flags.bindWalkTimeout(flagSet)
		},
	}

	cacheUpdateCmdTemplate = &cmdTemplate{
		Use:   "update [dirOrFile]",
		Short: "Update the cache by downloading all artifacts.",
		Long: `This will download artifacts to a cache directory before running any commands. Note that calling this command is not necessary, all artifacts are automatically downloaded when required by other commands. This just provides a mechanism to pre-cache artifacts during your build.

Artifacts are downloaded to the following directories based on flags and environment variables:

- If --cache-path is set, then this directory will be used. The user is
  expected to manually manage this directory, and the "delete" subcommand
  will have no effect on it.
- Otherwise, if $PROTOTOOL_CACHE_PATH is set, then this directory will be used.
  The user is expected to manually manage this directory, and the "delete"
  subcommand will have no effect on it.
- Otherwise, if $XDG_CACHE_HOME is set, then $XDG_CACHE_HOME/prototool
  will be used.
- Otherwise, if on Linux, $HOME/.cache/prototool will be used, or on Darwin,
  $HOME/Library/Caches/prototool will be used.`,
		Args: cobra.MaximumNArgs(1),
		Run: func(runner exec.Runner, args []string, flags *flags) error {
			return runner.CacheUpdate(args)
		},
		BindFlags: func(flagSet *pflag.FlagSet, flags *flags) {
			flags.bindCachePath(flagSet)
			flags.bindConfigData(flagSet)
			flags.bindWalkTimeout(flagSet)
		},
	}

	cacheDeleteCmdTemplate = &cmdTemplate{
		Use:   "delete",
		Short: "Delete all artifacts in the default cache.",
		Long: `The following directory will be deleted based on environment variables:

- If $XDG_CACHE_HOME is set, then $XDG_CACHE_HOME/prototool will be deleted.
- Otherwise, if on Linux, $HOME/.cache/prototool will be deleted, or on Darwin,
  $HOME/Library/Caches/prototool will be deleted.

  This will not delete any custom caches created using the --cache-path flag or PROTOTOOL_CACHE_PATH environment variable.`,
		Args: cobra.NoArgs,
		Run: func(runner exec.Runner, args []string, flags *flags) error {
			return runner.CacheDelete()
		},
	}

	compileCmdTemplate = &cmdTemplate{
		Use:   "compile [dirOrFile]",
		Short: "Compile with protoc to check for failures.",
		Long:  `Stubs will not be generated. To generate stubs, use the "gen" command. Calling "compile" has the effect of calling protoc with "-o /dev/null".`,
		Args:  cobra.MaximumNArgs(1),
		Run: func(runner exec.Runner, args []string, flags *flags) error {
			return runner.Compile(args, flags.dryRun)
		},
		BindFlags: func(flagSet *pflag.FlagSet, flags *flags) {
			flags.bindCachePath(flagSet)
			flags.bindConfigData(flagSet)
			flags.bindDryRun(flagSet)
			flags.bindErrorFormat(flagSet)
			flags.bindJSON(flagSet)
			flags.bindProtocURL(flagSet)
			flags.bindProtocBinPath(flagSet)
			flags.bindProtocWKTPath(flagSet)
			flags.bindWalkTimeout(flagSet)
		},
	}

	filesCmdTemplate = &cmdTemplate{
		Use:   "files [dirOrFile]",
		Short: "Print all files that match the input arguments.",
		Args:  cobra.MaximumNArgs(1),
		Run: func(runner exec.Runner, args []string, flags *flags) error {
			return runner.Files(args)
		},
		BindFlags: func(flagSet *pflag.FlagSet, flags *flags) {
			flags.bindConfigData(flagSet)
			flags.bindWalkTimeout(flagSet)
		},
	}

	generateCmdTemplate = &cmdTemplate{
		Use:   "generate [dirOrFile]",
		Short: "Generate with protoc.",
		Args:  cobra.MaximumNArgs(1),
		Run: func(runner exec.Runner, args []string, flags *flags) error {
			return runner.Gen(args, flags.dryRun)
		},
		BindFlags: func(flagSet *pflag.FlagSet, flags *flags) {
			flags.bindCachePath(flagSet)
			flags.bindConfigData(flagSet)
			flags.bindDryRun(flagSet)
			flags.bindErrorFormat(flagSet)
			flags.bindJSON(flagSet)
			flags.bindProtocURL(flagSet)
			flags.bindProtocBinPath(flagSet)
			flags.bindProtocWKTPath(flagSet)
			flags.bindWalkTimeout(flagSet)
		},
	}

	configInitCmdTemplate = &cmdTemplate{
		Use:   "init [dirPath]",
		Short: "Generate an initial config file in the current or given directory.",
		Long:  `The currently recommended options will be set.`,
		Args:  cobra.MaximumNArgs(1),
		Run: func(runner exec.Runner, args []string, flags *flags) error {
			return runner.Init(args, flags.uncomment, flags.document)
		},
		BindFlags: func(flagSet *pflag.FlagSet, flags *flags) {
			flags.bindDocument(flagSet)
			flags.bindUncomment(flagSet)
		},
	}

	versionCmdTemplate = &cmdTemplate{
		Use:   "version",
		Short: "Print the version.",
		Args:  cobra.NoArgs,
		BindFlags: func(flagSet *pflag.FlagSet, flags *flags) {
			flags.bindJSON(flagSet)
		},
		Run: func(runner exec.Runner, args []string, flags *flags) error {
			return runner.Version()
		},
	}
)

// cmdTemplate contains the static parts of a cobra.Command such as
// documentation that we want to store outside of runtime creation.
//
// We do not just store cobra.Commands as in theory they have fields
// with types such as slices that if we were to return a blind copy,
// would mean that both the global cmdTemplate and the runtime
// cobra.Command would point to the same location. By making a new
// struct, we can also do more fancy templating things like prepending
// the Short description to the Long description for consistency, and
// have our own abstractions for the Run command.
type cmdTemplate struct {
	// Use is the one-line usage message.
	// This field is required.
	Use string
	// Short is the short description shown in the 'help' output.
	// This field is required.
	Short string
	// Long is the long message shown in the 'help <this-command>' output.
	// The Short field will be prepended to the Long field with a newline
	// when applied to a *cobra.Command.
	// This field is optional.
	Long string
	// Expected arguments.
	// This field is optional.
	Args cobra.PositionalArgs
	// Run is the command to run given an exec.Runner, args, and flags.
	// This field is required.
	Run func(exec.Runner, []string, *flags) error
	// BindFlags binds flags to the *pflag.FlagSet on Build.
	// There is no corollary to this on *cobra.Command.
	// This field is optional, although usually will be set.
	// We need to do this before run as the flags are populated
	// before Run is called.
	BindFlags func(*pflag.FlagSet, *flags)
}

// Build builds a *cobra.Command from the cmdTemplate.
func (c *cmdTemplate) Build(develMode bool, exitCodeAddr *int, stdin io.Reader, stdout io.Writer, stderr io.Writer, flags *flags) *cobra.Command {
	command := &cobra.Command{}
	command.Use = c.Use
	command.Short = strings.TrimSpace(c.Short)
	if c.Long != "" {
		command.Long = wordwrap.WrapString(fmt.Sprintf("%s\n\n%s", strings.TrimSpace(c.Short), strings.TrimSpace(c.Long)), wordWrapLength)
	}
	command.Args = c.Args
	command.Run = func(_ *cobra.Command, args []string) {
		checkCmd(develMode, exitCodeAddr, stdin, stdout, stderr, args, flags, c.Run)
	}
	if c.BindFlags != nil {
		c.BindFlags(command.PersistentFlags(), flags)
	}
	return command
}

func checkCmd(develMode bool, exitCodeAddr *int, stdin io.Reader, stdout io.Writer, stderr io.Writer, args []string, flags *flags, f func(exec.Runner, []string, *flags) error) {
	runner, err := getRunner(develMode, stdin, stdout, stderr, flags)
	if err != nil {
		*exitCodeAddr = printAndGetErrorExitCode(err, stdout)
		return
	}
	if err := f(runner, args, flags); err != nil {
		*exitCodeAddr = printAndGetErrorExitCode(err, stdout)
	}
}

func getRunner(develMode bool, stdin io.Reader, stdout io.Writer, stderr io.Writer, flags *flags) (exec.Runner, error) {
	logger, err := getLogger(stderr, flags.debug)
	if err != nil {
		return nil, err
	}
	runnerOptions := []exec.RunnerOption{
		exec.RunnerWithLogger(logger),
	}
	if flags.cachePath != "" {
		runnerOptions = append(
			runnerOptions,
			exec.RunnerWithCachePath(flags.cachePath),
		)
	} else if envCachePath := os.Getenv("PROTOTOOL_CACHE_PATH"); envCachePath != "" {
		runnerOptions = append(
			runnerOptions,
			exec.RunnerWithCachePath(envCachePath),
		)
	}
	if flags.configData != "" {
		runnerOptions = append(
			runnerOptions,
			exec.RunnerWithConfigData(flags.configData),
		)
	}
	if flags.json {
		runnerOptions = append(
			runnerOptions,
			exec.RunnerWithJSON(),
		)
	}
	if flags.protocBinPath != "" {
		runnerOptions = append(
			runnerOptions,
			exec.RunnerWithProtocBinPath(flags.protocBinPath),
		)
	} else if envProtocBinPath := os.Getenv("PROTOTOOL_PROTOC_BIN_PATH"); envProtocBinPath != "" {
		runnerOptions = append(
			runnerOptions,
			exec.RunnerWithProtocBinPath(envProtocBinPath),
		)
	}
	if flags.protocWKTPath != "" {
		runnerOptions = append(
			runnerOptions,
			exec.RunnerWithProtocWKTPath(flags.protocWKTPath),
		)
	} else if envProtocWKTPath := os.Getenv("PROTOTOOL_PROTOC_WKT_PATH"); envProtocWKTPath != "" {
		runnerOptions = append(
			runnerOptions,
			exec.RunnerWithProtocWKTPath(envProtocWKTPath),
		)
	}
	if flags.errorFormat != "" {
		runnerOptions = append(
			runnerOptions,
			exec.RunnerWithErrorFormat(flags.errorFormat),
		)
	}
	if flags.protocURL != "" {
		runnerOptions = append(
			runnerOptions,
			exec.RunnerWithProtocURL(flags.protocURL),
		)
	}
	if flags.walkTimeout != "" {
		parsedWalkTimeout, err := time.ParseDuration(flags.walkTimeout)
		if err != nil {
			return nil, err
		}
		runnerOptions = append(
			runnerOptions,
			exec.RunnerWithWalkTimeout(parsedWalkTimeout),
		)
	}
	if develMode {
		runnerOptions = append(
			runnerOptions,
			exec.RunnerWithDevelMode(),
		)
	}
	workDirPath, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return exec.NewRunner(workDirPath, stdin, stdout, runnerOptions...), nil
}

func getLogger(stderr io.Writer, debug bool) (*zap.Logger, error) {
	level := zapcore.InfoLevel
	if debug {
		level = zapcore.DebugLevel
	}
	return zap.New(
		zapcore.NewCore(
			zapcore.NewConsoleEncoder(
				zap.NewDevelopmentEncoderConfig(),
			),
			zapcore.Lock(zapcore.AddSync(stderr)),
			zap.NewAtomicLevelAt(level),
		),
	), nil
}
