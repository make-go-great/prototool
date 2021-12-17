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
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uber/prototool/internal/settings"
	"github.com/uber/prototool/internal/vars"
)

func TestDownload(t *testing.T) {
	assertExact(t, true, false, 0, ``, "cache", "update", "testdata/foo")
	fileInfo, err := os.Stat(filepath.Join("testcache", "protobuf", vars.DefaultProtocVersion))
	assert.NoError(t, err)
	assert.True(t, fileInfo.IsDir())
	fileInfo, err = os.Stat(filepath.Join("testcache", "protobuf", vars.DefaultProtocVersion+".lock"))
	assert.NoError(t, err)
	assert.False(t, fileInfo.IsDir())
}

func TestCompile(t *testing.T) {
	t.Parallel()
	assertDoCompileFiles(
		t,
		false,
		false,
		`testdata/compile/errors_on_import/dep_errors.proto:6:1:Expected ";".`,
		"testdata/compile/errors_on_import/dep_errors.proto",
	)
	assertDoCompileFiles(
		t,
		false,
		false,
		`testdata/compile/errors_on_import/dep_errors.proto:6:1:Expected ";".`,
		"testdata/compile/errors_on_import",
	)
	assertDoCompileFiles(
		t,
		false,
		false,
		`testdata/compile/extra_import/extra_import.proto:6:1:Import "dep.proto" was not used.`,
		"testdata/compile/extra_import/extra_import.proto",
	)
	assertDoCompileFiles(
		t,
		false,
		false,
		`testdata/compile/json/json_camel_case_conflict.proto:7:9:The JSON camel-case name of field "helloworld" conflicts with field "helloWorld". This is not allowed in proto3.`,
		"testdata/compile/json/json_camel_case_conflict.proto",
	)
	assertDoCompileFiles(
		t,
		false,
		false,
		`testdata/compile/semicolon/missing_package_semicolon.proto:5:1:Expected ";".`,
		"testdata/compile/semicolon/missing_package_semicolon.proto",
	)
	assertDoCompileFiles(
		t,
		false,
		false,
		`testdata/compile/syntax/missing_syntax.proto:1:1:No syntax specified. Please use 'syntax = "proto2";' or 'syntax = "proto3";' to specify a syntax version.
		testdata/compile/syntax/missing_syntax.proto:4:3:Expected "required", "optional", or "repeated".`,
		"testdata/compile/syntax/missing_syntax.proto",
	)
	assertDoCompileFiles(
		t,
		false,
		false,
		`testdata/compile/recursive/one.proto:5:1:File recursively imports itself one.proto -> two.proto -> one.proto.
		testdata/compile/recursive/one.proto:5:1:Import "two.proto" was not found or had errors.`,
		"testdata/compile/recursive/one.proto",
	)
	assertDoCompileFiles(
		t,
		true,
		false,
		``,
		"testdata/compile/proto2/syntax_proto2.proto",
	)
	assertDoCompileFiles(
		t,
		false,
		false,
		`testdata/compile/notimported/not_imported.proto:11:3:"foo.Dep" seems to be defined in "dep.proto", which is not imported by "not_imported.proto".  To use it here, please add the necessary import.`,
		"testdata/compile/notimported/not_imported.proto",
	)
	assertDoCompileFiles(
		t,
		false,
		true,
		`{"filename":"testdata/compile/errors_on_import/dep_errors.proto","line":6,"column":1,"message":"Expected \";\"."}`,
		"testdata/compile/errors_on_import/dep_errors.proto",
	)
}

func TestInit(t *testing.T) {
	t.Parallel()
	tmpDir, err := ioutil.TempDir("", "")
	require.NoError(t, err)
	require.NotEmpty(t, tmpDir)
	defer func() {
		_ = os.RemoveAll(tmpDir)
	}()

	assertDo(t, false, false, 0, "", "config", "init", tmpDir)
	assertDo(t, false, false, 1, fmt.Sprintf("%s already exists", filepath.Join(tmpDir, settings.DefaultConfigFilename)), "config", "init", tmpDir)
}

func TestVersion(t *testing.T) {
	t.Parallel()
	assertRegexp(t, false, false, 0, fmt.Sprintf("Version:.*%s\nDefault protoc version:.*%s\n", vars.Version, vars.DefaultProtocVersion), "version")
}

func TestVersionJSON(t *testing.T) {
	t.Parallel()
	assertRegexp(t, false, false, 0, fmt.Sprintf(`(?s){.*"version":.*"%s",.*"default_protoc_version":.*"%s".*}`, vars.Version, vars.DefaultProtocVersion), "version", "--json")
}

