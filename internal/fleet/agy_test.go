package fleet

import "testing"

func TestCheckAgyWaiting(t *testing.T) {
	cases := []struct {
		txt      string
		wantWait bool
		wantNote string
	}{
		{
			txt:      "Requesting permission for:\n  git stash push\nRun this command?\n> 1. Yes, run command\n  4. No, cancel\n",
			wantWait: true,
			wantNote: "permission",
		},
		{
			txt:      "Are you sure you want to proceed?\n[Y/n]\n",
			wantWait: true,
			wantNote: "permission",
		},
		{
			txt:      "Select configuration:\n> 1. (Recommended) Default\n  2. Custom\n",
			wantWait: true,
			wantNote: "your input",
		},
		{
			txt:      "Running tests...\nPASS\nok  test 0.05s\n",
			wantWait: false,
			wantNote: "",
		},
	}

	for _, c := range cases {
		gotWait, gotNote := checkAgyWaiting(c.txt)
		if gotWait != c.wantWait || gotNote != c.wantNote {
			t.Errorf("checkAgyWaiting(%q) = (%v, %q), want (%v, %q)", c.txt, gotWait, gotNote, c.wantWait, c.wantNote)
		}
	}
}
