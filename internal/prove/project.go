package prove

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/alainrk/paranoid/internal/hotspot"
	"github.com/alainrk/paranoid/internal/sandbox"
)

// DefaultToolchain is the Lean toolchain of a Lake project that
// `formal prove` creates, when the repo has no lean-toolchain file and
// the user passes none. The Int8 to Int64 types the models use need a
// recent Lean.
const DefaultToolchain = "leanprover/lean4:v4.24.0"

var toolchainRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/:+-]*$`)

var formalWordRe = regexp.MustCompile(`\bFormal\b`)

// lakefileText is the lakefile of a project that prove creates. The
// Formal library holds every module under Formal/.
const lakefileText = `name = "paranoid_formal"
defaultTargets = ["Formal"]

[[lean_lib]]
name = "Formal"
globs = ["Formal.+"]
`

// Project is the Lake project that holds the models.
type Project struct {
	// Info is the detected project. Info.Dir is "" or "formal".
	Info sandbox.ProofProject
	// RepoRoot is the absolute repo root.
	RepoRoot string
	// LakeRoot is the absolute folder with the lakefile.
	LakeRoot string
	// Created lists the slash paths, relative to the repo root, of the
	// files Setup wrote.
	Created []string
}

// Setup finds the Lake project to use, or creates formal/ with a
// lakefile.toml and a lean-toolchain. A project at the repo root wins,
// then formal/. The toolchain of a new project is toolchain when given,
// else the repo's own lean-toolchain file, else DefaultToolchain. With
// create false (the continue step) nothing is written, and a missing
// project is an error.
func Setup(repoRoot, toolchain string, create bool) (Project, error) {
	p := Project{RepoRoot: repoRoot}
	info, reason, ok := sandbox.DetectProof(repoRoot)
	switch {
	case ok && info.Kind != sandbox.KindLean:
		return p, errors.New("the repo root holds a Rocq project; formal prove writes Lean models and needs a Lean project (it can create formal/ only when the root has no proof project)")
	case ok:
		p.Info = info
	case sandbox.IsLakeProject(repoRoot) || sandbox.IsLakeProject(filepath.Join(repoRoot, sandbox.FormalDir)):
		return p, fmt.Errorf("the Lean project cannot be used: %s", reason)
	case !create:
		return p, fmt.Errorf("no Lean project found at the repo root or in %s/; run `paranoid formal prove <file>:<symbol>` first", sandbox.FormalDir)
	default:
		if err := p.create(toolchain); err != nil {
			return p, err
		}
		info, reason, ok = sandbox.DetectProof(repoRoot)
		if !ok {
			return p, fmt.Errorf("created %s/ but it is still not a usable Lean project: %s", sandbox.FormalDir, reason)
		}
		p.Info = info
	}
	p.LakeRoot = repoRoot
	if p.Info.Dir != "" {
		p.LakeRoot = filepath.Join(repoRoot, filepath.FromSlash(p.Info.Dir))
	}
	if len(p.Created) == 0 {
		if err := p.checkFormalLib(); err != nil {
			return p, err
		}
	}
	return p, nil
}

// checkFormalLib refuses a Lake project whose lakefile does not mention
// a Formal library, because the models live in Formal/Paranoid/.
func (p Project) checkFormalLib() error {
	var text strings.Builder
	for _, n := range []string{"lakefile.toml", "lakefile.lean"} {
		raw, err := os.ReadFile(filepath.Join(p.LakeRoot, n)) // #nosec G304 -- fixed name in the project folder
		if err == nil {
			text.Write(raw)
		}
	}
	if !formalWordRe.MatchString(text.String()) {
		return errors.New("the Lake project has no library named Formal; add one (in lakefile.toml: [[lean_lib]] with name = \"Formal\" and globs = [\"Formal.+\"]) so models can live in Formal/Paranoid/")
	}
	return nil
}

// create writes the missing files of formal/.
func (p *Project) create(toolchain string) error {
	if toolchain == "" {
		toolchain = repoToolchain(p.RepoRoot)
	}
	if toolchain == "" {
		toolchain = DefaultToolchain
	}
	if !toolchainRe.MatchString(toolchain) {
		return fmt.Errorf("%q is not a usable Lean toolchain name (for example %s)", toolchain, DefaultToolchain)
	}
	dir := filepath.Join(p.RepoRoot, sandbox.FormalDir)
	if err := os.MkdirAll(dir, 0o755); err != nil { // #nosec G301 -- a normal project folder
		return fmt.Errorf("creating %s/: %w", sandbox.FormalDir, err)
	}
	files := []struct{ name, body string }{
		{"lean-toolchain", toolchain + "\n"},
		{"lakefile.toml", lakefileText},
		{".gitignore", ".lake/\n"},
	}
	for _, f := range files {
		target := filepath.Join(dir, f.name)
		if _, err := os.Stat(target); err == nil {
			continue
		}
		if f.name == "lakefile.toml" && sandbox.IsLakeFileIn(dir) {
			continue
		}
		if err := os.WriteFile(target, []byte(f.body), 0o644); err != nil { // #nosec G306 -- project files meant to be committed
			return fmt.Errorf("writing %s/%s: %w", sandbox.FormalDir, f.name, err)
		}
		p.Created = append(p.Created, path.Join(sandbox.FormalDir, f.name))
	}
	return nil
}

// repoToolchain reads the lean-toolchain file at the repo root, or
// returns "".
func repoToolchain(root string) string {
	raw, err := os.ReadFile(filepath.Join(root, "lean-toolchain")) // #nosec G304 -- fixed name under the repo root
	if err != nil {
		return ""
	}
	for _, l := range strings.Split(string(raw), "\n") {
		if t := strings.TrimSpace(l); t != "" {
			return t
		}
	}
	return ""
}

// srcPrefix is the folder inside the Lake project that holds module
// sources: the first srcDir of the lakefile, or "".
func (p Project) srcPrefix() string {
	if len(p.Info.SrcDirs) > 0 {
		return p.Info.SrcDirs[0]
	}
	return ""
}

// ModelFile returns the slash path of the model file relative to the
// repo root. This is the value the lock file records as `file`.
func (p Project) ModelFile(h *hotspot.Hotspot) string {
	return path.Join(p.Info.Dir, p.srcPrefix(), h.LeanFile)
}

// ModelPath returns the absolute path of the model file.
func (p Project) ModelPath(h *hotspot.Hotspot) string {
	return filepath.Join(p.RepoRoot, filepath.FromSlash(p.ModelFile(h)))
}

// Module returns the Lean module name of the model, such as
// Formal.Paranoid.UtilAdd.
func Module(h *hotspot.Hotspot) string {
	return strings.TrimSuffix(strings.ReplaceAll(h.LeanFile, "/", "."), ".lean")
}
