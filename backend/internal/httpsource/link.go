package httpsource

import "strings"

// linkTarget finds, in the Link headers of a response (RFC 8288), the target of the first link whose relation is rel.
// Only what a next page needs is read: <target>; rel="name". A header that is not in that form is passed over.
func linkTarget(h map[string][]string, rel string) string {
	for name, values := range h {
		if !strings.EqualFold(name, "Link") {
			continue
		}
		for _, v := range values {
			if target, ok := linkIn(v, rel); ok {
				return target
			}
		}
	}
	return ""
}

// linkIn reads one header value, which holds one or more links separated by commas (a comma inside <> or inside quotes
// is no separator).
func linkIn(value, rel string) (string, bool) {
	for _, link := range splitLinks(value) {
		target, relations, ok := parseLink(link)
		if !ok {
			continue
		}
		for _, r := range strings.Fields(strings.ToLower(relations)) {
			if r == strings.ToLower(rel) {
				return target, true
			}
		}
	}
	return "", false
}

func splitLinks(value string) []string {
	var out []string
	depth, quoted, start := 0, false, 0
	for i := 0; i < len(value); i++ {
		switch c := value[i]; {
		case c == '"':
			quoted = !quoted
		case quoted:
		case c == '<':
			depth++
		case c == '>':
			depth = max(depth-1, 0)
		case c == ',' && depth == 0:
			out = append(out, value[start:i])
			start = i + 1
		}
	}
	return append(out, value[start:])
}

// parseLink reads <target>; param=value; ... and gives the target and the value of the rel parameter.
func parseLink(link string) (target, relations string, ok bool) {
	link = strings.TrimSpace(link)
	if !strings.HasPrefix(link, "<") {
		return "", "", false
	}
	end := strings.Index(link, ">")
	if end < 0 {
		return "", "", false
	}
	target = link[1:end]
	for _, param := range strings.Split(link[end+1:], ";") {
		name, value, found := strings.Cut(strings.TrimSpace(param), "=")
		if found && strings.EqualFold(strings.TrimSpace(name), "rel") {
			relations = strings.Trim(strings.TrimSpace(value), `"`)
		}
	}
	return target, relations, relations != "" && target != ""
}
