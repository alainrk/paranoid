package python

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alainrk/paranoid/internal/analyze/testintegrity/common"
	"github.com/alainrk/paranoid/internal/finding"
)

const path = "test_calc.py"

func modified(base, head string) []finding.Finding {
	return Analyze([]common.FileChange{{Path: path, Kind: common.FileModified, Base: []byte(base), Head: []byte(head)}})
}

func ruleIDs(findings []finding.Finding) []string {
	out := make([]string, 0, len(findings))
	for _, f := range findings {
		out = append(out, f.RuleID)
	}
	return out
}

func TestDeletedFile(t *testing.T) {
	base := "def test_a():\n    assert 1 == 2\n\ndef test_b():\n    assert True is True\n"
	got := Analyze([]common.FileChange{{Path: path, Kind: common.FileDeleted, Base: []byte(base)}})
	require.Len(t, got, 1)
	require.Equal(t, "TI001", got[0].RuleID)
	require.Equal(t, "test file with 2 tests deleted", got[0].Evidence)
}

func TestRemovedTest(t *testing.T) {
	base := "def test_a():\n    assert f() == 1\n\ndef test_b():\n    assert g() == 2\n"
	head := "def test_a():\n    assert f() == 1\n"
	got := modified(base, head)
	require.Equal(t, []string{"TI002", "TI010"}, ruleIDs(got))
	require.Contains(t, got[0].Evidence, "test_b was at base line 4")
}

func TestRenameWithSameBodyDoesNotFire(t *testing.T) {
	base := "def test_old_name():\n    assert f() == 1\n"
	head := "def test_new_name():\n    assert f() == 1\n"
	require.Empty(t, modified(base, head))
}

func TestAssertionRemoved(t *testing.T) {
	base := "def test_a():\n    assert f() == 1\n    assert g() == 2\n    assert h() == 3\n    assert i() == 4\n    assert j() == 5\n"
	head := "def test_a():\n    assert f() == 1\n    assert g() == 2\n    assert h() == 3\n    assert i() == 4\n"
	got := modified(base, head)
	require.Equal(t, []string{"TI003"}, ruleIDs(got))
	require.Equal(t, "assertions in test_a went from 5 to 4", got[0].Evidence)
	require.Equal(t, 1, got[0].Line)
}

func TestSkipAddedToExistingTest(t *testing.T) {
	base := "import pytest\n\ndef test_a():\n    assert f() == 1\n"
	head := "import pytest\n\n@pytest.mark.skip(reason='flaky')\ndef test_a():\n    assert f() == 1\n"
	got := modified(base, head)
	require.Equal(t, []string{"TI004"}, ruleIDs(got))
	require.Equal(t, 3, got[0].Line)
}

func TestConditionalSkipAddedToExistingTestFires(t *testing.T) {
	base := "def test_a():\n    assert f() == 1\n"
	head := "@pytest.mark.skipif(sys.platform == 'win32', reason='windows')\ndef test_a():\n    assert f() == 1\n"
	got := modified(base, head)
	require.Equal(t, []string{"TI004"}, ruleIDs(got))
}

func TestNewTestWithUnconditionalSkip(t *testing.T) {
	base := "def test_a():\n    assert f() == 1\n"
	head := "def test_a():\n    assert f() == 1\n\n@pytest.mark.skip\ndef test_b():\n    assert g() == 2\n"
	got := modified(base, head)
	require.Equal(t, []string{"TI004"}, ruleIDs(got))
	require.Contains(t, got[0].Evidence, "new test test_b skips itself")
}

func TestNewTestWithConditionalSkipDoesNotFire(t *testing.T) {
	base := "def test_a():\n    assert f() == 1\n"
	head := "def test_a():\n    assert f() == 1\n\n@pytest.mark.skipif(not has_db(), reason='needs db')\ndef test_b():\n    assert g() == 2\n"
	require.Empty(t, modified(base, head))
}

func TestPytestmarkSkipAdded(t *testing.T) {
	base := "import pytest\n\ndef test_a():\n    assert f() == 1\n"
	head := "import pytest\n\npytestmark = pytest.mark.skip\n\ndef test_a():\n    assert f() == 1\n"
	got := modified(base, head)
	require.Equal(t, []string{"TI004"}, ruleIDs(got))
	require.Equal(t, 3, got[0].Line)
}

