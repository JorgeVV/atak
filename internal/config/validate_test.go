package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// redirectConfigDir points os.UserConfigDir() at a fresh temp directory and
// returns the atak config dir inside it. Setting XDG_CONFIG_HOME alone is not
// enough: os.UserConfigDir reads it on Linux, but uses $HOME/Library/Application
// Support on darwin and %AppData% on Windows, so a test that sets only the one
// variable silently reads the developer's real config.json.
func redirectConfigDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	t.Setenv("AppData", filepath.Join(dir, "AppData"))
	base, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("os.UserConfigDir: %v", err)
	}
	return filepath.Join(base, "atak")
}

func TestValidateConfig(t *testing.T) {
	base := `"workerCount":1,"backupLevel":6,"modOutputMode":false,"modOutputName":"ATAK","stripMipsWhenDisabled":false,"modsDir":"","backupDir":"","modlistPath":""`

	cases := []struct {
		label   string
		json    string
		wantErr string // empty means expect success
	}{
		// (a) missing scanExclusions
		{
			"missing scanExclusions",
			`{` + base + `,"compressionBackend":"texconv"}`,
			`field "scanExclusions" is missing`,
		},
		// (b) scanExclusions wrong type
		{
			"scanExclusions wrong type (string)",
			`{` + base + `,"compressionBackend":"texconv","scanExclusions":"foo"}`,
			`expected array of strings, got string`,
		},
		// (c) invalid compressionBackend value
		{
			"compressionBackend invalid value",
			`{` + base + `,"compressionBackend":"gpu-magic","scanExclusions":[]}`,
			`invalid value "gpu-magic"`,
		},
		// (d) missing compressionBackend
		{
			"missing compressionBackend",
			`{` + base + `,"scanExclusions":[]}`,
			`field "compressionBackend" is missing`,
		},
		// valid: empty scanExclusions array is allowed
		{
			"empty scanExclusions array is valid",
			`{` + base + `,"compressionBackend":"texconv","scanExclusions":[]}`,
			"",
		},
		// valid: compressonator-bc7e is a valid backend
		{
			"compressonator-bc7e is valid",
			`{` + base + `,"compressionBackend":"compressonator-bc7e","scanExclusions":[]}`,
			"",
		},
		// missing workerCount
		{
			"missing workerCount",
			`{"backupLevel":6,"modOutputMode":false,"modOutputName":"ATAK","stripMipsWhenDisabled":false,"compressionBackend":"texconv","scanExclusions":[],"modsDir":"","backupDir":"","modlistPath":""}`,
			`field "workerCount" is missing`,
		},
		// wrong type: workerCount is string
		{
			"workerCount wrong type",
			`{` + base + `,"compressionBackend":"texconv","scanExclusions":[],"workerCount":"1"}`,
			`field "workerCount" has wrong type (expected number, got string)`,
		},
		// compressionBackend wrong type (not string)
		{
			"compressionBackend wrong type",
			`{` + base + `,"compressionBackend":42,"scanExclusions":[]}`,
			`field "compressionBackend" has wrong type (expected string, got number)`,
		},
		// empty compressionBackend string
		{
			"compressionBackend empty string",
			`{` + base + `,"compressionBackend":"","scanExclusions":[]}`,
			`invalid value ""`,
		},
		// malformed JSON
		{
			"malformed JSON",
			`{not valid json`,
			`invalid JSON`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			cfgDir := redirectConfigDir(t)
			if err := os.MkdirAll(cfgDir, 0755); err != nil {
				t.Fatalf("mkdir %s: %v", cfgDir, err)
			}
			if err := os.WriteFile(filepath.Join(cfgDir, "config.json"), []byte(tc.json), 0644); err != nil {
				t.Fatalf("write config.json: %v", err)
			}

			_, err := Load()

			if tc.wantErr == "" {
				if err != nil {
					t.Errorf("expected no error, got: %v", err)
				}
				return
			}
			if err == nil {
				t.Errorf("expected error containing %q, got nil", tc.wantErr)
				return
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestLoadFirstRunNoValidation(t *testing.T) {
	// First run (no config.json) must not trigger validation and must return defaults.
	redirectConfigDir(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("first run Load() returned error: %v", err)
	}
	if cfg.WorkerCount != 1 {
		t.Errorf("unexpected default WorkerCount: %d", cfg.WorkerCount)
	}
	if cfg.CompressionBackend != BackendTexconv {
		t.Errorf("unexpected default CompressionBackend: %q", cfg.CompressionBackend)
	}
}
