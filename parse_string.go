package llmjsonrepair

import (
	"fmt"
	"strings"
)

// stringStep 是读字符串时处理完一个字符（或一小段）之后的结果。
type stringStep struct {
	done bool  // 字符串读完了（或失败了），不用再往下读
	ok   bool  // done 为 true 时，parseString 返回的第一个值
	err  error // done 为 true 时，parseString 返回的错误
}

var (
	stringContinue = stringStep{}
	stringFinished = stringStep{done: true, ok: true}
)

// stringParser 保存读一个字符串过程中要用的状态。
type stringParser struct {
	text            *[]rune
	i               *int
	output          *strings.Builder
	str             strings.Builder // 到目前为止读出来的字符串（带开头的引号）
	isEndQuote      func(rune) bool
	iBefore         int  // 开头引号的位置，重读时回到这里
	oBefore         int  // 开始读之前 output 的长度，重读时截回到这里
	stopAtDelimiter bool // 结尾引号缺失时，碰到第一个分隔符就结束
	stopAtIndex     int  // 重读时在这个位置结束，-1 表示不限
	filePath        bool // 内容像文件路径，反斜杠按字面处理
}

// parseString parses a string, handling various quote and escape scenarios.
// Returns (success, error) where error is non-nil for non-repairable issues.
func parseString(text *[]rune, i *int, output *strings.Builder, stopAtDelimiter bool, stopAtIndex int) (bool, error) {
	if *i >= len(*text) {
		return false, nil
	}

	skipEscapeChars := (*text)[*i] == codeBackslash
	if skipEscapeChars {
		// repair: remove the first escape character
		*i++
	}

	if *i >= len(*text) || !isQuote((*text)[*i]) {
		return false, nil
	}

	p := &stringParser{
		text:            text,
		i:               i,
		output:          output,
		isEndQuote:      endQuoteMatcher((*text)[*i]),
		iBefore:         *i,
		oBefore:         output.Len(),
		stopAtDelimiter: stopAtDelimiter,
		stopAtIndex:     stopAtIndex,
		filePath:        analyzePotentialFilePath(text, *i),
	}
	p.str.WriteRune('"')
	*i++

	for {
		step := p.next()
		if step.done {
			return step.ok, step.err
		}
		if skipEscapeChars {
			// repair: skipped escape character
			skipCharacter(text, i, codeBackslash)
		}
	}
}

// next 读下一个字符（或一小段）。
func (p *stringParser) next() stringStep {
	if *p.i >= len(*p.text) {
		return p.onTextEnd()
	}
	if p.stopAtIndex != -1 && *p.i == p.stopAtIndex {
		// use the stop index detected in the first iteration, and repair end quote
		return p.finishWithMissingQuote()
	}

	char := (*p.text)[*p.i]
	switch {
	case p.isEndQuote(char):
		return p.onQuote()
	case p.stopAtDelimiter && isUnquotedStringDelimiter(char):
		return p.onDelimiterWithoutEndQuote()
	case char == '\\':
		return p.onBackslash()
	default:
		return p.onPlainChar()
	}
}

// finishWithMissingQuote 在字符串末尾（最后一段空白之前）补上结尾引号，结束。
func (p *stringParser) finishWithMissingQuote() stringStep {
	p.output.WriteString(insertBeforeLastWhitespace(p.str.String(), "\""))
	return stringFinished
}

// retry 回到字符串开头，换一种方式重新读。
func (p *stringParser) retry(stopAtDelimiter bool, stopAtIndex int) stringStep {
	*p.i = p.iBefore
	resetOutput(p.output, p.output.String()[:p.oBefore])
	ok, err := parseString(p.text, p.i, p.output, stopAtDelimiter, stopAtIndex)
	return stringStep{done: true, ok: ok, err: err}
}

// onTextEnd 文本读完了还没遇到结尾引号。
func (p *stringParser) onTextEnd() stringStep {
	iPrev := prevNonWhitespaceIndex(*p.text, *p.i-1)
	if !p.stopAtDelimiter && iPrev != -1 && isDelimiter((*p.text)[iPrev]) {
		// if the text ends with a delimiter, like ["hello],
		// so the missing end quote should be inserted before this delimiter
		// retry parsing the string, stopping at the first next delimiter
		return p.retry(true, -1)
	}
	// repair missing quote
	return p.finishWithMissingQuote()
}

// endsStringAt 判断结尾引号后面 text[j] 这个字符是不是说明字符串到此结束：
// 文本结尾、分隔符、下一个值的开头（引号或数字）。
func (p *stringParser) endsStringAt(j int) bool {
	if j >= len(*p.text) {
		return true
	}
	c := (*p.text)[j]
	return isDelimiter(c) || isQuote(c) || isDigit(c) || isFullWidthDelimiterAt(p.text, j)
}

