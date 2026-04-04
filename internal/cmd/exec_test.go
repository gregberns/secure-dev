// Package cmd provides tests for the exec command.
// REQ-007-013: Exec Command
// REQ-007-014: Exec JSON Output
// NOTE: Tests use global getBackendFunc — do not use t.Parallel().
package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sd/internal/backend"
	"sd/internal/backend/memory"
	"sd/internal/ui"
)

// execCall records the arguments of an Exec invocation.
type execCall struct {
	VMName  string
	Command []string
}

// setupExecMemoryTest creates a memory backend, registers VMs with given statuses,
// and injects it into getBackendFunc. Returns the backend and a pointer to a slice
// of execCalls for tracking invocations.
func setupExecMemoryTest(t *testing.T, statusMap map[string]backend.VMStatus, result backend.ExecResult, execErr error) (*memory.Backend, *[]execCall) {
	t.Helper()
	newRootTestEnv(t)

	mb := memory.New()
	for name, status := range statusMap {
		require.NoError(t, mb.Create(context.Background(), name, backend.VMConfig{}))
		if status != backend.StatusRunning {
			require.NoError(t, mb.SetStatus(name, status))
		}
	}

	calls := &[]execCall{}
	if execErr != nil {
		mb.SetMethodError("exec", execErr)
	} else {
		mb.SetExecHandler(func(ctx context.Context, name string, command []string) (backend.ExecResult, error) {
			*calls = append(*calls, execCall{VMName: name, Command: command})
			return result, nil
		})
	}

	origGetBackend := getBackendFunc
	getBackendFunc = func(_ string) (backend.Backend, error) {
		return mb, nil
	}
	t.Cleanup(func() { getBackendFunc = origGetBackend; mb.Reset() })

	return mb, calls
}

// --- Unit tests ---

func TestExecCommand_Registered(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	found := false
	for _, cmd := range root.Commands() {
		if cmd.Name() == "exec" {
			found = true
			assert.Equal(t, "connection", cmd.GroupID)
			break
		}
	}
	assert.True(t, found, "exec command must be registered")
}

func TestExecCommand_MinArgs(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	cmd, _, err := root.Find([]string{"exec"})
	require.NoError(t, err)
	assert.NotNil(t, cmd.Args, "exec must have an Args validator")

	// Zero args must fail
	err = cmd.Args(cmd, nil)
	assert.Error(t, err, "exec must reject zero args")

	// One arg must fail
	err = cmd.Args(cmd, []string{"myvm"})
	assert.Error(t, err, "exec must reject one arg (needs vm + command)")

	// Two args must pass
	err = cmd.Args(cmd, []string{"myvm", "echo"})
	assert.NoError(t, err, "exec must accept vm name + command")
}

func TestExecCommand_BasicExec_HumanOutput(t *testing.T) {
	_, calls := setupExecMemoryTest(t,
		map[string]backend.VMStatus{"myvm": backend.StatusRunning},
		backend.ExecResult{ExitCode: 0, Stdout: "hello world\n", Stderr: ""},
		nil,
	)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"exec", "myvm", "--", "echo", "hello", "world"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)
	assert.Equal(t, "hello world\n", buf.String())

	// Verify backend was called with correct args
	require.Len(t, *calls, 1)
	assert.Equal(t, "myvm", (*calls)[0].VMName)
	assert.Equal(t, []string{"echo", "hello", "world"}, (*calls)[0].Command)
}

func TestExecCommand_BasicExec_JSONOutput(t *testing.T) {
	setupExecMemoryTest(t,
		map[string]backend.VMStatus{"myvm": backend.StatusRunning},
		backend.ExecResult{ExitCode: 0, Stdout: "hello\n", Stderr: ""},
		nil,
	)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "exec", "myvm", "--", "echo", "hello"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)

	var result map[string]any
	err = json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err, "output must be valid JSON: %s", buf.String())

	assert.True(t, result["ok"].(bool))
	data := result["data"].(map[string]any)
	assert.Equal(t, float64(0), data["exit_code"])
	assert.Equal(t, "hello\n", data["stdout"])
	assert.Equal(t, "", data["stderr"])
}