func TestTautologyAdded(t *testing.T) {
	base := "def test_a():\n    assert f() == 1\n"
	head := "def test_a():\n    assert f() == 1\n    assert True\n    assert 1 == 1\n    assert x == x\n"
	got := modified(base, head)
	require.Equal(t, []string{"TI006", "TI006", "TI006"}, ruleIDs(got))
}

func TestTautologyAtBaseNotReReported(t *testing.T) {
	base := "def test_a():\n    assert True\n    assert f() == 1\n"
	head := "def test_a():\n    assert True\n    assert f() == 1\n    assert g() == 2\n"
	require.Empty(t, modified(base, head))
}

func TestUnittestTautologies(t *testing.T) {
	base := "class TestX(unittest.TestCase):\n    def test_a(self):\n        self.assertEqual(f(), 1)\n"
	head := "class TestX(unittest.TestCase):\n    def test_a(self):\n        self.assertEqual(f(), 1)\n        self.assertTrue(True)\n        self.assertEqual(x, x)\n"
	got := modified(base, head)
	require.Equal(t, []string{"TI006", "TI006"}, ruleIDs(got))
}

func TestRenamedSoNotCollected(t *testing.T) {
	base := "def test_parse():\n    assert parse('x') == 1\n"
	head := "def parse_check():\n    assert parse('x') == 2\n\ndef parse_test_disabled():\n    assert parse('x') == 1\n"
	got := modified(base, head)
	require.Equal(t, []string{"TI007"}, ruleIDs(got))
	require.Equal(t, "test_parse is now parse_test_disabled", got[0].Evidence)
}

func TestRenamedToStrippedPrefix(t *testing.T) {
	base := "def test_parse():\n    assert parse('x') == 1\n"
	head := "def parse():\n    assert parse('x') == 2\n"
	got := modified(base, head)
	require.Equal(t, []string{"TI007"}, ruleIDs(got))
}

func TestXfailAdded(t *testing.T) {
	base := "def test_a():\n    assert f() == 1\n"
	head := "@pytest.mark.xfail\ndef test_a():\n    assert f() == 1\n"
	got := modified(base, head)
	require.Equal(t, []string{"TI008"}, ruleIDs(got))
	require.Equal(t, 1, got[0].Line)
}

func TestNewXfailTest(t *testing.T) {
	base := "def test_a():\n    assert f() == 1\n"
	head := "def test_a():\n    assert f() == 1\n\n@pytest.mark.xfail\ndef test_b():\n    assert g() == 2\n"
	got := modified(base, head)
	require.Equal(t, []string{"TI008"}, ruleIDs(got))
}

func TestSharpAssertionDrop(t *testing.T) {
	base := "def test_a():\n    assert a() == 1\n    assert b() == 2\n    assert c() == 3\n    assert d() == 4\n"
	head := "def test_a():\n    assert a() == 1\n    assert b() == 2\n"
	got := modified(base, head)
	require.Equal(t, []string{"TI003", "TI010"}, ruleIDs(got))
	require.Equal(t, "total assertions went from 4 to 2", got[1].Evidence)
}

func TestWeakenedAssertion(t *testing.T) {
	base := "class TestX(unittest.TestCase):\n    def test_a(self):\n        self.assertEqual(body, 'full text')\n"
	head := "class TestX(unittest.TestCase):\n    def test_a(self):\n        self.assertIn('text', body)\n"
	got := modified(base, head)
	require.Equal(t, []string{"TI005"}, ruleIDs(got))
	require.Contains(t, got[0].Evidence, "assertEqual replaced by assertIn")
}

func TestToleranceLoosened(t *testing.T) {
	base := "class TestX(unittest.TestCase):\n    def test_a(self):\n        self.assertAlmostEqual(x, y, places=7)\n"
	head := "class TestX(unittest.TestCase):\n    def test_a(self):\n        self.assertAlmostEqual(x, y, places=2)\n"
	got := modified(base, head)
	require.Equal(t, []string{"TI005"}, ruleIDs(got))
	require.Contains(t, got[0].Evidence, "went from 7 to 2")
}