// onQuote 遇到一个引号：是字符串的结尾，还是字符串正文里没转义的引号。
func (p *stringParser) onQuote() stringStep {
	text, i, output := p.text, p.i, p.output

	iQuote := *i
	oQuote := p.str.Len()
	p.str.WriteRune('"')
	*i++
	output.WriteString(p.str.String())

	iAfterWhitespace := *i
	var tempWhitespace strings.Builder
	parseWhitespaceAndSkipComments(text, &iAfterWhitespace, &tempWhitespace, false)

	if p.stopAtDelimiter || p.endsStringAt(iAfterWhitespace) {
		// The quote is followed by the end of the text, a delimiter,
		// or a next value. So the quote is indeed the end of the string.
		*i = iAfterWhitespace
		output.WriteString(tempWhitespace.String())
		parseConcatenatedString(text, i, output)
		return stringFinished
	}

	if step, retried := p.retryByCharBeforeQuote(iQuote); retried {
		return step
	}

	// revert to right after the quote but before any whitespace, and continue parsing the string
	resetOutput(output, output.String()[:p.oBefore])
	*i = iQuote + 1

	// repair unescaped quote；单词里的撇号（it's）是正文，原样留着
	inner := "\\\""
	if isWordApostrophe(text, iQuote) {
		inner = string((*text)[iQuote])
	}
	revertedStr := p.str.String()[:oQuote] + inner
	p.str.Reset()
	p.str.WriteString(revertedStr)
	return stringContinue
}

// retryByCharBeforeQuote 看引号前一个字符：是逗号或分隔符，说明前面的结尾引号漏了，换一种方式重读。
func (p *stringParser) retryByCharBeforeQuote(iQuote int) (stringStep, bool) {
	iPrevChar := prevNonWhitespaceIndex(*p.text, iQuote-1)
	if iPrevChar == -1 {
		return stringContinue, false
	}
	prevChar := (*p.text)[iPrevChar]
	switch {
	case prevChar == ',':
		return p.retry(false, iPrevChar), true
	case isDelimiter(prevChar):
		return p.retry(true, -1), true
	}
	return stringContinue, false
}

// onDelimiterWithoutEndQuote 缺结尾引号时碰到第一个分隔符，字符串到此结束。
func (p *stringParser) onDelimiterWithoutEndQuote() stringStep {
	text, i := p.text, p.i
	if *i > 0 && (*text)[*i-1] == ':' &&
		regexURLStart.MatchString(string((*text)[p.iBefore+1:min(*i+2, len(*text))])) {
		for *i < len(*text) && isURLChar((*text)[*i]) {
			p.str.WriteRune((*text)[*i])
			*i++
		}
	}

	// repair missing quote
	p.output.WriteString(insertBeforeLastWhitespace(p.str.String(), "\""))
	parseConcatenatedString(text, i, p.output)
	return stringFinished
}

// onPlainChar 读普通字符：没转义的双引号、控制字符、其他字符。
func (p *stringParser) onPlainChar() stringStep {
	text, i := p.text, p.i
	char := (*text)[*i]
	switch {
	case char == '"' && (*text)[*i-1] != '\\':
		// repair unescaped double quote
		p.str.WriteString("\\\"")
	case isControlCharacter(char):
		// unescaped control character
		if replacement, ok := controlCharacters[char]; ok {
			p.str.WriteString(replacement)
		}
	default:
		// Check character validity - matches TypeScript throwInvalidCharacter()
		if !isValidStringCharacter(char) {
			// Format control characters as Unicode escape sequences to match TypeScript
			message := fmt.Sprintf("invalid character \"\\\\u%04x\"", char)
			return stringStep{done: true, err: newInvalidCharacterError(message, *i)}
		}
		p.str.WriteRune(char)
	}
	*i++
	return stringContinue
}

// onBackslash 读反斜杠开头的转义。
func (p *stringParser) onBackslash() stringStep {
	text, i := p.text, p.i
	if *i+1 >= len(*text) {
		// repair: incomplete escape sequence at end of string
		// just remove the backslash and end the string
		step := p.finishWithMissingQuote()
		*i++
		return step
	}

	char := (*text)[*i+1]
	if _, ok := escapeCharacters[char]; ok {
		p.writeEscapedPair()
		return stringContinue
	}
	if char == 'u' {
		return p.onUnicodeEscape()
	}
	if char == codeNewline {
		p.str.WriteString(`\n`)
		*i += 2
		return stringContinue
	}
	return p.onInvalidEscape(char)
}

// writeLiteralBackslash 文件路径里的反斜杠按字面处理，写成 \\ 。
func (p *stringParser) writeLiteralBackslash() {
	p.str.WriteString("\\\\")
	*p.i++
}

