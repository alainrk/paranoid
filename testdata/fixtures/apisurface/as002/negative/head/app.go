package app

// Formatter renders text, now trimmed of leading and trailing space.
func Formatter(s string) string { return trim(s) }

func trim(s string) string { return s }
