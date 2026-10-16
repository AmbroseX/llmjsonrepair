// 本文件改动：JSON 转义改用标准库 encoding/json。

package llmjsonrepair

import (
	"bytes"
	"encoding/json"
	"strings"
)

// parseNumber parses a number, handling various numeric formats.
func parseNumber(text *[]rune, i *int, output *strings.Builder) bool {
	start := *i
	step := scanMinusSign(text, i)
	if step == numberOK {
		// Preserve leading zeros by quoting the token instead of rewriting its value.
		skipDigits(text, i)
		step = scanFraction(text, i)
	}
	if step == numberOK {
		step = scanExponent(text, i)
	}

	switch step {
	case numberTruncated:
		repairNumberEndingWithNumericSymbol(text, start, i, output)
		return true
	case numberInvalid:
		*i = start
		return false
	}

	if !atEndOfNumber(text, i) || *i <= start {
		*i = start
		return false
	}
	writeNumber(string((*text)[start:*i]), output)
	return true
}

// numberScanStep 是扫描数字的一段（符号、小数、指数）之后的结果。
type numberScanStep int

const (
	numberOK        numberScanStep = iota // 这一段正常，接着往下扫
	numberTruncated                       // 数字停在 - . e 这类符号上就结束了，补个 0
	numberInvalid                         // 符号后面不是数字，这不是数字
)

func skipDigits(text *[]rune, i *int) {
	for *i < len(*text) && isDigit((*text)[*i]) {
		*i++
	}
}

// afterNumericSymbol 检查 - . e 这类符号后面：数字到头了算被截断，不是数字算无效。
func afterNumericSymbol(text *[]rune, i *int) numberScanStep {
	if atEndOfNumber(text, i) {
		return numberTruncated
	}
	if !isDigit((*text)[*i]) {
		return numberInvalid
	}
	return numberOK
}

func scanMinusSign(text *[]rune, i *int) numberScanStep {
	if *i < len(*text) && (*text)[*i] == codeMinus {
		*i++
		return afterNumericSymbol(text, i)
	}
	return numberOK
}

func scanFraction(text *[]rune, i *int) numberScanStep {
	if *i < len(*text) && (*text)[*i] == codeDot {
		*i++
		step := afterNumericSymbol(text, i)
		if step == numberOK {
			skipDigits(text, i)
		}
		return step
	}
	return numberOK
}

func scanExponent(text *[]rune, i *int) numberScanStep {
	if *i < len(*text) && ((*text)[*i] == codeLowercaseE || (*text)[*i] == codeUppercaseE) {
		*i++
		if *i < len(*text) && ((*text)[*i] == codeMinus || (*text)[*i] == codePlus) {
			*i++
		}
		step := afterNumericSymbol(text, i)
		if step == numberOK {
			skipDigits(text, i)
		}
		return step
	}
	return numberOK
}

// writeNumber 写出数字；前导零的（007）原样加引号，不改它的值。
func writeNumber(num string, output *strings.Builder) {
	if leadingZeroRe.MatchString(num) {
		output.WriteByte('"')
		output.WriteString(num)
		output.WriteByte('"')
		return
	}
	output.WriteString(num)
}

// jsonKeywords maps JSON and Python keyword names to their JSON equivalents.
var jsonKeywords = []struct{ name, value string }{
	{"true", "true"}, {"false", "false"}, {"null", "null"},
	{"True", "true"}, {"False", "false"}, {"None", "null"},
}

// parseKeywords parses JSON keywords (true, false, null) and Python keywords (True, False, None).
func parseKeywords(text *[]rune, i *int, output *strings.Builder) bool {
	for _, kw := range jsonKeywords {
		if parseKeyword(text, i, output, kw.name, kw.value) {
			return true
		}
	}
	return false
}

// parseKeyword parses a specific keyword from the input text.
func parseKeyword(text *[]rune, i *int, output *strings.Builder, name, value string) bool {
	end := *i + len(name)
	if end > len(*text) || string((*text)[*i:end]) != name {
		return false
	}
	if end < len(*text) && !isStructuralDelimiter((*text)[end]) && !isWhitespace((*text)[end]) && !isSpecialWhitespace((*text)[end]) {
		return false
	}

	output.WriteString(value)
	*i = end
	return true
}

func hasURLPrefix(text []rune, start int) bool {
	for _, prefix := range []string{"https://", "http://", "ftp://"} {
		end := start + len(prefix)
		if end <= len(text) && strings.EqualFold(string(text[start:end]), prefix) {
			return true
		}
	}
	return false
}

