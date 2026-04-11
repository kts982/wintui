package main

import (
	"testing"

	"charm.land/lipgloss/v2"
)

func TestAllocateSectionHeightsKeepsInstalledUsableWithManyUpdates(t *testing.T) {
	sections := []sectionDef{
		{title: "Updates", desiredH: 16, minH: minScrollableSectionHeight},
		{title: "Installed", desiredH: 212, minH: minScrollableSectionHeight},
	}

	got := allocateSectionHeights(sections, 19)

	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	if got[1] < minScrollableSectionHeight {
		t.Fatalf("installed height = %d, want at least %d", got[1], minScrollableSectionHeight)
	}
	if got[0]+got[1] != 19 {
		t.Fatalf("panel height sum = %d, want 19", got[0]+got[1])
	}
}

func TestAllocateSectionHeightsGivesRemainderToInstalledPanel(t *testing.T) {
	sections := []sectionDef{
		{title: "Updates", desiredH: 4, minH: minScrollableSectionHeight},
		{title: "Installed", desiredH: 212, minH: minScrollableSectionHeight},
	}

	got := allocateSectionHeights(sections, 19)

	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	if got[1] <= got[0] {
		t.Fatalf("installed height = %d, updates height = %d, want installed to absorb spare space", got[1], got[0])
	}
	if got[0]+got[1] != 19 {
		t.Fatalf("panel height sum = %d, want 19", got[0]+got[1])
	}
}

func TestAllocateSectionHeightsPreservesExactQueueSection(t *testing.T) {
	sections := []sectionDef{
		{title: "Queue", desiredH: 3, minH: 3, exact: true},
		{title: "Updates", desiredH: 12, minH: minScrollableSectionHeight},
		{title: "Installed", desiredH: 40, minH: minScrollableSectionHeight},
	}

	got := allocateSectionHeights(sections, 22)

	if len(got) != 3 {
		t.Fatalf("len(got) = %d, want 3", len(got))
	}
	if got[0] != 3 {
		t.Fatalf("queue height = %d, want exact height 3", got[0])
	}
	if got[2] < minScrollableSectionHeight {
		t.Fatalf("installed height = %d, want at least %d", got[2], minScrollableSectionHeight)
	}
	if got[0]+got[1]+got[2] != 22 {
		t.Fatalf("panel height sum = %d, want 22", got[0]+got[1]+got[2])
	}
}

func TestRenderSectionsHeightMatchesListLayoutHeight(t *testing.T) {
	ws := newWorkspaceScreen()
	for i := 0; i < 14; i++ {
		ws.items = append(ws.items, workspaceItem{
			pkg:         Package{Name: "Update", ID: "Update.ID", Version: "1.0", Available: "1.1", Source: "winget"},
			upgradeable: true,
			installed:   "1.0",
			available:   "1.1",
		})
	}
	for i := 0; i < 210; i++ {
		ws.items = append(ws.items, workspaceItem{
			pkg:       Package{Name: "Installed", ID: "Installed.ID", Version: "1.0", Source: "winget"},
			installed: "1.0",
		})
	}

	l := computeLayout(132, 34)
	got := lipgloss.Height(ws.renderSections(l))
	if got != l.list.H {
		t.Fatalf("renderSections height = %d, want list.H %d", got, l.list.H)
	}
}

func TestSummaryPanelHeightMatchesAssignedHeight(t *testing.T) {
	sp := newSummaryPanel()
	sp.setSize(46, 34)
	sp.pkg = &Package{Name: "Microsoft Teams", ID: "Microsoft.Teams", Version: "1.0", Available: "1.1", Source: "winget"}
	sp.installed = "1.0"
	sp.detail = &PackageDetail{
		Publisher:   "Microsoft Corporation",
		License:     "Proprietary",
		Homepage:    "https://www.microsoft.com/microsoft-teams",
		Description: "Working together is easier with Microsoft Teams.",
	}

	got := lipgloss.Height(sp.view())
	if got != sp.height {
		t.Fatalf("summaryPanel height = %d, want assigned height %d", got, sp.height)
	}
}
