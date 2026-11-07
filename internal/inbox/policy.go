package inbox

import "time"

func rank(severity string) int {
	switch severity {
	case "info":
		return 0
	case "normal":
		return 1
	case "urgent":
		return 2
	}
	return -1
}

func minutes(hhmm string) int {
	t, err := time.Parse("15:04", hhmm)
	if err != nil {
		return 0
	}
	return t.Hour()*60 + t.Minute()
}

func zone() *time.Location {
	if loc, err := time.LoadLocation("Asia/Shanghai"); err == nil {
		return loc
	}
	return time.FixedZone("CST", 8*3600)
}

func inQuiet(now time.Time, q Quiet) bool {
	if q.From == "" || q.To == "" {
		return false
	}
	n := now.In(zone())
	m := n.Hour()*60 + n.Minute()
	start, end := minutes(q.From), minutes(q.To)
	if start < end {
		return m >= start && m < end
	}
	return m >= start || m < end
}

// quietEnd is the first moment after now when the quiet stretch is over.
func quietEnd(now time.Time, q Quiet) int64 {
	n := now.In(zone())
	end := minutes(q.To)
	target := time.Date(n.Year(), n.Month(), n.Day(), end/60, end%60, 0, 0, zone())
	if !target.After(now) {
		target = target.AddDate(0, 0, 1)
	}
	return target.UnixMilli()
}

type planned struct {
	channel Channel
	after   int64
}

type plan struct {
	deliver    []planned
	suppressed string
}

// decide says which channels an item goes to and when. The inbox always keeps it: the policy only
// decides whether anyone is bothered.
func (in *Inbox) decide(it *Item, spec Type, now time.Time, occurred int64, collapsed bool, opt Options) plan {
	s := in.settings()
	r := rank(it.Severity)
	if !s.Enabled {
		return plan{suppressed: "disabled"}
	}
	ts := s.Types[it.Type]
	if ts.Enabled != nil && !*ts.Enabled {
		return plan{suppressed: "type_off"}
	}
	if spec.TTL > 0 && now.UnixMilli()-occurred > spec.TTL.Milliseconds() {
		return plan{suppressed: "expired"}
	}
	if r <= 0 {
		return plan{suppressed: "info_only"}
	}
	if r == 1 && collapsed {
		return plan{suppressed: "collapsed"}
	}
	if r == 1 && it.Type != "digest.summary" && s.Digest.Enabled && len(ts.Channels) == 0 && !opt.Local {
		return plan{suppressed: "digest"}
	}
	var p plan
	for _, c := range s.Channels {
		if !c.Enabled || rank(c.MinSeverity) > r {
			continue
		}
		if opt.NoDesktop && c.Type == "desktop" {
			continue
		}
		if opt.Local && c.Type != "desktop" {
			continue
		}
		if len(ts.Channels) > 0 && !contains(ts.Channels, c.ID) {
			continue
		}
		quiet := s.Quiet
		if c.Quiet != nil {
			quiet = *c.Quiet
		}
		after := now.UnixMilli()
		if inQuiet(now, quiet) && (r < 2 || !quiet.Urgent) {
			after = quietEnd(now, quiet)
		}
		p.deliver = append(p.deliver, planned{c, after})
	}
	return p
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
