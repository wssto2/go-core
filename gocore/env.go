package gocore

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

// LoadEnv reads the environment file at path (KEY=value lines, as in a
// ".env") into the process environment, the way godotenv.Load does: a
// variable already set, even to the empty string, keeps its value, so the
// real environment wins over the file. Call it first in main, before New
// reads the environment:
//
//	if err := gocore.LoadEnv(".env"); err != nil && !errors.Is(err, fs.ErrNotExist) {
//	    log.Fatal(err)
//	}
//	app := gocore.New()
//
// A file that does not exist returns an error that is fs.ErrNotExist, so the
// caller decides whether that matters: the example above takes the file when
// there is one; drop the errors.Is to require it. A file that does not parse
// is an error and sets nothing.
//
// The forms read are:
//
//	# a comment line, and a blank line
//	KEY=value                  surrounding spaces are cut; so is " # a trailing comment"
//	export KEY=value           the export prefix is allowed
//	KEY: value                 so is the yaml form
//	KEY="two words # kept"     double quotes keep spaces and #, may span lines, and read \n, \r and \" ;
//	                           a $NAME or ${NAME} set earlier in the file expands in them
//	KEY='literal $NAME \n'     single quotes read nothing
//	KEY=${OTHER}/path          $NAME expands in an unquoted value too; write \$ for a literal dollar
//
// $NAME expands only from variables the file itself set earlier (godotenv's
// rule), not from the process environment.
func LoadEnv(path string) error {
	src, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return fmt.Errorf("gocore: reading the env file: %w", err)
	}

	vars, err := parseEnv(src)
	if err != nil {
		return fmt.Errorf("gocore: parsing the env file %s: %w", path, err)
	}

	for key, value := range vars {
		if _, set := os.LookupEnv(key); set {
			continue
		}

		err = os.Setenv(key, value)
		if err != nil {
			return fmt.Errorf("gocore: setting %s from the env file: %w", key, err)
		}
	}

	return nil
}

var (
	errEnvName     = errors.New("unexpected character in a variable name")
	errEnvQuote    = errors.New("unterminated quoted value")
	escapePair     = regexp.MustCompile(`\\.`)
	unescapePair   = regexp.MustCompile(`\\([^$])`)
	variableInText = regexp.MustCompile(`(\\)?(\$)(\()?\{?([A-Z0-9_]+)?\}?`)
)

// parseEnv reads the whole file; the last of a repeated key wins.
func parseEnv(src []byte) (map[string]string, error) {
	vars := map[string]string{}
	rest := bytes.ReplaceAll(src, []byte("\r\n"), []byte("\n"))

	for {
		rest = statementStart(rest)
		if rest == nil {
			return vars, nil
		}

		key, afterKey, err := envKey(rest)
		if err != nil {
			return nil, err
		}

		value, afterValue, err := envValue(afterKey, vars)
		if err != nil {
			return nil, err
		}

		vars[key] = value
		rest = afterValue
	}
}

// statementStart skips blank and comment lines; nil is the end of the file.
func statementStart(src []byte) []byte {
	for {
		src = bytes.TrimLeftFunc(src, unicode.IsSpace)
		if len(src) == 0 {
			return nil
		}

		if src[0] != '#' {
			return src
		}

		end := bytes.IndexByte(src, '\n')
		if end < 0 {
			return nil
		}

		src = src[end:]
	}
}

// envKey reads "KEY=" or "export KEY:" and returns what follows it.
func envKey(src []byte) (string, []byte, error) {
	if trimmed, ok := bytes.CutPrefix(src, []byte("export")); ok && len(trimmed) > 0 && isBlank(rune(trimmed[0])) {
		src = bytes.TrimLeftFunc(trimmed, isBlank)
	}

	for at, char := range string(src) {
		switch {
		case char == '=' || char == ':':
			return strings.TrimRightFunc(string(src[:at]), unicode.IsSpace), bytes.TrimLeftFunc(src[at+1:], isBlank), nil
		case isBlank(char), char == '_', char == '.', unicode.IsLetter(char), unicode.IsNumber(char):
		default:
			return "", nil, fmt.Errorf("%w: %q near %q", errEnvName, char, firstLine(src))
		}
	}

	return "", nil, fmt.Errorf("%w: no = after %q", errEnvName, firstLine(src))
}

// envValue reads one value and returns what follows it.
func envValue(src []byte, vars map[string]string) (string, []byte, error) {
	if len(src) == 0 {
		return "", nil, nil
	}

	if quote := src[0]; quote == '"' || quote == '\'' {
		return quotedValue(src, quote, vars)
	}

	end := bytes.IndexAny(src, "\r\n")
	if end < 0 {
		end = len(src)
	}

	line := []rune(string(src[:end]))
	for at := len(line) - 1; at > 0; at-- {
		if line[at] == '#' && isBlank(line[at-1]) {
			line = line[:at]

			break
		}
	}

	return expand(strings.TrimFunc(string(line), isBlank), vars), src[end:], nil
}

// quotedValue reads up to the closing quote; only double quotes read escapes and variables.
func quotedValue(src []byte, quote byte, vars map[string]string) (string, []byte, error) {
	for end := 1; end < len(src); end++ {
		if src[end] != quote || src[end-1] == '\\' {
			continue
		}

		value := string(bytes.Trim(src[:end], string(quote)))
		if quote == '"' {
			value = expand(unescape(value), vars)
		}

		return value, src[end+1:], nil
	}

	return "", nil, fmt.Errorf("%w: %s", errEnvQuote, firstLine(src))
}

func unescape(text string) string {
	text = escapePair.ReplaceAllStringFunc(text, func(pair string) string {
		switch pair {
		case `\n`:
			return "\n"
		case `\r`:
			return "\r"
		default:
			return pair
		}
	})

	return unescapePair.ReplaceAllString(text, "$1")
}

func expand(text string, vars map[string]string) string {
	return variableInText.ReplaceAllStringFunc(text, func(match string) string {
		parts := variableInText.FindStringSubmatch(match)

		switch {
		case parts[1] == `\` || parts[3] == "(":
			return match[1:]
		case parts[4] != "":
			return vars[parts[4]]
		default:
			return match
		}
	})
}

// isBlank is a space that does not end the line.
func isBlank(r rune) bool {
	switch r {
	case '\t', '\v', '\f', '\r', ' ', '\u0085', '\u00a0':
		return true
	}

	return false
}

func firstLine(src []byte) string {
	line, _, _ := bytes.Cut(src, []byte("\n"))

	return string(line)
}
