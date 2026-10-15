package fleet

import (
	"os"
	"path/filepath"
	"testing"
)

func TestQuota(t *testing.T) {
	sd := t.TempDir()
	if loadQuota(sd, 1000) != nil {
		t.Error("no file must be no quota, not a zero one")
	}
	write := func(s string) { os.WriteFile(filepath.Join(sd, "quota"), []byte(s), 0o600) }

	now := int64(100000)
	write("58 104800 41 600000 -1 0 99000\n")
	q := loadQuota(sd, now)
	if q == nil || q.FiveHour.Pct == nil || *q.FiveHour.Pct != 58 || *q.SevenDay.Pct != 41 {
		t.Fatalf("fresh figures: %+v", q)
	}
	if q.Spend != nil {
		t.Error("an unreported spend limit must be absent, not 0%")
	}

	// the 5h window reset since it was seen: unknown, not the old 58
	write("58 99500 41 600000 -1 0 99000\n")
	if q := loadQuota(sd, now); q.FiveHour.Pct != nil || q.SevenDay.Pct == nil {
		t.Errorf("past 5h reset: %+v %+v", q.FiveHour, q.SevenDay)
	}
	// no reset given, but seen longer ago than the window is long
	write("58 0 41 0 -1 0 80000\n")
	if q := loadQuota(sd, now); q.FiveHour.Pct != nil || q.SevenDay.Pct == nil {
		t.Errorf("seen 5.5h ago: %+v %+v", q.FiveHour, q.SevenDay)
	}
	write("58 0 41 0 -1 0 1\n")
	if q := loadQuota(sd, now*10); q.SevenDay.Pct != nil {
		t.Error("a weekly figure older than a week must read unknown")
	}
	for _, bad := range []string{"", "garbage", "1 2 3", "a b c d e f g"} {
		write(bad)
		if loadQuota(sd, now) != nil {
			t.Errorf("unreadable %q must be no quota", bad)
		}
	}
}
