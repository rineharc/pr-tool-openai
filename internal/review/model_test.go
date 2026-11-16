package review

import "testing"

func TestParsePatchTracksBothSides(t *testing.T) {
	lines := ParsePatch("@@ -10,3 +20,4 @@ func example\n context\n-removed\n+added\n+more\n")
	if len(lines) != 6 {
		t.Fatalf("expected 6 lines, got %d: %#v", len(lines), lines)
	}
	tests := []struct {
		index      int
		kind       LineKind
		oldLine    int
		newLine    int
		anchorLine int
		anchorSide string
	}{
		{1, Context, 10, 20, 20, "RIGHT"},
		{2, Removed, 11, 0, 11, "LEFT"},
		{3, Added, 0, 21, 21, "RIGHT"},
		{4, Added, 0, 22, 22, "RIGHT"},
	}
	for _, test := range tests {
		line := lines[test.index]
		if line.Kind != test.kind || line.OldLine != test.oldLine || line.NewLine != test.newLine {
			t.Errorf("line %d = %#v", test.index, line)
		}
		anchor, side, ok := line.Anchor()
		if !ok || anchor != test.anchorLine || side != test.anchorSide {
			t.Errorf("anchor %d = %d %s %t", test.index, anchor, side, ok)
		}
	}
}

func TestParsePatchHandlesNoNewlineMarker(t *testing.T) {
	lines := ParsePatch("@@ -1 +1 @@\n-old\n\\ No newline at end of file\n+new\n\\ No newline at end of file")
	if len(lines) != 5 {
		t.Fatalf("expected 5 lines, got %d", len(lines))
	}
	if lines[2].Kind != Header || lines[4].Kind != Header {
		t.Fatalf("no-newline markers should not advance line numbers: %#v", lines)
	}
	if lines[3].NewLine != 1 {
		t.Fatalf("added line number = %d, want 1", lines[3].NewLine)
	}
}
