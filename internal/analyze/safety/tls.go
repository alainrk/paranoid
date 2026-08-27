package safety

import (
	"regexp"

	"github.com/alainrk/paranoid/internal/analyze/testintegrity/common"
	"github.com/alainrk/paranoid/internal/finding"
)

// tlsPatterns match a line that turns off TLS/SSL certificate
// verification. Each entry is a distinct, well-known way to do this
// across the languages the tool already knows about.
var tlsPatterns = []*regexp.Regexp{
	regexp.MustCompile(`InsecureSkipVerify\s*:\s*true`),                // Go crypto/tls
	regexp.MustCompile(`\bverify\s*=\s*False\b`),                       // Python requests/httpx
	regexp.MustCompile(`rejectUnauthorized\s*:\s*false`),               // Node https/tls options
	regexp.MustCompile(`NODE_TLS_REJECT_UNAUTHORIZED\s*[:=]\s*['"]?0`), // Node env override
	regexp.MustCompile(`\bcurl\b[^\n]*(\s-k\b|\s--insecure\b)`),        // curl in a script
}

// analyzeTLS implements SF001: TLS verification newly disabled. Test
// files are skipped entirely, since a test that talks to a local test
// server with a self-signed certificate is a common, legitimate use of
// these same flags. Documentation prose is skipped too (see
// isProseFile): an example pattern written in a README or a rule doc
// is not a real occurrence.
func analyzeTLS(ch FileChange) []finding.Finding {
	if common.IsTestFile(ch.Path) || isProseFile(ch.Path) {
		return nil
	}
	_, added := diffMarks(splitLines(ch.Base), splitLines(ch.Head), tlsPatterns)
	var out []finding.Finding
	for _, m := range added {
		out = append(out, newFinding("SF001", ch.Path, m.line, m.text,
			"TLS certificate verification was newly disabled.",
			"Restore certificate verification, or explain why this connection is exempt."))
	}
	return out
}
