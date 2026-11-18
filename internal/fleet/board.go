package fleet

// Diff is an agent's checkout measured against the branch it would land on:
// `git diff --shortstat <base>`, committed and uncommitted together.
type Diff struct {
	Added   int `json:"added"`
	Removed int `json:"removed"`
}

// PR is the pull request an agent's branch heads. CI is null when kel could
// not ask (gh failed, or `pr = off`), never a guess.
type PR struct {
	Number int     `json:"number"`
	State  string  `json:"state"` // open | merged | closed
	CI     *string `json:"ci"`    // pass | fail | pending
}

// Transition is one line of <pane-id>.log: when the agent entered a state,
// and the note it entered it with.
type Transition struct {
	At    float64 `json:"at"`
	State string  `json:"state"`
	Note  string  `json:"note,omitempty"`
}
