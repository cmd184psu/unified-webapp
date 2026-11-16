package runner

import "regexp"

var (
	rateMsgRe  = regexp.MustCompile(`(?i)rate.?limit|usage limit|quota|too many requests|\b429\b|limit reached|out of (credits|usage)|try again (later|in)`)
	netMsgRe   = regexp.MustCompile(`(?i)connection (refused|reset)|dns|no such host|timed? ?out|network|unreachable|ECONN|\b50[234]\b`)
	loginMsgRe = regexp.MustCompile(`(?i)not signed in|log ?in|unauthori[sz]ed|\b401\b`)
)

// ClassifyMessage maps an agent CLI's error text to a failure class: a usage or
// rate limit, an unreachable network, or (anything else, a login problem
// included) a crash.
func ClassifyMessage(msg string) ErrClass {
	switch {
	case loginMsgRe.MatchString(msg):
		return ErrCrash
	case rateMsgRe.MatchString(msg):
		return ErrRateLimit
	case netMsgRe.MatchString(msg):
		return ErrUnreachable
	}
	return ErrCrash
}