func TestExecCommand_NonZeroExitCode_JSONOutput(t *testing.T) {
	setupExecMemoryTest(t,
		map[string]backend.VMStatus{"myvm": backend.StatusRunning},
		backend.ExecResult{ExitCode: 42, Stdout: "", Stderr: "error: something failed\n"},
		nil,
	)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	// Prevent os.Exit from killing the test process
	oldOsExit := osExit
	var exitCode int
	osExit = func(code int) { exitCode = code }
	defer func() { osExit = oldOsExit }()

	root := RootCmd()
	root.SetArgs([]string{"--json", "exec", "myvm", "--", "false"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)
	assert.Equal(t, 42, exitCode)

	var result map[string]any
	err = json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err, "output must be valid JSON: %s", buf.String())

	data := result["data"].(map[string]any)
	assert.Equal(t, float64(42), data["exit_code"])
	assert.Equal(t, "error: something failed\n", data["stderr"])
}

func TestExecCommand_VMNotFound(t *testing.T) {
	setupExecMemoryTest(t, map[string]backend.VMStatus{}, backend.ExecResult{}, nil)

	root := RootCmd()
	root.SetArgs([]string{"exec", "nonexistent", "--", "echo"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "vm_not_found", cliErr.Code)
	assert.Contains(t, cliErr.Message, "nonexistent")
}

func TestExecCommand_VMNotRunning(t *testing.T) {
	setupExecMemoryTest(t,
		map[string]backend.VMStatus{"myvm": backend.StatusStopped},
		backend.ExecResult{}, nil,
	)

	root := RootCmd()
	root.SetArgs([]string{"exec", "myvm", "--", "echo"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "vm_not_running", cliErr.Code)
	assert.Contains(t, cliErr.Message, "myvm")
	assert.Contains(t, cliErr.Message, "sd start")
}

func TestExecCommand_VMInErrorStatus(t *testing.T) {
	setupExecMemoryTest(t,
		map[string]backend.VMStatus{"myvm": backend.StatusError},
		backend.ExecResult{}, nil,
	)

	root := RootCmd()
	root.SetArgs([]string{"exec", "myvm", "--", "echo"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "vm_not_running", cliErr.Code)
}

func TestExecCommand_BackendUnavailable(t *testing.T) {
	mb, _ := setupExecMemoryTest(t, map[string]backend.VMStatus{}, backend.ExecResult{}, nil)
	mb.SetMethodError("available", fmt.Errorf("backend not available"))

	root := RootCmd()
	root.SetArgs([]string{"exec", "myvm", "--", "echo"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "backend_unavailable", cliErr.Code)
}

func TestExecCommand_BackendGetError(t *testing.T) {
	newRootTestEnv(t)

	origGetBackend := getBackendFunc
	getBackendFunc = func(name string) (backend.Backend, error) {
		return nil, fmt.Errorf("no such backend")
	}
	t.Cleanup(func() { getBackendFunc = origGetBackend })

	root := RootCmd()
	root.SetArgs([]string{"exec", "myvm", "--", "echo"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "backend_unavailable", cliErr.Code)
}

func TestExecCommand_ExecBackendError(t *testing.T) {
	setupExecMemoryTest(t,
		map[string]backend.VMStatus{"myvm": backend.StatusRunning},
		backend.ExecResult{},
		fmt.Errorf("internal error"),
	)

	root := RootCmd()
	root.SetArgs([]string{"exec", "myvm", "--", "echo"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "exec_failed", cliErr.Code)
	assert.Contains(t, cliErr.Message, "internal error")
}

func TestExecCommand_ExecBackendErrVMNotRunning(t *testing.T) {
	setupExecMemoryTest(t,
		map[string]backend.VMStatus{"myvm": backend.StatusRunning},
		backend.ExecResult{},
		backend.ErrVMNotRunning,
	)

	root := RootCmd()
	root.SetArgs([]string{"exec", "myvm", "--", "echo"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "vm_not_running", cliErr.Code)
}

func TestExecCommand_ExecBackendErrVMNotFound(t *testing.T) {
	setupExecMemoryTest(t,
		map[string]backend.VMStatus{"myvm": backend.StatusRunning},
		backend.ExecResult{},
		backend.ErrVMNotFound,
	)

	root := RootCmd()
	root.SetArgs([]string{"exec", "myvm", "--", "echo"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "vm_not_found", cliErr.Code)
}

func TestExecCommand_EmptyName(t *testing.T) {
	setupExecMemoryTest(t, map[string]backend.VMStatus{}, backend.ExecResult{}, nil)

	root := RootCmd()
	root.SetArgs([]string{"exec", "", "echo"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "invalid_argument", cliErr.Code)
}

func TestExecCommand_StatusCheckError(t *testing.T) {
	mb, _ := setupExecMemoryTest(t, map[string]backend.VMStatus{}, backend.ExecResult{}, nil)
	mb.SetMethodError("status", fmt.Errorf("connection refused"))

	root := RootCmd()
	root.SetArgs([]string{"exec", "myvm", "--", "echo"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "exec_failed", cliErr.Code)
	assert.Contains(t, cliErr.Message, "connection refused")
}

func TestExecCommand_StderrPassedThrough(t *testing.T) {
	setupExecMemoryTest(t,
		map[string]backend.VMStatus{"myvm": backend.StatusRunning},
		backend.ExecResult{ExitCode: 0, Stdout: "out\n", Stderr: "warning message\n"},
		nil,
	)

	oldStdout := os.Stdout
	outR, outW, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = outW

	oldStderr := os.Stderr
	errR, errW, err := os.Pipe()
	require.NoError(t, err)
	os.Stderr = errW

	root := RootCmd()
	root.SetArgs([]string{"exec", "myvm", "--", "cmd"})
	execErr := root.Execute()

	outW.Close()
	errW.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr

	var outBuf, errBuf bytes.Buffer
	_, _ = outBuf.ReadFrom(outR)
	_, _ = errBuf.ReadFrom(errR)

	require.NoError(t, execErr)
	assert.Equal(t, "out\n", outBuf.String())
	assert.Equal(t, "warning message\n", errBuf.String())
}

func TestExecCommand_CommandWithArgs(t *testing.T) {
	_, calls := setupExecMemoryTest(t,
		map[string]backend.VMStatus{"myvm": backend.StatusRunning},
		backend.ExecResult{ExitCode: 0},
		nil,
	)

	root := RootCmd()
	root.SetArgs([]string{"--json", "exec", "myvm", "--", "ls", "-la", "/tmp"})
	err := root.Execute()

	require.NoError(t, err)
	require.Len(t, *calls, 1)
	assert.Equal(t, []string{"ls", "-la", "/tmp"}, (*calls)[0].Command)
}

func TestExecCommand_MissingVMName(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	root.SetArgs([]string{"exec"})
	err := root.Execute()
	assert.Error(t, err, "exec without args must fail")
}

func TestExecCommand_MissingCommand(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	root.SetArgs([]string{"exec", "myvm"})
	err := root.Execute()
	assert.Error(t, err, "exec with only VM name must fail")
}

// --- Property-based tests ---

// Property: JSON output from exec always contains ok=true, data.exit_code, data.stdout, data.stderr.
func TestProperty_ExecJSONAlwaysValid(t *testing.T) {
	cases := []struct {
		name     string
		vmName   string
		exitCode int
		stdout   string
		stderr   string
	}{
		{"zero_exit", "vm1", 0, "ok\n", ""},
		{"nonzero_exit", "vm2", 1, "", "failed\n"},
		{"high_exit", "vm3", 127, "out", "err"},
		{"empty_output", "vm4", 0, "", ""},
		{"large_exit", "vm5", 255, "x", "y"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setupExecMemoryTest(t,
				map[string]backend.VMStatus{tc.vmName: backend.StatusRunning},
				backend.ExecResult{ExitCode: tc.exitCode, Stdout: tc.stdout, Stderr: tc.stderr},
				nil,
			)

			// Capture os.Exit for non-zero exit codes
			oldOsExit := osExit
			osExit = func(code int) {}
			defer func() { osExit = oldOsExit }()

			oldStdout := os.Stdout
			r, w, err := os.Pipe()
			require.NoError(t, err)
			os.Stdout = w

			root := RootCmd()
			root.SetArgs([]string{"--json", "exec", tc.vmName, "--", "cmd"})
			execErr := root.Execute()

			w.Close()
			os.Stdout = oldStdout

			var buf bytes.Buffer
			_, _ = buf.ReadFrom(r)

			require.NoError(t, execErr)

			var result map[string]any
			err = json.Unmarshal(buf.Bytes(), &result)
			require.NoError(t, err, "JSON must parse for %s: %s", tc.name, buf.String())
			assert.True(t, result["ok"].(bool), "ok must be true for %s", tc.name)

			data := result["data"].(map[string]any)
			assert.Equal(t, float64(tc.exitCode), data["exit_code"], "exit_code must match for %s", tc.name)
			assert.Equal(t, tc.stdout, data["stdout"], "stdout must match for %s", tc.name)
			assert.Equal(t, tc.stderr, data["stderr"], "stderr must match for %s", tc.name)
		})
	}
}

// Property: human output always passes through stdout from the remote command.
func TestProperty_ExecHumanOutputContainsStdout(t *testing.T) {
	outputs := []string{"hello\n", "multi\nline\n", "", "x", "long output with spaces and things\n"}
	for _, output := range outputs {
		t.Run(fmt.Sprintf("output_%q", output), func(t *testing.T) {
			setupExecMemoryTest(t,
				map[string]backend.VMStatus{"vm1": backend.StatusRunning},
				backend.ExecResult{ExitCode: 0, Stdout: output},
				nil,
			)

			oldStdout := os.Stdout
			r, w, err := os.Pipe()
			require.NoError(t, err)
			os.Stdout = w

			root := RootCmd()
			root.SetArgs([]string{"exec", "vm1", "--", "cmd"})
			execErr := root.Execute()

			w.Close()
			os.Stdout = oldStdout

			var buf bytes.Buffer
			_, _ = buf.ReadFrom(r)

			require.NoError(t, execErr)
			assert.Equal(t, output, buf.String(), "human output must match stdout for %q", output)
		})
	}
}

// Property: error codes are always snake_case.
func TestProperty_ExecErrorCodesSnakeCase(t *testing.T) {
	t.Run("vm_not_found", func(t *testing.T) {
		setupExecMemoryTest(t, map[string]backend.VMStatus{}, backend.ExecResult{}, nil)

		root := RootCmd()
		root.SetArgs([]string{"exec", "ghost", "--", "cmd"})
		err := root.Execute()
		require.Error(t, err)
		cliErr := err.(ui.CLIError)
		assert.Equal(t, "vm_not_found", cliErr.Code)
	})

	t.Run("vm_not_running", func(t *testing.T) {
		setupExecMemoryTest(t,
			map[string]backend.VMStatus{"vm1": backend.StatusStopped},
			backend.ExecResult{}, nil,
		)

		root := RootCmd()
		root.SetArgs([]string{"exec", "vm1", "--", "cmd"})
		err := root.Execute()
		require.Error(t, err)
		cliErr := err.(ui.CLIError)
		assert.Equal(t, "vm_not_running", cliErr.Code)
	})

	t.Run("backend_unavailable", func(t *testing.T) {
		mb, _ := setupExecMemoryTest(t, map[string]backend.VMStatus{}, backend.ExecResult{}, nil)
		mb.SetMethodError("available", fmt.Errorf("backend not available"))

		root := RootCmd()
		root.SetArgs([]string{"exec", "vm1", "--", "cmd"})
		err := root.Execute()
		require.Error(t, err)
		cliErr := err.(ui.CLIError)
		assert.Equal(t, "backend_unavailable", cliErr.Code)
	})

	t.Run("invalid_argument_empty_name", func(t *testing.T) {
		setupExecMemoryTest(t, map[string]backend.VMStatus{}, backend.ExecResult{}, nil)

		root := RootCmd()
		root.SetArgs([]string{"exec", "", "cmd"})
		err := root.Execute()
		require.Error(t, err)
		cliErr := err.(ui.CLIError)
		assert.Equal(t, "invalid_argument", cliErr.Code)
	})

	t.Run("exec_failed", func(t *testing.T) {
		setupExecMemoryTest(t,
			map[string]backend.VMStatus{"vm1": backend.StatusRunning},
			backend.ExecResult{},
			fmt.Errorf("internal error"),
		)

		root := RootCmd()
		root.SetArgs([]string{"exec", "vm1", "--", "cmd"})
		err := root.Execute()
		require.Error(t, err)
		cliErr := err.(ui.CLIError)
		assert.Equal(t, "exec_failed", cliErr.Code)
	})
}

// Property: running VM always calls backend Exec exactly once.
func TestProperty_ExecRunningVM_CallsBackendOnce(t *testing.T) {
	names := []string{"vm1", "vm2", "test-vm"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			_, calls := setupExecMemoryTest(t,
				map[string]backend.VMStatus{name: backend.StatusRunning},
				backend.ExecResult{ExitCode: 0},
				nil,
			)

			root := RootCmd()
			root.SetArgs([]string{"--json", "exec", name, "--", "echo"})
			err := root.Execute()
			require.NoError(t, err)
			require.Len(t, *calls, 1, "exec must call backend exactly once for %q", name)
			assert.Equal(t, name, (*calls)[0].VMName)
		})
	}
}

// Property: stopped VM never calls backend Exec.
func TestProperty_ExecStoppedVM_NeverCallsBackend(t *testing.T) {
	statuses := []backend.VMStatus{backend.StatusStopped, backend.StatusCreating, backend.StatusError}
	for _, status := range statuses {
		t.Run(string(status), func(t *testing.T) {
			_, calls := setupExecMemoryTest(t,
				map[string]backend.VMStatus{"vm1": status},
				backend.ExecResult{}, nil,
			)

			root := RootCmd()
			root.SetArgs([]string{"exec", "vm1", "--", "echo"})
			err := root.Execute()
			require.Error(t, err)
			assert.Empty(t, *calls, "exec on non-running VM must not call backend.Exec for status %s", status)
		})
	}
}

// Property: JSON output always contains required fields.
func TestProperty_ExecJSONRequiredFields(t *testing.T) {
	setupExecMemoryTest(t,
		map[string]backend.VMStatus{"myvm": backend.StatusRunning},
		backend.ExecResult{ExitCode: 0, Stdout: "test", Stderr: ""},
		nil,
	)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "exec", "myvm", "--", "echo"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)

	var result map[string]any
	err = json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err)

	assert.Contains(t, result, "ok", "JSON must have 'ok' field")
	assert.Contains(t, result, "data", "JSON must have 'data' field")

	data := result["data"].(map[string]any)
	assert.Contains(t, data, "exit_code", "data must have 'exit_code' field")
	assert.Contains(t, data, "stdout", "data must have 'stdout' field")
	assert.Contains(t, data, "stderr", "data must have 'stderr' field")
}

// Property: error JSON format is consistent for vm_not_found.
func TestProperty_ExecErrorJSONFormat(t *testing.T) {
	setupExecMemoryTest(t, map[string]backend.VMStatus{}, backend.ExecResult{}, nil)

	root := RootCmd()
	root.SetArgs([]string{"exec", "nonexistent", "--", "cmd"})
	execErr := root.Execute()

	require.Error(t, execErr)
	cliErr, ok := execErr.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "vm_not_found", cliErr.Code)
	assert.Contains(t, cliErr.Message, "nonexistent")
}