// writeEscapedPair 写出合法的 JSON 转义（\n \" 之类）；文件路径里则把反斜杠当字面字符。
func (p *stringParser) writeEscapedPair() {
	if p.filePath {
		p.writeLiteralBackslash()
		return
	}
	p.str.WriteRune((*p.text)[*p.i])
	p.str.WriteRune((*p.text)[*p.i+1])
	*p.i += 2
}

// onInvalidEscape 读不认识的转义（\x 之类），去掉反斜杠留下后面的字符。
func (p *stringParser) onInvalidEscape(char rune) stringStep {
	text, i := p.text, p.i
	if p.stopAtIndex != -1 && *i == p.stopAtIndex-1 && isDelimiter((*text)[p.stopAtIndex]) {
		// stop before the delimiter that triggered reparsing to avoid infinite recursion
		p.output.WriteString(insertBeforeLastWhitespace(p.str.String(), "\""))
		*i = p.stopAtIndex
		return stringFinished
	}

	if p.filePath {
		p.writeLiteralBackslash()
		return stringContinue
	}
	// Default behavior: remove invalid escape character
	p.str.WriteRune(char)
	*i += 2
	return stringContinue
}

// onUnicodeEscape 读 \u 开头的转义。
func (p *stringParser) onUnicodeEscape() stringStep {
	text, i := p.text, p.i
	const hexDigits = 4
	j := 2
	hexCount := 0
	// Count valid hex characters
	for j < unicodeEscapeLen && *i+j < len(*text) && isHex((*text)[*i+j]) {
		j++
		hexCount++
	}

	switch {
	case hexCount == hexDigits:
		if p.filePath {
			p.writeLiteralBackslash()
		} else {
			// Valid Unicode escape sequence - keep as is
			p.str.WriteString(string((*text)[*i : *i+unicodeEscapeLen]))
			*i += unicodeEscapeLen
		}
	case *i+j >= len(*text):
		// repair invalid or truncated unicode char at the end of the text
		// by removing the unicode char and ending the string here
		*i = len(*text)
	default:
		return p.onInvalidUnicode(hexCount)
	}
	return stringContinue
}

// onInvalidUnicode \u 后面不是 4 位十六进制：文件路径里的 \users 之类当字面反斜杠，其他报错。
func (p *stringParser) onInvalidUnicode(hexCount int) stringStep {
	text, i := p.text, p.i
	if p.filePath && hexCount == 0 && *i+2 < len(*text) {
		nextChar := (*text)[*i+2]
		if (nextChar >= 'a' && nextChar <= 'z') || (nextChar >= 'A' && nextChar <= 'Z') {
			// Looks like \users, \util - treat as literal backslash
			p.writeLiteralBackslash()
			return stringContinue
		}
		return stringStep{done: true, err: invalidUnicodeSequenceError(*text, *i, hexCount, false)}
	}
	return stringStep{done: true, err: invalidUnicodeSequenceError(*text, *i, hexCount, true)}
}

func invalidUnicodeSequenceError(text []rune, start, hexCount int, quoteIncomplete bool) *Error {
	end := 2
	for end < unicodeEscapeLen && start+end < len(text) {
		nextChar := text[start+end]
		if nextChar == '"' || nextChar == '\'' || isWhitespace(nextChar) {
			break
		}
		end++
	}

	chars := string(text[start : start+end])
	if quoteIncomplete && hexCount < 4 && end == 2+hexCount {
		return newInvalidUnicodeError(fmt.Sprintf("invalid unicode character %q\"", chars), start)
	}
	return newInvalidUnicodeError(fmt.Sprintf("invalid unicode character %q", chars), start)
}

// parseConcatenatedString parses concatenated strings (e.g., "hello" + "world").
func parseConcatenatedString(text *[]rune, i *int, output *strings.Builder) {
	processed := false

	iBeforeWhitespace := *i
	oBeforeWhitespace := output.Len()
	parseWhitespaceAndSkipComments(text, i, output, true)

	for *i < len(*text) && (*text)[*i] == '+' {
		processed = true
		*i++
		parseWhitespaceAndSkipComments(text, i, output, true)

		// repair: remove the end quote of the first string
		resetOutput(output, stripLastOccurrence(output.String(), "\"", true))
		start := output.Len()

		stringProcessed, err := parseString(text, i, output, false, -1)
		if err != nil {
			// For concatenated strings, errors are not critical - just stop processing
			stringProcessed = false
		}
		if stringProcessed {
			// repair: remove the start quote of the second string
			outputStr := output.String()
			if len(outputStr) > start {
				resetOutput(output, outputStr[:start]+outputStr[start+1:])
			}
		} else {
			// repair: remove the + because it is not followed by a string
			resetOutput(output, insertBeforeLastWhitespace(output.String(), "\""))
		}
	}

	if !processed {
		// revert parsing whitespace
		*i = iBeforeWhitespace
		resetOutput(output, output.String()[:oBeforeWhitespace])
	}
}
