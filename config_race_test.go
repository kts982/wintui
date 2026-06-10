package main

import (
	"testing"
)

// TestAppSettingsConcurrentCycleAndWorkerRead exercises the appSettings
// write path (settings screen cycling + override publishing, as the Update
// goroutine does) against the worker-goroutine read paths (the arg builders
// a refresh/action tea.Cmd calls). Run with -race: before appSettings was
// published via setAppSettings and snapshotted via currentSettings, this
// raced.
func TestAppSettingsConcurrentCycleAndWorkerRead(t *testing.T) {
	original := appSettings
	t.Cleanup(func() { setAppSettings(original) })
	setAppSettings(DefaultSettings())

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 500 {
			// Worker-side reads, as dispatched tea.Cmds perform them.
			_ = installCommandArgs("Test.App", "winget", "")
			_ = uninstallCommandArgs(Package{ID: "Test.App", Source: "winget"}, true, false)
			_ = currentSettings().packageElevateOverride("Test.App", "winget")
			_ = shouldRetryUninstallWithoutPurge(nil, "")
		}
	}()

	s := &settingsScreen{diskState: DefaultSettings()}
	for i := range 500 {
		s.cursor = i % len(settingDefs)
		_ = s.cycleForward()
		// Override mutation, as persistPackageOverride builds it (without
		// the disk write).
		next := appSettings.clone()
		next.setOverride("Test.App", "winget", PackageOverride{Scope: ScopeUser})
		setAppSettings(next)
	}
	<-done
}
