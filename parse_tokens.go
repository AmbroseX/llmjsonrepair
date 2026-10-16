package llmjsonrepair

import "strings"

// parseWhitespaceAndSkipComments parses whitespace and removes comments.
func parseWhitespaceAndSkipComments(text *[]rune, i *int, output *strings.Builder, skipNewline bool) {
	parseWhitespace(text, i, output, skipNewline)
	for parseComment(text, i) {
		parseWhitespace(text, i, output, skipNewline)
	}
}

// parseWhitespace parses whitespace characters.
func parseWhitespace(text *[]rune, i *int, output *strings.Builder, skipNewline bool) {
	isW := isWhitespace
	if !skipNewline {
		isW = isWhitespaceExceptNewline
	}

	for *i < len(*text) && (isW((*text)[*i]) || isSpecialWhitespace((*text)[*i])) {
		if isSpecialWhitespace((*text)[*i]) {
			output.WriteByte(' ') // repair special whitespace
		} else {
			output.WriteRune((*text)[*i])
		}
		*i++
	}
}

// parseComment parses both single-line (//) and multi-line (/* */) comments.
func parseComment(text *[]rune, i *int) bool {
	if *i+1 >= len(*text) || (*text)[*i] != codeSlash {
		return false
	}

	switch (*text)[*i+1] {
	case codeAsterisk:
		for *i < len(*text) && (*i+1 >= len(*text) || (*text)[*i] != codeAsterisk || (*text)[*i+1] != codeSlash) {
			*i++
		}
		if *i+2 <= len(*text) {
			*i += 2
		}
		return true
	case codeSlash:
		for *i < len(*text) && (*text)[*i] != codeNewline {
			*i++
		}
		return true
	default:
		return false
	}
}

// resetOutput replaces the entire output buffer with the given string.
func resetOutput(output *strings.Builder, s string) {
	output.Reset()
	output.WriteString(s)
}

// parseCharacter parses a specific character and adds it to output if it matches.
// 全角的括号、冒号、逗号也算匹配，输出的永远是半角的 code。
func parseCharacter(text *[]rune, i *int, output *strings.Builder, code rune) bool {
	if runeIs(text, *i, code) {
		output.WriteRune(code)
		*i++
		return true
	}
	return false
}

// skipCharacter skips a specific character if it matches.
// 全角的括号、冒号、逗号也算匹配。
func skipCharacter(text *[]rune, i *int, code rune) bool {
	if runeIs(text, *i, code) {
		*i++
		return true
	}
	return false
}

// skipEllipsis skips ellipsis (three dots) in arrays or objects.
func skipEllipsis(text *[]rune, i *int, output *strings.Builder) {
	parseWhitespaceAndSkipComments(text, i, output, true)

	if *i+2 >= len(*text) || (*text)[*i] != codeDot || (*text)[*i+1] != codeDot || (*text)[*i+2] != codeDot {
		return
	}

	*i += 3
	parseWhitespaceAndSkipComments(text, i, output, true)
	skipCharacter(text, i, codeComma)
}

// skipExtraCommas 丢掉连着的多余逗号（逗号之间可以夹空白和注释）。
func skipExtraCommas(text *[]rune, i *int, output *strings.Builder) {
	for {
		j := *i
		var whitespace strings.Builder
		parseWhitespaceAndSkipComments(text, &j, &whitespace, true)
		if !runeIs(text, j, codeComma) {
			return
		}
		output.WriteString(whitespace.String())
		*i = j + 1
	}
}