func TestFiles(t *testing.T) {
	assertExact(t, false, false, 0, `testdata/foo/bar/dep.proto
testdata/foo/success.proto`, "files", "testdata/foo")
}

func TestGenerateDescriptorSetSameDirAsConfigFile(t *testing.T) {
	t.Parallel()
	// https://github.com/uber/prototool/issues/389
	generatedFilePath := "testdata/generate/descriptorset/descriptorset.bin"
	if _, err := os.Stat(generatedFilePath); err == nil {
		assert.NoError(t, os.Remove(generatedFilePath))
	}
	_, exitCode := testDo(t, true, false, "generate", filepath.Dir(generatedFilePath))
	assert.Equal(t, 0, exitCode)
	_, err := os.Stat(generatedFilePath)
	assert.NoError(t, err)
}

func assertDoCompileFiles(t *testing.T, expectSuccess bool, asJSON bool, expectedLinePrefixes string, filePaths ...string) {
	lines := getCleanLines(expectedLinePrefixes)
	expectedExitCode := 0
	if !expectSuccess {
		expectedExitCode = 255
	}
	cmd := []string{"compile"}
	if asJSON {
		cmd = append(cmd, "--json")
	}
	assertDo(t, true, true, expectedExitCode, strings.Join(lines, "\n"), append(cmd, filePaths...)...)
}

func assertRegexp(t *testing.T, withCachePath bool, extraErrorFormat bool, expectedExitCode int, expectedRegexp string, args ...string) {
	stdout, exitCode := testDo(t, withCachePath, extraErrorFormat, args...)
	assert.Equal(t, expectedExitCode, exitCode)
	matched, err := regexp.MatchString(expectedRegexp, stdout)
	assert.NoError(t, err)
	assert.True(t, matched, "Expected regex %s but got %s", expectedRegexp, stdout)
}

func assertExact(t *testing.T, withCachePath bool, extraErrorFormat bool, expectedExitCode int, expectedStdout string, args ...string) {
	stdout, exitCode := testDo(t, withCachePath, extraErrorFormat, args...)
	assert.Equal(t, expectedExitCode, exitCode)
	assert.Equal(t, expectedStdout, stdout)
}

func assertDo(t *testing.T, withCachePath bool, extraErrorFormat bool, expectedExitCode int, expectedLinePrefixes string, args ...string) {
	assertDoInternal(t, nil, withCachePath, extraErrorFormat, expectedExitCode, expectedLinePrefixes, args...)
}

func testDoStdin(t *testing.T, stdin io.Reader, withCachePath bool, extraErrorFormat bool, args ...string) (string, int) {
	return testDoInternal(stdin, withCachePath, extraErrorFormat, args...)
}

func testDo(t *testing.T, withCachePath bool, extraErrorFormat bool, args ...string) (string, int) {
	return testDoInternal(nil, withCachePath, extraErrorFormat, args...)
}

func getCleanLines(output string) []string {
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

// do not use these in tests

func assertDoInternal(t *testing.T, stdin io.Reader, withCachePath bool, extraErrorFormat bool, expectedExitCode int, expectedLinePrefixes string, args ...string) {
	stdout, exitCode := testDoStdin(t, stdin, withCachePath, extraErrorFormat, args...)
	outputSplit := getCleanLines(stdout)
	assert.Equal(t, expectedExitCode, exitCode, strings.Join(outputSplit, "\n"))
	expectedLinePrefixesSplit := getCleanLines(expectedLinePrefixes)
	require.Equal(t, len(expectedLinePrefixesSplit), len(outputSplit), strings.Join(outputSplit, "\n"))
	for i, expectedLinePrefix := range expectedLinePrefixesSplit {
		assert.True(t, strings.HasPrefix(outputSplit[i], expectedLinePrefix), "%s %d %s", expectedLinePrefix, i, strings.Join(outputSplit, "\n"))
	}
}

func testDoInternal(stdin io.Reader, withCachePath bool, extraErrorFormat bool, args ...string) (string, int) {
	if stdin == nil {
		stdin = os.Stdin
	}
	if withCachePath {
		args = append(
			args,
			"--cache-path", "testcache",
		)
	}
	if extraErrorFormat {
		args = append(
			args,
			"--error-format", "filename:line:column:id:message",
		)
	}
	buffer := bytes.NewBuffer(nil)
	// develMode is on, so we have access to all commands
	exitCode := do(true, args, stdin, buffer, buffer)
	return strings.TrimSpace(buffer.String()), exitCode
}
