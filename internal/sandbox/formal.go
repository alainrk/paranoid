package sandbox

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

// Proof project kinds.
const (
	// KindLean is a Lean 4 project built with Lake.
	KindLean = "lean"
	// KindRocq is a Rocq (Coq) project built with make or dune.
	KindRocq = "rocq"
)

// Default images for proof projects. --formal-image overrides them.
const (
	leanImage = "debian:bookworm-slim"
	rocqImage = "rocq/rocq-prover:9.1"
)

// DefaultFormalTimeout bounds the whole proof re-check. Real proof
// projects are slow to build.
const DefaultFormalTimeout = 30 * time.Minute

// MaxAuditTargets caps how many theorems one re-check audits.
const MaxAuditTargets = 200

// Paths of the generated axiom-audit files, inside the .paranoid
// directory of the exported tree. Build tools skip dot directories, so
// dune does not pick the Rocq file up as part of a theory.
const (
	leanAuditFile = ".paranoid/axioms_audit.lean"
	rocqAuditFile = ".paranoid/axioms_audit.v"
)

// Markers the check script prints. The parser keys on them, so the
// build output and the audit output can share one stream.
const (
	markBuildOK     = "PARANOID-BUILD-OK"
	markBuildFailed = "PARANOID-BUILD-FAILED"
	markAudit       = "PARANOID-AUDIT"
	markTheorem     = "PARANOID-BEGIN "
)

// LogicalDir is one -Q or -R line of a _CoqProject: a directory that
// is mapped to a logical module prefix.
type LogicalDir struct {
	Dir       string // slash path relative to the project root, "" for the root
	Logical   string // module prefix, "" for none
	Recursive bool   // true for -R, false for -Q
	// BuildDir is the directory passed to the audit compile when it
	// differs from Dir. Dune builds into _build/default/<dir>.
	BuildDir string
}

// ProofProject is a detected proof project at the root of an exported
// tree.
type ProofProject struct {
	Kind      string // KindLean or KindRocq
	Image     string
	Toolchain string // Lean: the lean-toolchain value
	Mathlib   bool   // Lean: the project depends on Mathlib
	SrcDirs   []string
	Dune      bool         // Rocq: built with dune
	Dirs      []LogicalDir // Rocq: logical path map
}

// AuditTarget is one theorem whose axioms are asked from the kernel.
type AuditTarget struct {
	Name string // as written in the source (Lean: namespace-qualified)
	File string // slash path of the proof file, relative to the root
}

// TheoremAxioms is the kernel's answer for one theorem.
type TheoremAxioms struct {
	Name   string
	File   string
	Axioms []string // sorted, empty when the theorem is closed
}

// ProofResult is the outcome of one proof re-check.
type ProofResult struct {
	Ran        bool
	SkipReason string // set when Ran is false
	BuildOK    bool
	// BuildErrors holds the first error lines when the build failed.
	BuildErrors string
	// Theorems holds one entry per audited theorem the kernel answered
	// for, sorted by file then name. A theorem the kernel could not
	// resolve is left out.
	Theorems []TheoremAxioms
}

