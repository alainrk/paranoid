package deps

// This file holds the embedded standard library and builtin lists the
// import extractors use to avoid flagging modules that never live in a
// registry.

// pythonStdlib is the set of top-level standard library module names,
// from CPython 3.12 sys.stdlib_module_names (public names only).
var pythonStdlib = toSet([]string{
	"abc", "aifc", "antigravity", "argparse", "array", "ast", "asyncio",
	"atexit", "audioop", "base64", "bdb", "binascii", "bisect", "builtins",
	"bz2", "cProfile", "calendar", "cgi", "cgitb", "chunk", "cmath", "cmd",
	"code", "codecs", "codeop", "collections", "colorsys", "compileall",
	"concurrent", "configparser", "contextlib", "contextvars", "copy",
	"copyreg", "crypt", "csv", "ctypes", "curses", "dataclasses", "datetime",
	"dbm", "decimal", "difflib", "dis", "doctest", "email", "encodings",
	"ensurepip", "enum", "errno", "faulthandler", "fcntl", "filecmp",
	"fileinput", "fnmatch", "fractions", "ftplib", "functools", "gc",
	"genericpath", "getopt", "getpass", "gettext", "glob", "graphlib",
	"grp", "gzip", "hashlib", "heapq", "hmac", "html", "http", "idlelib",
	"imaplib", "imghdr", "importlib", "inspect", "io", "ipaddress",
	"itertools", "json", "keyword", "linecache", "locale", "logging",
	"lzma", "mailbox", "mailcap", "marshal", "math", "mimetypes", "mmap",
	"modulefinder", "msilib", "msvcrt", "multiprocessing", "netrc", "nis",
	"nntplib", "ntpath", "nturl2path", "numbers", "opcode", "operator",
	"optparse", "os", "ossaudiodev", "pathlib", "pdb", "pickle",
	"pickletools", "pipes", "pkgutil", "platform", "plistlib", "poplib",
	"posix", "posixpath", "pprint", "profile", "pstats", "pty", "pwd",
	"py_compile", "pyclbr", "pydoc", "queue", "quopri", "random", "re",
	"readline", "reprlib", "resource", "rlcompleter", "runpy", "sched",
	"secrets", "select", "selectors", "shelve", "shlex", "shutil", "signal",
	"site", "smtplib", "sndhdr", "socket", "socketserver", "spwd",
	"sqlite3", "sre_compile", "sre_constants", "sre_parse", "ssl", "stat",
	"statistics", "string", "stringprep", "struct", "subprocess", "sunau",
	"symtable", "sys", "sysconfig", "syslog", "tabnanny", "tarfile",
	"telnetlib", "tempfile", "termios", "test", "textwrap", "this",
	"threading", "time", "timeit", "tkinter", "token", "tokenize",
	"tomllib", "trace", "traceback", "tracemalloc", "tty", "turtle",
	"turtledemo", "types", "typing", "unicodedata", "unittest", "urllib",
	"uu", "uuid", "venv", "warnings", "wave", "weakref", "webbrowser",
	"winreg", "winsound", "wsgiref", "xdrlib", "xml", "xmlrpc", "zipapp",
	"zipfile", "zipimport", "zlib", "zoneinfo",
	// Common local-package names that are never registry lookups.
	"__future__",
})

// pythonModuleAliases maps import names to their PyPI distribution
// names where they differ. Only well-known cases are listed; unknown
// modules keep their import name and may mismatch, which the docs
// call out.
var pythonModuleAliases = map[string]string{
	"attr":     "attrs",
	"bs4":      "beautifulsoup4",
	"cv2":      "opencv-python",
	"dateutil": "python-dateutil",
	"dotenv":   "python-dotenv",
	"jwt":      "pyjwt",
	"PIL":      "pillow",
	"sklearn":  "scikit-learn",
	"yaml":     "pyyaml",
}

// nodeBuiltins is the set of Node.js builtin module names.
var nodeBuiltins = toSet([]string{
	"assert", "async_hooks", "buffer", "child_process", "cluster",
	"console", "constants", "crypto", "dgram", "diagnostics_channel",
	"dns", "domain", "events", "fs", "http", "http2", "https", "inspector",
	"module", "net", "os", "path", "perf_hooks", "process", "punycode",
	"querystring", "readline", "repl", "stream", "string_decoder", "sys",
	"timers", "tls", "trace_events", "tty", "url", "util", "v8", "vm",
	"wasi", "worker_threads", "zlib",
})

// toSet builds a string set.
func toSet(names []string) map[string]bool {
	out := make(map[string]bool, len(names))
	for _, name := range names {
		out[name] = true
	}
	return out
}
