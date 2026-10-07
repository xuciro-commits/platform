package platformserver

import "time"

// quota paces owned work: each app may make Tenant.Quota attempts per minute
// (ADR-0027 D3), and the apps take turns being served first. Volatile; used
// only by the runner under the tenant's main lock.
type quota struct {
	used map[string]usedMinute // attempts per app in the current minute
	turn int                   // the app the next round starts at
}

type usedMinute struct {
	minute time.Time
	n      int
}

// over reports whether app has used its limit attempts of the minute; 0: no limit.
func (q *quota) over(app string, limit int, now time.Time) bool {
	if limit <= 0 {
		return false
	}
	u := q.used[app]
	return u.minute.Equal(now.Truncate(time.Minute)) && u.n >= limit
}

func (q *quota) spend(app string, now time.Time) {
	if q.used == nil {
		q.used = map[string]usedMinute{}
	}
	u, minute := q.used[app], now.Truncate(time.Minute)
	if !u.minute.Equal(minute) {
		u = usedMinute{minute: minute}
	}
	u.n++
	q.used[app] = u
}

// next advances the round-robin start over n apps.
func (q *quota) next(n int) { q.turn = (q.turn + 1) % max(n, 1) }
