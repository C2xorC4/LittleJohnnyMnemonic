package main

import (
	"errors"
	"strings"
	"testing"
)

// withFakeSchtasks swaps schtasksRunner for the duration of the test,
// capturing every invocation's args slice. The returned slice grows with
// each call.
func withFakeSchtasks(t *testing.T, response string, returnErr error) *[][]string {
	t.Helper()
	calls := &[][]string{}
	saved := schtasksRunner
	schtasksRunner = func(args ...string) (string, error) {
		captured := make([]string, len(args))
		copy(captured, args)
		*calls = append(*calls, captured)
		return response, returnErr
	}
	t.Cleanup(func() { schtasksRunner = saved })
	return calls
}

func TestInstallAutodreamSchedule_BuildsCorrectSchtasksCommand(t *testing.T) {
	calls := withFakeSchtasks(t, "SUCCESS: The task was created.", nil)

	out, err := installAutodreamSchedule(15)
	if err != nil {
		t.Fatal(err)
	}
	if out == "" {
		t.Error("expected non-empty output")
	}
	if len(*calls) != 1 {
		t.Fatalf("got %d schtasks calls, want 1", len(*calls))
	}
	args := (*calls)[0]

	// Must include /Create, /SC MINUTE, /MO 15, /TN <task>, /TR <exe> autodream, /IT, /F
	// /IT (interactive task, runs only when user is logged on) replaces /RU
	// INTERACTIVE — the latter required admin elevation, /IT does not.
	want := map[string]string{
		"/SC": "MINUTE",
		"/MO": "15",
		"/TN": autodreamTaskName,
	}
	for flag, val := range want {
		idx := indexOf(args, flag)
		if idx < 0 {
			t.Errorf("args missing flag %s; got %v", flag, args)
			continue
		}
		if idx+1 >= len(args) || args[idx+1] != val {
			t.Errorf("flag %s: got value %q, want %q", flag, args[idx+1], val)
		}
	}
	if indexOf(args, "/Create") < 0 {
		t.Errorf("missing /Create; got %v", args)
	}
	if indexOf(args, "/F") < 0 {
		t.Errorf("missing /F; got %v", args)
	}
	if indexOf(args, "/IT") < 0 {
		t.Errorf("missing /IT (required for non-elevated install); got %v", args)
	}
	if indexOf(args, "/RU") >= 0 {
		t.Errorf("must NOT include /RU (causes Access Denied without admin); got %v", args)
	}
	// /TR value should end with " autodream"
	idx := indexOf(args, "/TR")
	if idx < 0 || idx+1 >= len(args) {
		t.Fatalf("missing /TR")
	}
	if !strings.HasSuffix(args[idx+1], " autodream") {
		t.Errorf("/TR value %q should end with ' autodream'", args[idx+1])
	}
}

func TestInstallAutodreamSchedule_DefaultsToFifteen(t *testing.T) {
	calls := withFakeSchtasks(t, "ok", nil)
	if _, err := installAutodreamSchedule(0); err != nil {
		t.Fatal(err)
	}
	args := (*calls)[0]
	idx := indexOf(args, "/MO")
	if args[idx+1] != "15" {
		t.Errorf("zero interval should default to 15; got %s", args[idx+1])
	}
}

func TestInstallAutodreamSchedule_HonorsCustomInterval(t *testing.T) {
	calls := withFakeSchtasks(t, "ok", nil)
	if _, err := installAutodreamSchedule(30); err != nil {
		t.Fatal(err)
	}
	args := (*calls)[0]
	idx := indexOf(args, "/MO")
	if args[idx+1] != "30" {
		t.Errorf("/MO = %s, want 30", args[idx+1])
	}
}

func TestInstallAutodreamSchedule_PropagatesSchtasksErr(t *testing.T) {
	withFakeSchtasks(t, "", errors.New("simulated schtasks failure"))
	_, err := installAutodreamSchedule(15)
	if err == nil {
		t.Error("expected error to propagate")
	}
	if !strings.Contains(err.Error(), "simulated schtasks failure") {
		t.Errorf("err = %v, want wrapping of schtasks error", err)
	}
}

func TestUninstallAutodreamSchedule_BuildsDeleteCommand(t *testing.T) {
	calls := withFakeSchtasks(t, "SUCCESS", nil)
	if _, err := uninstallAutodreamSchedule(); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(*calls))
	}
	args := (*calls)[0]
	if indexOf(args, "/Delete") < 0 {
		t.Errorf("missing /Delete; got %v", args)
	}
	if indexOf(args, "/F") < 0 {
		t.Errorf("missing /F; got %v", args)
	}
	idx := indexOf(args, "/TN")
	if idx < 0 || args[idx+1] != autodreamTaskName {
		t.Errorf("/TN argument missing or wrong; got %v", args)
	}
}

func TestUninstallAutodreamSchedule_PropagatesSchtasksErr(t *testing.T) {
	withFakeSchtasks(t, "", errors.New("task not found"))
	_, err := uninstallAutodreamSchedule()
	if err == nil {
		t.Error("expected error to propagate")
	}
}

func TestAutodreamScheduleStatus_PresentWhenSchtasksSucceeds(t *testing.T) {
	withFakeSchtasks(t, "TaskName: JmAutodream", nil)
	installed, err := autodreamScheduleStatus()
	if err != nil {
		t.Fatal(err)
	}
	if !installed {
		t.Error("expected installed=true when /Query succeeds")
	}
}

func TestAutodreamScheduleStatus_AbsentWhenSchtasksErrors(t *testing.T) {
	withFakeSchtasks(t, "", errors.New("ERROR: cannot find file"))
	installed, err := autodreamScheduleStatus()
	if err != nil {
		t.Errorf("err = %v, want nil (not-installed should not be a fatal error)", err)
	}
	if installed {
		t.Error("expected installed=false when /Query errors")
	}
}

// indexOf returns the index of needle in haystack, or -1.
func indexOf(haystack []string, needle string) int {
	for i, s := range haystack {
		if s == needle {
			return i
		}
	}
	return -1
}