func TestApproxToleranceRaised(t *testing.T) {
	base := "def test_a():\n    assert x == pytest.approx(y, rel=1e-6)\n"
	head := "def test_a():\n    assert x == pytest.approx(y, rel=1e-2)\n"
	got := modified(base, head)
	require.Equal(t, []string{"TI005"}, ruleIDs(got))
}

func TestNewMocking(t *testing.T) {
	base := "def test_a():\n    assert api.get() == 1\n"
	head := "def test_a(monkeypatch):\n    monkeypatch.setattr(api, 'get', lambda: 1)\n    assert api.get() == 1\n"
	got := modified(base, head)
	require.Equal(t, []string{"TI012"}, ruleIDs(got))
}

func TestNewFileWithMockDoesNotFireTI012(t *testing.T) {
	head := "def test_a(monkeypatch):\n    monkeypatch.setattr(api, 'get', lambda: 1)\n    assert api.get() == 1\n"
	got := Analyze([]common.FileChange{{Path: path, Kind: common.FileAdded, Head: []byte(head)}})
	require.Empty(t, got)
}

func TestNewFileWithSkipAndTautology(t *testing.T) {
	head := "import pytest\n\n@pytest.mark.skip\ndef test_a():\n    assert True\n"
	got := Analyze([]common.FileChange{{Path: path, Kind: common.FileAdded, Head: []byte(head)}})
	require.Equal(t, []string{"TI004", "TI006"}, ruleIDs(got))
}

func TestFlakyMarkAdded(t *testing.T) {
	base := "def test_a():\n    assert f() == 1\n"
	head := "@pytest.mark.flaky(reruns=3)\ndef test_a():\n    assert f() == 1\n"
	got := modified(base, head)
	require.Equal(t, []string{"TI013"}, ruleIDs(got))
}

func TestBareFlakyDecoratorOnNewTest(t *testing.T) {
	head := "@flaky\ndef test_a():\n    assert f() == 1\n"
	got := Analyze([]common.FileChange{{Path: path, Kind: common.FileAdded, Head: []byte(head)}})
	require.Equal(t, []string{"TI013"}, ruleIDs(got))
}

func TestSleepAddedToExistingTest(t *testing.T) {
	base := "def test_a():\n    assert f() == 1\n"
	head := "def test_a():\n    time.sleep(1)\n    assert f() == 1\n"
	got := modified(base, head)
	require.Equal(t, []string{"TI014"}, ruleIDs(got))
	require.Equal(t, finding.Low, got[0].Severity)
}

func TestAsyncioSleepInNewTest(t *testing.T) {
	head := "async def test_a():\n    await asyncio.sleep(1)\n    assert f() == 1\n"
	got := Analyze([]common.FileChange{{Path: path, Kind: common.FileAdded, Head: []byte(head)}})
	require.Equal(t, []string{"TI014"}, ruleIDs(got))
}

func TestPytestTimeoutRaisedBeyondThreshold(t *testing.T) {
	base := "@pytest.mark.timeout(5)\ndef test_a():\n    assert f() == 1\n"
	head := "@pytest.mark.timeout(30)\ndef test_a():\n    assert f() == 1\n"
	got := modified(base, head)
	require.Equal(t, []string{"TI015"}, ruleIDs(got))
	require.Contains(t, got[0].Evidence, "went from 5 to 30")
}

func TestPytestTimeoutRaisedWithinThresholdIsQuiet(t *testing.T) {
	base := "@pytest.mark.timeout(5)\ndef test_a():\n    assert f() == 1\n"
	head := "@pytest.mark.timeout(14)\ndef test_a():\n    assert f() == 1\n"
	require.Empty(t, modified(base, head))
}

func TestSegment(t *testing.T) {
	lines := []string{
		"import pytest",           // 1
		"",                        // 2
		"@pytest.mark.slow",       // 3
		"def test_a():",           // 4
		"    assert f() == 1",     // 5
		"",                        // 6
		"class TestX:",            // 7
		"    def test_b(self):",   // 8
		"        assert g() == 2", // 9
		"",                        // 10
		"def helper():",           // 11
		"    return 3",            // 12
	}
	units := segment(lines)
	require.Len(t, units, 2)
	require.Equal(t, common.Unit{Name: "test_a", Line: 4, Start: 3, End: 6}, units[0])
	require.Equal(t, common.Unit{Name: "test_b", Line: 8, Start: 8, End: 10}, units[1])
}
