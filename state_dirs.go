package main

import (
	"fmt"
	"sort"
	"sync"
)

// stateDirIssues records MkdirAll failures from the state-path helpers
// (configPath, diskCachePath, selfUpdateStateDir). Those helpers swallow the
// error by design — they must still return a usable path — so a failed
// directory creation otherwise surfaces only as settings/cache silently not
// saving. Recording it here lets `wintui doctor` and the Health tab show it.
var stateDirIssues = struct {
	sync.Mutex
	m map[string]string
}{m: make(map[string]string)}

// recordStateDirResult notes the outcome of a state-dir MkdirAll. A nil err
// clears any previously recorded failure for purpose, so a transient problem
// self-heals in doctor output.
func recordStateDirResult(purpose string, err error) {
	stateDirIssues.Lock()
	defer stateDirIssues.Unlock()
	if err == nil {
		delete(stateDirIssues.m, purpose)
		return
	}
	stateDirIssues.m[purpose] = err.Error()
}

// stateDirErrors returns the recorded failures as sorted "purpose: error"
// lines, for stable doctor output.
func stateDirErrors() []string {
	stateDirIssues.Lock()
	defer stateDirIssues.Unlock()
	out := make([]string, 0, len(stateDirIssues.m))
	for purpose, msg := range stateDirIssues.m {
		out = append(out, fmt.Sprintf("%s: %s", purpose, msg))
	}
	sort.Strings(out)
	return out
}
