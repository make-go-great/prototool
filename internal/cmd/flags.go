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
	"github.com/spf13/pflag"
)

type flags struct {
	cachePath     string
	configData    string
	debug         bool
	disableFormat bool
	disableLint   bool
	document      bool
	dryRun        bool
	errorFormat   string
	fix           bool
	json          bool
	protocBinPath string
	protocWKTPath string
	protocURL     string
	uncomment     bool
	walkTimeout   string
}

func (f *flags) bindCachePath(flagSet *pflag.FlagSet) {
	flagSet.StringVar(&f.cachePath, "cache-path", "", "The path to use for the cache, otherwise uses the default behavior. The user is expected to clean and manage this cache path. See prototool help cache update for more details.")
}

func (f *flags) bindConfigData(flagSet *pflag.FlagSet) {
	flagSet.StringVar(&f.configData, "config-data", "", "The configuration data to use instead of reading prototool.yaml or prototool.json files.\nThis will act as if there is a configuration file with the given data in the current directory, and no other configuration files recursively.\nThis is an advanced feature and is not recommended to be generally used.")
}

func (f *flags) bindDebug(flagSet *pflag.FlagSet) {
	flagSet.BoolVar(&f.debug, "debug", false, "Run in debug mode, which will print out debug logging.")
}

func (f *flags) bindDisableFormat(flagSet *pflag.FlagSet) {
	flagSet.BoolVar(&f.disableFormat, "disable-format", false, "Do not run formatting.")
}

func (f *flags) bindDisableLint(flagSet *pflag.FlagSet) {
	flagSet.BoolVar(&f.disableLint, "disable-lint", false, "Do not run linting.")
}

func (f *flags) bindDryRun(flagSet *pflag.FlagSet) {
	flagSet.BoolVar(&f.dryRun, "dry-run", false, "Print the protoc commands that would have been run without actually running them.")
}

func (f *flags) bindDocument(flagSet *pflag.FlagSet) {
	flagSet.BoolVar(&f.document, "document", false, "Document all available options. Automatically set if --uncomment is set.")
}

func (f *flags) bindErrorFormat(flagSet *pflag.FlagSet) {
	flagSet.StringVar(&f.errorFormat, "error-format", "filename:line:column:message", `The colon-separated fields to print out on error. Valid values are "filename:line:column:id:message".`)
}

func (f *flags) bindFix(flagSet *pflag.FlagSet) {
	flagSet.BoolVarP(&f.fix, "fix", "f", false, "Fix the file according to the Style Guide.")
}

func (f *flags) bindJSON(flagSet *pflag.FlagSet) {
	flagSet.BoolVar(&f.json, "json", false, "Output as JSON.")
}

func (f *flags) bindProtocURL(flagSet *pflag.FlagSet) {
	flagSet.StringVar(&f.protocURL, "protoc-url", "", "The url to use to download the protoc zip file, otherwise uses GitHub Releases. Setting this option will ignore the config protoc.version setting.")
}

func (f *flags) bindProtocBinPath(flagSet *pflag.FlagSet) {
	flagSet.StringVar(&f.protocBinPath, "protoc-bin-path", "", "The path to the protoc binary. Setting this option will ignore the config protoc.version setting.\nThis flag must be used with protoc-wkt-path and must not be used with the protoc-url flag.\nThis setting can also be controlled using the $PROTOTOOL_PROTOC_BIN_PATH environment variable, however this flag takes precedence.")
}

func (f *flags) bindProtocWKTPath(flagSet *pflag.FlagSet) {
	flagSet.StringVar(&f.protocWKTPath, "protoc-wkt-path", "", "The path to the well-known types. Setting this option will ignore the config protoc.version setting.\nThis flag must be used with protoc-bin-path and must not be used with the protoc-url flag.\nThis setting can also be controlled using the $PROTOTOOL_PROTOC_WKT_PATH environment variable, however this flag takes precedence.")
}

func (f *flags) bindUncomment(flagSet *pflag.FlagSet) {
	flagSet.BoolVar(&f.uncomment, "uncomment", false, "Uncomment the example config settings. Automatically sets --document.")
}

func (f *flags) bindWalkTimeout(flagSet *pflag.FlagSet) {
	flagSet.StringVar(&f.walkTimeout, "walk-timeout", "3s", "The maximum time to allow for walking the directory structure looking for proto files.")
}
