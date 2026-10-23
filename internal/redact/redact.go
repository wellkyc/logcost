package redact

import "regexp"

const Placeholder = "<redacted>"

var authScheme = regexp.MustCompile(`(?i)^(bearer|basic|token)$`)

type rule struct {
	pattern *regexp.Regexp
	replace func(match []string) string
}

var keyWords = `password|passwd|pwd|passphrase|secret|token|api[_-]?key|access[_-]?key|private[_-]?key|credential|authorization|auth|session[_-]?id|sessionid|cookie|signature`

var rules = []rule{
	{
		pattern: regexp.MustCompile(`(-----BEGIN [A-Z ]*PRIVATE KEY-----).*`),
		replace: func(m []string) string { return m[1] + Placeholder },
	},
	{
		pattern: regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*://)[^/\s:@]+:[^/\s@]+@`),
		replace: func(m []string) string { return m[1] + Placeholder + "@" },
	},
	{
		pattern: regexp.MustCompile(`(?i)\b(bearer|basic|token)(\s+)[A-Za-z0-9._~+/=-]{8,}`),
		replace: func(m []string) string { return m[1] + m[2] + Placeholder },
	},
	{
		pattern: regexp.MustCompile(`(?i)("?[A-Za-z0-9_.-]*(?:` + keyWords + `)[A-Za-z0-9_.-]*"?\s*[:=]\s*)("[^"]*"|'[^']*'|[^\s,;&"']+)`),
		replace: func(m []string) string {
			value := m[2]
			if value == "<*>" || authScheme.MatchString(value) {
				return m[0]
			}
			if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') {
				return m[1] + value[:1] + Placeholder + value[:1]
			}
			return m[1] + Placeholder
		},
	},
	{
		pattern: regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]*`),
		replace: func([]string) string { return Placeholder },
	},
	{
		pattern: regexp.MustCompile(`\b(?:(?:AKIA|ASIA)[0-9A-Z]{16}|gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|xox[abprs]-[A-Za-z0-9-]{10,}|(?:sk|rk|pk)_(?:live|test)_[A-Za-z0-9]{10,}|AIza[0-9A-Za-z_-]{35}|glpat-[A-Za-z0-9_-]{20,}|npm_[A-Za-z0-9]{30,})\b`),
		replace: func([]string) string { return Placeholder },
	},
	{
		pattern: regexp.MustCompile(`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b`),
		replace: func([]string) string { return "<email>" },
	},
	{
		pattern: regexp.MustCompile(`\b\d(?:[ -]?\d){12,18}\b`),
		replace: func(m []string) string {
			if luhn(m[0]) {
				return "<card>"
			}
			return m[0]
		},
	},
}

func luhn(s string) bool {
	sum, count, double := 0, 0, false
	for i := len(s) - 1; i >= 0; i-- {
		c := s[i]
		if c < '0' || c > '9' {
			continue
		}
		d := int(c - '0')
		if double {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		count++
		double = !double
	}
	return count >= 13 && sum%10 == 0
}

func String(s string) string {
	for _, r := range rules {
		if !r.pattern.MatchString(s) {
			continue
		}
		s = r.pattern.ReplaceAllStringFunc(s, func(match string) string {
			parts := r.pattern.FindStringSubmatch(match)
			if parts == nil {
				return match
			}
			return r.replace(parts)
		})
	}
	return s
}
