package mask

import "strings"

const Wildcard = "<*>"

const (
	MaxTokens   = 96
	MaxTokenLen = 64
)

const (
	openers = `([{"'<`
	closers = `)]}"',;:.>`
)

func Tokens(line string) []string {
	fields := strings.Fields(line)
	if len(fields) > MaxTokens {
		fields = append(fields[:MaxTokens], "<…>")
	}
	for i, field := range fields {
		fields[i] = Token(field)
	}
	return fields
}

func Token(t string) string {
	if len(t) > MaxTokenLen {
		return Wildcard
	}
	start, end := 0, len(t)
	for start < end && strings.IndexByte(openers, t[start]) >= 0 {
		start++
	}
	for end > start && strings.IndexByte(closers, t[end-1]) >= 0 {
		end--
	}
	core := t[start:end]
	if core == "" || core == Wildcard {
		return t
	}
	masked := maskCore(core)
	if masked == core {
		return t
	}
	return t[:start] + masked + t[end:]
}

func maskCore(core string) string {
	if k := keySplit(core); k > 0 {
		value := maskValue(core[k+1:])
		if value == core[k+1:] {
			return core
		}
		return core[:k+1] + value
	}
	return maskValue(core)
}

func maskValue(v string) string {
	if v == "" || v == Wildcard {
		return v
	}
	if v[0] == '"' || v[0] == '\'' {
		quote, inner, closing := v[:1], v[1:], ""
		if strings.HasSuffix(inner, quote) {
			inner, closing = inner[:len(inner)-1], quote
		}
		if inner != "" && Variable(inner) {
			return quote + Wildcard + closing
		}
		return v
	}
	if strings.IndexByte(v, '/') >= 0 && !datetime(v) {
		return maskPath(v)
	}
	if Variable(v) {
		return Wildcard
	}
	return v
}

func Edges(a, b string) string {
	start := 0
	for start < len(a) && start < len(b) && a[start] == b[start] && strings.IndexByte(openers, a[start]) >= 0 {
		start++
	}
	end := 0
	for end < len(a)-start && end < len(b)-start && a[len(a)-1-end] == b[len(b)-1-end] && strings.IndexByte(closers, a[len(a)-1-end]) >= 0 {
		end++
	}
	return a[:start] + Wildcard + a[len(a)-end:]
}

func Key(token string) string {
	if k := keySplit(token); k > 0 {
		return token[:k+1]
	}
	return ""
}

func keySplit(s string) int {
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '=' || c == ':':
			if i == 0 || !isLetter(s[0]) {
				return -1
			}
			if c == ':' && (i+1 < len(s) && s[i+1] == ':' || strings.Count(s, ":") >= 2 && allHex(s[:i])) {
				return -1
			}
			return i
		case isLetter(c) || isDigit(c) || c == '_' || c == '-' || c == '.':
		default:
			return -1
		}
	}
	return -1
}

func maskPath(path string) string {
	query := ""
	if q := strings.IndexByte(path, '?'); q >= 0 {
		path, query = path[:q], "?"+Wildcard
	}
	parts := strings.Split(path, "/")
	changed := query != ""
	for i, part := range parts {
		if part != "" && Variable(part) {
			parts[i] = Wildcard
			changed = true
		}
	}
	if !changed {
		return path
	}
	return strings.Join(parts, "/") + query
}

func allHex(s string) bool {
	for i := 0; i < len(s); i++ {
		if !isHex(s[i]) {
			return false
		}
	}
	return true
}

func isLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
func isDigit(c byte) bool  { return c >= '0' && c <= '9' }
func isHex(c byte) bool {
	return isDigit(c) || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

func Variable(s string) bool {
	if s == "" {
		return false
	}
	digits, letters, hex, colons, others := 0, 0, 0, 0, 0
	numeric := true
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case isDigit(c):
			digits++
			hex++
		case isLetter(c):
			letters++
			if isHex(c) {
				hex++
			}
		case c == ':':
			colons++
		case strings.IndexByte(".,+-_/TZ", c) >= 0:
		case c == '@':
			others++
		default:
			others++
			numeric = false
		}
		if !isDigit(c) && strings.IndexByte(".,:+-_/", c) < 0 {
			numeric = false
		}
	}
	if digits == 0 {
		return others == 1 && strings.IndexByte(s, '@') > 0 && strings.IndexByte(s, '.') > 0
	}
	if numeric {
		return true
	}
	if unitSuffix(s) {
		return true
	}
	if len(s) >= 6 && hex+countByte(s, '-') == len(s) {
		return true
	}
	if colons >= 2 && hex+colons+countByte(s, '.') == len(s) {
		return true
	}
	if datetime(s) {
		return true
	}
	if len(s) >= 12 && digits >= 2 && letters > 0 && others == 0 {
		return true
	}
	if strings.IndexByte(s, '@') > 0 && strings.IndexByte(s, '.') > 0 {
		return true
	}
	return len(s) >= 3 && digits*10 >= len(s)*5
}

func countByte(s string, c byte) int {
	return strings.Count(s, string(c))
}

func unitSuffix(s string) bool {
	i := 0
	for i < len(s) && (isDigit(s[i]) || s[i] == '.' || s[i] == ',') {
		i++
	}
	if i == 0 || !isDigit(s[0]) {
		return false
	}
	rest := s[i:]
	if rest == "%" {
		return true
	}
	if len(rest) == 0 || len(rest) > 3 {
		return false
	}
	for j := 0; j < len(rest); j++ {
		if !isLetter(rest[j]) {
			return false
		}
	}
	return true
}

func datetime(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !isDigit(c) && strings.IndexByte("-:.T Z+/,", c) < 0 {
			return false
		}
	}
	return true
}