// parseUnquotedStringWithMode parses unquoted strings with a mode parameter to control URL parsing.
func parseUnquotedStringWithMode(text *[]rune, i *int, output *strings.Builder, isKey bool, depth int) (bool, error) {
	start := *i

	if *i >= len(*text) {
		return false, nil
	}

	// Check for function name start (MongoDB/JSONP function calls)
	if isFunctionNameCharStart((*text)[*i]) {
		handled, err := parseFunctionCall(text, i, output, depth)
		if handled || err != nil {
			return handled, err
		}
	}

	// Check if this starts with a URL pattern (only when not parsing a key)
	if !isKey && hasURLPrefix(*text, start) {
		for *i < len(*text) && isURLChar((*text)[*i]) {
			*i++
		}
	} else {
		skipUnquotedText(text, i, isKey)
	}

	if *i <= start {
		return false, nil
	}

	for *i > start && isWhitespace((*text)[*i-1]) {
		*i--
	}

	writeUnquotedSymbol(string((*text)[start:*i]), output)

	if *i < len(*text) && (*text)[*i] == codeDoubleQuote {
		*i++
	}

	return true, nil
}

// parseFunctionCall 读 callback(...) 这种 MongoDB / JSONP 函数调用，只留括号里的值。
// 后面不是左括号就不算函数调用，返回 false，由调用方把名字当不带引号的字符串读。
func parseFunctionCall(text *[]rune, i *int, output *strings.Builder, depth int) (bool, error) {
	for *i < len(*text) && isFunctionNameChar((*text)[*i]) {
		*i++
	}

	j := *i
	for j < len(*text) && (isWhitespace((*text)[j]) || isSpecialWhitespace((*text)[j])) {
		j++
	}
	if j >= len(*text) || (*text)[j] != codeOpenParenthesis {
		return false, nil
	}

	childDepth, err := nextNestingDepth(depth, j)
	if err != nil {
		return false, err
	}

	*i = j + 1
	if _, err := parseValue(text, i, output, childDepth); err != nil {
		return false, err
	}

	if *i < len(*text) && (*text)[*i] == codeCloseParenthesis {
		*i++
		if *i < len(*text) && (*text)[*i] == codeSemicolon {
			*i++
		}
	}
	return true, nil
}

// skipUnquotedText 往后走到不带引号的字符串结束处：碰到分隔符或引号停；读键时碰到冒号也停。
func skipUnquotedText(text *[]rune, i *int, isKey bool) {
	for *i < len(*text) && !isUnquotedStringDelimiter(structuralRune((*text)[*i])) && !isQuote((*text)[*i]) {
		if isKey && structuralRune((*text)[*i]) == codeColon {
			return
		}
		*i++
	}
}

// writeUnquotedSymbol 把不带引号的内容加上引号写出；undefined 写成 null。
func writeUnquotedSymbol(symbol string, output *strings.Builder) {
	if symbol == "undefined" {
		output.WriteString("null")
		return
	}
	output.WriteByte('"')
	for _, char := range symbol {
		if isSingleQuoteLike(char) || isDoubleQuoteLike(char) {
			output.WriteByte('"')
		} else {
			output.WriteRune(char)
		}
	}
	output.WriteByte('"')
}

func isRegexFlag(char rune) bool {
	switch char {
	case 'd', 'g', 'i', 'm', 's', 'u', 'v', 'y':
		return true
	default:
		return false
	}
}

// parseRegex parses a regex literal like /pattern/flags and wraps it in quotes.
func parseRegex(text *[]rune, i *int, output *strings.Builder) bool {
	if *i >= len(*text) || (*text)[*i] != codeSlash {
		return false
	}

	start := *i
	*i++
	inCharClass := false
	escaped := false
	closed := false

	for *i < len(*text) && !closed {
		char := (*text)[*i]
		switch {
		case escaped:
			escaped = false
		case char == codeBackslash:
			escaped = true
		case char == codeOpeningBracket:
			inCharClass = true
		case char == codeClosingBracket:
			inCharClass = false
		case char == codeSlash && !inCharClass:
			closed = true
		}
		*i++
	}

	for *i < len(*text) && isRegexFlag((*text)[*i]) {
		*i++
	}

	// json.Marshal properly escapes quotes, backslashes, and other special
	// characters in the regex content, preventing XSS when repaired JSON is
	// parsed with eval. See josdejong/jsonrepair#150.
	// json.Marshal cannot fail for a valid UTF-8 string derived from runes.
	regexContent := string((*text)[start:*i])
	// 标准库默认会把 < > & 转成 < 等，原库（go-json-experiment）不转，这里关掉保持一致。
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(regexContent)
	output.Write(bytes.TrimSuffix(buf.Bytes(), []byte("\n")))
	return true
}