// toolchainRe limits what a lean-toolchain file may hold, because the
// value reaches a shell script.
var toolchainRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/:+-]*$`)

// safePathRe limits paths and module names that reach a shell script.
var safePathRe = regexp.MustCompile(`^[A-Za-z0-9_./+-]+$`)

// logicalRe limits Rocq logical names.
var logicalRe = regexp.MustCompile(`^[A-Za-z0-9_.']*$`)

// srcDirRe finds srcDir settings in a Lake file.
var srcDirRe = regexp.MustCompile(`srcDir\s*(?::=|=)\s*"([^"]+)"`)

// DetectProof inspects an exported tree for a proof project at its
// root. When there is none it returns false and the reason.
func DetectProof(dir string) (ProofProject, string, bool) {
	if fileExists(dir, "lean-toolchain") && (fileExists(dir, "lakefile.lean") || fileExists(dir, "lakefile.toml")) {
		return detectLean(dir)
	}
	if fileExists(dir, "_CoqProject") {
		return detectRocq(dir, false)
	}
	if fileExists(dir, "dune-project") {
		raw := readSmall(dir, "dune-project")
		if strings.Contains(raw, "coq") || strings.Contains(raw, "rocq") {
			return detectRocq(dir, true)
		}
	}
	return ProofProject{}, "no proof project at the repo root (need lean-toolchain with a lakefile, a _CoqProject, or a dune-project with a Coq stanza)", false
}

// readSmall reads a file under dir, returning "" on any error.
func readSmall(dir, name string) string {
	raw, err := os.ReadFile(filepath.Join(dir, name)) // #nosec G304 -- path inside our own export dir
	if err != nil {
		return ""
	}
	return string(raw)
}

func detectLean(dir string) (ProofProject, string, bool) {
	tc := strings.TrimSpace(firstNonEmptyLine(readSmall(dir, "lean-toolchain")))
	if !toolchainRe.MatchString(tc) {
		return ProofProject{}, "the lean-toolchain file has an unsupported value", false
	}
	p := ProofProject{Kind: KindLean, Image: leanImage, Toolchain: tc}
	lakeText := readSmall(dir, "lakefile.toml") + "\n" + readSmall(dir, "lakefile.lean")
	manifest := readSmall(dir, "lake-manifest.json")
	p.Mathlib = strings.Contains(strings.ToLower(lakeText), "mathlib") || strings.Contains(manifest, `"name": "mathlib"`)
	for _, m := range srcDirRe.FindAllStringSubmatch(lakeText, -1) {
		d := strings.Trim(path.Clean(m[1]), "/")
		if d != "." && d != "" && !strings.HasPrefix(d, "..") {
			p.SrcDirs = append(p.SrcDirs, d)
		}
	}
	sort.Strings(p.SrcDirs)
	return p, "", true
}

func detectRocq(dir string, dune bool) (ProofProject, string, bool) {
	p := ProofProject{Kind: KindRocq, Image: rocqImage, Dune: dune}
	if dune {
		p.Dirs = duneTheories(dir)
		if len(p.Dirs) == 0 {
			return ProofProject{}, "the dune-project has no coq.theory stanza the re-check can read", false
		}
		return p, "", true
	}
	p.Dirs = ParseCoqProject(readSmall(dir, "_CoqProject"))
	return p, "", true
}

// ParseCoqProject reads the -Q and -R lines of a _CoqProject. Lines it
// cannot read safely are skipped.
func ParseCoqProject(text string) []LogicalDir {
	var out []LogicalDir
	for _, line := range strings.Split(text, "\n") {
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		f := strings.Fields(line)
		if len(f) != 3 || (f[0] != "-Q" && f[0] != "-R") {
			continue
		}
		dir := strings.Trim(path.Clean(f[1]), "/")
		if dir == "." {
			dir = ""
		}
		logical := strings.Trim(f[2], `"`)
		if (dir != "" && !safePathRe.MatchString(dir)) || strings.HasPrefix(dir, "..") || !logicalRe.MatchString(logical) {
			continue
		}
		out = append(out, LogicalDir{Dir: dir, Logical: logical, Recursive: f[0] == "-R"})
	}
	return out
}

var (
	duneTheoryRe = regexp.MustCompile(`\((?:coq|rocq)\.theory\b`)
	duneNameRe   = regexp.MustCompile(`\(name\s+([A-Za-z0-9_.']+)\s*\)`)
)

// duneTheories finds coq.theory stanzas in dune files at the root and
// one level down, and maps each to its directory. Dune builds into
// _build/default, which becomes the BuildDir.
func duneTheories(dir string) []LogicalDir {
	var out []LogicalDir
	scan := func(rel string) {
		text := readSmall(dir, path.Join(rel, "dune"))
		for _, loc := range duneTheoryRe.FindAllStringIndex(text, -1) {
			rest := text[loc[0]:]
			if end := strings.Index(rest[1:], "\n("); end >= 0 {
				rest = rest[:end+1]
			}
			if m := duneNameRe.FindStringSubmatch(rest); m != nil {
				out = append(out, LogicalDir{Dir: rel, Logical: m[1], Recursive: true, BuildDir: path.Join("_build/default", rel)})
			}
		}
	}
	scan("")
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") && e.Name() != "_build" && safePathRe.MatchString(e.Name()) {
			scan(e.Name())
		}
	}
	return out
}

func firstNonEmptyLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			return l
		}
	}
	return ""
}

// LeanModule maps a Lean file path (relative to the Lake package root)
// to its module name: Proofs/Rev.lean becomes Proofs.Rev. A srcDir
// prefix is stripped first. It returns false for paths it cannot turn
// into a safe module name.
func (p ProofProject) LeanModule(file string) (string, bool) {
	file = strings.TrimPrefix(path.Clean(strings.ReplaceAll(file, `\`, "/")), "./")
	if !strings.HasSuffix(file, ".lean") {
		return "", false
	}
	for _, d := range p.SrcDirs {
		if strings.HasPrefix(file, d+"/") {
			file = strings.TrimPrefix(file, d+"/")
			break
		}
	}
	parts := strings.Split(strings.TrimSuffix(file, ".lean"), "/")
	for _, part := range parts {
		if !safeIdent(part) || strings.Contains(part, ".") {
			return "", false
		}
	}
	return strings.Join(parts, "."), true
}

// RocqModule maps a Rocq file path to its fully qualified module name
// using the project's -Q and -R lines. The longest matching directory
// wins. A -Q directory only maps files directly inside it.
func (p ProofProject) RocqModule(file string) (string, bool) {
	file = strings.TrimPrefix(path.Clean(strings.ReplaceAll(file, `\`, "/")), "./")
	if !strings.HasSuffix(file, ".v") {
		return "", false
	}
	base := strings.TrimSuffix(file, ".v")
	best := -1
	var module string
	for _, d := range p.Dirs {
		rel := base
		if d.Dir != "" {
			if !strings.HasPrefix(base, d.Dir+"/") {
				continue
			}
			rel = strings.TrimPrefix(base, d.Dir+"/")
		}
		if !d.Recursive && strings.Contains(rel, "/") {
			continue
		}
		if len(d.Dir) > best {
			best = len(d.Dir)
			segs := strings.Split(rel, "/")
			if d.Logical != "" {
				segs = append([]string{d.Logical}, segs...)
			}
			module = strings.Join(segs, ".")
		}
	}
	if best < 0 {
		return "", false
	}
	for _, seg := range strings.Split(module, ".") {
		if !safeIdent(seg) {
			return "", false
		}
	}
	return module, true
}

// safeIdent reports whether s is a plain identifier: letters, digits,
// and a few punctuation marks. Names go into generated proof files, so
// anything with spaces, quotes, or newlines is refused.
func safeIdent(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r == '_' || r == '.' || r == '\'' || r == '!' || r == '?' || r == '«' || r == '»':
		case r >= '0' && r <= '9', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		case r > 127 && unicode.IsLetter(r):
		default:
			return false
		}
	}
	return true
}

// Plan is a ready-to-run re-check: scripts plus the generated audit
// file. Its fields are exported so tests can inspect them.
type Plan struct {
	Image       string
	Prep        string
	Check       string
	AuditFile   string // file name at the tree root
	AuditSource string
	// Targets are the audit targets that made it into the audit file,
	// in order. Targets whose module path could not be worked out are
	// dropped.
	Targets []AuditTarget
}

// BuildPlan builds the scripts and the audit file for a project and a
// target list. Targets are sorted by file then name and capped at
// MaxAuditTargets.
func (p ProofProject) BuildPlan(targets []AuditTarget) Plan {
	targets = append([]AuditTarget(nil), targets...)
	sort.SliceStable(targets, func(i, j int) bool {
		if targets[i].File != targets[j].File {
			return targets[i].File < targets[j].File
		}
		return targets[i].Name < targets[j].Name
	})
	if len(targets) > MaxAuditTargets {
		targets = targets[:MaxAuditTargets]
	}
	if p.Kind == KindLean {
		return p.leanPlan(targets)
	}
	return p.rocqPlan(targets)
}

func (p ProofProject) leanPlan(targets []AuditTarget) Plan {
	plan := Plan{Image: p.Image, AuditFile: leanAuditFile, Prep: leanPrepScript(p)}
	modSet := map[string]bool{}
	var body strings.Builder
	for _, t := range targets {
		mod, ok := p.LeanModule(t.File)
		if !ok || !safeIdent(t.Name) {
			continue
		}
		modSet[mod] = true
		plan.Targets = append(plan.Targets, t)
		fmt.Fprintf(&body, "#print axioms %s\n", t.Name)
	}
	mods := sortedKeys(modSet)
	var src strings.Builder
	for _, m := range mods {
		fmt.Fprintf(&src, "import %s\n", m)
	}
	src.WriteString(body.String())
	plan.AuditSource = src.String()
	plan.Check = leanCheckScript(mods)
	return plan
}

func (p ProofProject) rocqPlan(targets []AuditTarget) Plan {
	plan := Plan{Image: p.Image, AuditFile: rocqAuditFile, Prep: ""}
	modSet := map[string]bool{}
	var body strings.Builder
	for _, t := range targets {
		mod, ok := p.RocqModule(t.File)
		if !ok || !safeIdent(t.Name) {
			continue
		}
		modSet[mod] = true
		plan.Targets = append(plan.Targets, t)
		fmt.Fprintf(&body, "Goal True. idtac \"%s%s\". Abort.\nPrint Assumptions %s.%s.\n", markTheorem, t.Name, mod, t.Name)
	}
	var src strings.Builder
	for _, m := range sortedKeys(modSet) {
		fmt.Fprintf(&src, "Require Import %s.\n", m)
	}
	src.WriteString(body.String())
	plan.AuditSource = src.String()
	plan.Check = p.rocqCheckScript()
	return plan
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// leanPrepScript is the network-on step for a Lean project. What was
// found by running it for real:
//   - elan-init.sh with --default-toolchain none, then
//     "elan toolchain install <lean-toolchain>", puts a working Lean in
//     ELAN_HOME. ELAN_HOME lives on the shared volume, because every
//     container starts fresh.
//   - "lake resolve-deps" clones every Lake dependency at the revision
//     in the manifest and does not build anything.
//   - With the network off and no git binary, Lake thinks each git
//     dependency's URL "has changed" and deletes and re-clones it. So
//     the step copies the git binary and its shared libraries onto the
//     volume and the check step puts a small git wrapper first on PATH.
//   - Mathlib projects run "lake exe cache get" so the check step does
//     not have to compile Mathlib. A failed cache download is not
//     fatal; the check step then just takes longer.
func leanPrepScript(p ProofProject) string {
	var b strings.Builder
	b.WriteString(`set -e
export DEBIAN_FRONTEND=noninteractive
if command -v apt-get >/dev/null 2>&1; then
  apt-get update -qq >/dev/null
  apt-get install -y -qq --no-install-recommends curl git ca-certificates >/dev/null
fi
export ELAN_HOME=/work/.paranoid/elan
export XDG_CACHE_HOME=/work/.paranoid/cache
mkdir -p /work/.paranoid/bin /work/.paranoid/git/lib
curl -sSfL https://raw.githubusercontent.com/leanprover/elan/master/elan-init.sh -o /tmp/elan-init.sh
sh /tmp/elan-init.sh -y --default-toolchain none --no-modify-path >/dev/null 2>&1
export PATH="$ELAN_HOME/bin:$PATH"
`)
	fmt.Fprintf(&b, "elan toolchain install '%s'\n", p.Toolchain)
	b.WriteString(`cp "$(command -v git)" /work/.paranoid/git/git
ldd "$(command -v git)" | awk '/=> \//{print $3}' | grep -v 'libc\.so' | xargs -r -I{} cp -L {} /work/.paranoid/git/lib/
cat > /work/.paranoid/bin/git <<'WRAP'
#!/bin/sh
LD_LIBRARY_PATH=/work/.paranoid/git/lib exec /work/.paranoid/git/git "$@"
WRAP
chmod +x /work/.paranoid/bin/git
lake resolve-deps
`)
	if p.Mathlib {
		b.WriteString("lake exe cache get || echo 'paranoid: lake exe cache get failed, the build will compile Mathlib'\n")
	}
	b.WriteString("chmod -R a+rwX /work\n")
	return b.String()
}

// leanCheckScript is the network-off step for a Lean project: build,
// then ask the kernel for axioms. After a successful "lake build" it
// also builds the modules of audited files, which are not always part
// of a default target; that extra build is best effort.
func leanCheckScript(mods []string) string {
	var b strings.Builder
	b.WriteString(`cd /work
export ELAN_HOME=/work/.paranoid/elan
export XDG_CACHE_HOME=/work/.paranoid/cache
export PATH="/work/.paranoid/bin:$ELAN_HOME/bin:$PATH"
if lake build > /work/.paranoid/build.log 2>&1; then
  echo ` + markBuildOK + `
else
  echo ` + markBuildFailed + `
  grep -i -m 10 'error' /work/.paranoid/build.log || tail -n 10 /work/.paranoid/build.log
  exit 0
fi
`)
	if len(mods) > 0 {
		b.WriteString("lake build " + strings.Join(mods, " ") + " >/dev/null 2>&1 || true\n")
	}
	b.WriteString("echo " + markAudit + "\n")
	b.WriteString("lake env lean " + leanAuditFile + " 2>&1 || true\n")
	return b.String()
}

// rocqCheckScript is the network-off step for a Rocq project. Rocq 9
// ships only the "rocq" command; older images have coq_makefile and
// coqc, so the script picks whichever exists. "rocq compile" stops at
// the first error, so a theorem the kernel cannot find would hide all
// later answers. The script deletes the failing line and compiles
// again, up to ten times.
func (p ProofProject) rocqCheckScript() string {
	var b strings.Builder
	b.WriteString(`cd /work
if command -v rocq >/dev/null 2>&1; then MK="rocq makefile"; RC="rocq compile"; else MK=coq_makefile; RC=coqc; fi
`)
	var qr []string
	if p.Dune {
		b.WriteString("BUILD='dune build'\n")
	} else {
		b.WriteString("BUILD=\"$MK -f _CoqProject -o Makefile.paranoid && make -f Makefile.paranoid\"\n")
	}
	for _, d := range p.Dirs {
		flag := "-Q"
		if d.Recursive {
			flag = "-R"
		}
		dir := d.Dir
		if d.BuildDir != "" {
			dir = d.BuildDir
		}
		if dir == "" {
			dir = "."
		}
		qr = append(qr, fmt.Sprintf("%s %s '%s'", flag, dir, d.Logical))
	}
	b.WriteString("QR=\"" + strings.ReplaceAll(strings.Join(qr, " "), `"`, `\"`) + "\"\n")
	b.WriteString(`if eval "$BUILD" > /work/.paranoid/build.log 2>&1; then
  echo ` + markBuildOK + `
else
  echo ` + markBuildFailed + `
  grep -i -B1 -m 10 'error' /work/.paranoid/build.log || tail -n 10 /work/.paranoid/build.log
  exit 0
fi
echo ` + markAudit + `
i=0
while [ "$i" -lt 10 ]; do
  eval "$RC $QR ` + rocqAuditFile + `" > /work/.paranoid/audit.out 2>&1 && break
  n=$(sed -n 's/^File ".*axioms_audit\.v", line \([0-9][0-9]*\).*/\1/p' /work/.paranoid/audit.out | head -n 1)
  [ -z "$n" ] && break
  sed -i "${n}d" ` + rocqAuditFile + `
  i=$((i+1))
done
cat /work/.paranoid/audit.out
`)
	return b.String()
}

// RunProof exports nothing itself: exportDir already holds the head
// tree. It writes the audit file there, then runs three containers on
// a shared volume: setup (copy the tree, as root, no network), prep
// (network on, may be empty), and check (network off). A runtime
// failure is an error. Anything the project itself causes (a failed
// install, a failed build, a timeout) is reported in the result.
func (r *Runner) RunProof(ctx context.Context, exportDir string, p ProofProject, targets []AuditTarget, timeout time.Duration) (ProofResult, error) {
	if timeout <= 0 {
		timeout = DefaultFormalTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	plan := p.BuildPlan(targets)
	auditPath := filepath.Join(exportDir, filepath.FromSlash(plan.AuditFile))
	if err := os.MkdirAll(filepath.Dir(auditPath), 0o750); err != nil {
		return ProofResult{}, fmt.Errorf("creating the audit directory: %w", err)
	}
	if err := os.WriteFile(auditPath, []byte(plan.AuditSource), 0o644); err != nil { // #nosec G306 -- a temp export, not a secret
		return ProofResult{}, fmt.Errorf("writing the axiom audit file: %w", err)
	}

	volume := r.name + "-formal"
	if out, code, err := r.run(ctx, r.bin, "volume", "create", volume); err != nil || code != 0 {
		return ProofResult{}, fmt.Errorf("creating the sandbox volume: %s", firstLine(string(out)))
	}
	names := []string{r.name + "-fsetup", r.name + "-fprep", r.name + "-fcheck"}
	defer func() {
		// A killed docker CLI can leave its container running, so
		// remove the containers and the volume by name.
		cleanupCtx, cancel := context.WithTimeout(context.Background(), probeTimeout)
		defer cancel()
		for _, n := range names {
			_, _, _ = r.run(cleanupCtx, r.bin, "rm", "-f", n)
		}
		_, _, _ = r.run(cleanupCtx, r.bin, "volume", "rm", "-f", volume)
	}()

	timedOut := func() ProofResult {
		return ProofResult{Ran: false, SkipReason: fmt.Sprintf("the proof re-check timed out after %s", timeout)}
	}

	// Setup: copy the tree into the volume and make it writable for
	// whatever user the image runs as. Network off.
	setup := []string{
		"run", "--rm", "--name", names[0], "--network=none", "--user", "0:0", "--entrypoint", "sh",
		"-v", exportDir + ":/src:ro", "-v", volume + ":/work", "-w", "/work",
		p.Image, "-c", "mkdir -p /work/.paranoid && cp -a /src/. /work/ && chmod -R a+rwX /work",
	}
	out, code, err := r.run(ctx, r.bin, setup...)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return timedOut(), nil
		}
		return ProofResult{}, fmt.Errorf("proof re-check setup: %w", err)
	}
	if ctx.Err() != nil {
		return timedOut(), nil
	}
	if code != 0 {
		return ProofResult{Ran: false, SkipReason: fmt.Sprintf("the proof image could not be started: %s", lastLines(string(out), 3))}, nil
	}

	if plan.Prep != "" {
		prep := []string{
			"run", "--rm", "--name", names[1], "--user", "0:0", "--entrypoint", "sh",
			"-v", volume + ":/work", "-w", "/work",
			p.Image, "-c", plan.Prep,
		}
		out, code, err = r.run(ctx, r.bin, prep...)
		if err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				return timedOut(), nil
			}
			return ProofResult{}, fmt.Errorf("proof re-check prep: %w", err)
		}
		if ctx.Err() != nil {
			return timedOut(), nil
		}
		if code != 0 {
			return ProofResult{Ran: false, SkipReason: fmt.Sprintf("toolchain or dependency install failed: %s", lastLines(string(out), 3))}, nil
		}
	}

	// Check: same volume, network off. The image's own user and
	// entrypoint stay in place for Rocq, whose opam setup needs them.
	check := []string{"run", "--rm", "--name", names[2], "--network=none", "-v", volume + ":/work", "-w", "/work"}
	if p.Kind == KindLean {
		check = append(check, "--user", "0:0")
	}
	check = append(check, p.Image, "sh", "-c", plan.Check)
	out, code, err = r.run(ctx, r.bin, check...)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return timedOut(), nil
		}
		return ProofResult{}, fmt.Errorf("proof re-check: %w", err)
	}
	if ctx.Err() != nil {
		return timedOut(), nil
	}
	if code != 0 {
		return ProofResult{Ran: false, SkipReason: fmt.Sprintf("the check step failed to run (exit %d): %s", code, lastLines(string(out), 3))}, nil
	}
	return ParseProofOutput(p.Kind, string(out), plan.Targets), nil
}
