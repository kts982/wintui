package main

import "testing"

func TestExecutionLogPausesFollowWhenScrolledUp(t *testing.T) {
	log := newExecutionLog()
	log.setSize(120, 24)
	for i := 0; i < 30; i++ {
		log.appendLine("line")
	}

	if !log.vp.AtBottom() {
		t.Fatal("expected log to start at bottom while following")
	}

	_, handled := log.update(keyMsg("pgup"))
	if !handled {
		t.Fatal("expected pgup to be handled")
	}
	if log.follow {
		t.Fatal("expected follow mode to pause after scrolling up")
	}
	offsetBefore := log.vp.YOffset()

	log.appendLine("new line")
	if log.vp.YOffset() != offsetBefore {
		t.Fatalf("yOffset = %d, want %d while follow is paused", log.vp.YOffset(), offsetBefore)
	}
}

func TestExecutionLogFollowShortcutReturnsToBottom(t *testing.T) {
	log := newExecutionLog()
	log.setSize(120, 24)
	for i := 0; i < 30; i++ {
		log.appendLine("line")
	}
	_, _ = log.update(keyMsg("pgup"))

	_, handled := log.update(keyMsg("f"))
	if !handled {
		t.Fatal("expected follow shortcut to be handled")
	}
	if !log.follow {
		t.Fatal("expected follow mode to resume")
	}
	if !log.vp.AtBottom() {
		t.Fatal("expected viewport to jump back to bottom")
	}
}

func TestExecutionLogDoneToggleExpandsAndHandlesScroll(t *testing.T) {
	log := newExecutionLog()
	log.setSize(120, 24)
	for i := 0; i < 30; i++ {
		log.appendLine("line")
	}
	log.setDoneExpanded(false)

	_, handled := log.doneUpdate(keyMsg("l"))
	if !handled {
		t.Fatal("expected log toggle to be handled")
	}
	if !log.expanded {
		t.Fatal("expected done log to expand")
	}

	_, handled = log.doneUpdate(keyMsg("pgup"))
	if !handled {
		t.Fatal("expected scroll in expanded done view to be handled")
	}
	if log.follow {
		t.Fatal("expected follow mode to pause after scrolling done log")
	}
}
