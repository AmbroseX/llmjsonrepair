package llmjsonrepair

import "strings"

// objectMemberStatus 是解析完对象里一个键值对之后接下来该怎么走。
type objectMemberStatus int

const (
	memberNext  objectMemberStatus = iota // 正常，接着读下一个键值对
	memberStop                            // 对象到头了，退出循环去补右括号
	memberAbort                           // 这个对象解析失败，parseObject 返回 false
)

// parseObject parses a JSON object.
// Returns (success, error) where error is non-nil for non-repairable issues.
func parseObject(text *[]rune, i *int, output *strings.Builder, depth int) (bool, error) {
	if !runeIs(text, *i, codeOpeningBrace) {
		return false, nil
	}
	childDepth, err := nextNestingDepth(depth, *i)
	if err != nil {
		return false, err
	}

	output.WriteByte(codeOpeningBrace)
	*i++
	parseWhitespaceAndSkipComments(text, i, output, true)
	skipLeadingCommas(text, i, output)

	initial := true
	for *i < len(*text) && !runeIs(text, *i, codeClosingBrace) {
		firstKey := initial
		if !initial {
			parseObjectComma(text, i, output)
		}
		initial = false

		skipEllipsis(text, i, output)
		if firstKey {
			skipExtraOpeningBraces(text, i, output)
		}

		status, err := parseObjectMember(text, i, output, childDepth)
		if err != nil {
			return false, err
		}
		if status == memberAbort {
			return false, nil
		}
		if status == memberStop {
			break
		}
	}

	closeContainer(text, i, output, codeClosingBrace)
	return true, nil
}

// skipLeadingCommas 跳过容器开头的逗号，比如 {, message: "hi"} 和 [,,1]。
func skipLeadingCommas(text *[]rune, i *int, output *strings.Builder) {
	for skipCharacter(text, i, codeComma) {
		parseWhitespaceAndSkipComments(text, i, output, true)
	}
}

// closeContainer 写出容器的右括号 closing；输入里没有右括号就补一个。
func closeContainer(text *[]rune, i *int, output *strings.Builder, closing rune) {
	if runeIs(text, *i, closing) {
		output.WriteRune(closing)
		*i++
		return
	}
	// repair missing end bracket
	resetOutput(output, insertBeforeLastWhitespace(output.String(), string(closing)))
}

// skipExtraOpeningBraces 本仓库加的修复：对象一开头又多了一个 {（大模型写过 [{"a":1},{{"b":2}]），跳过它；
// 跟它配对的那个 } 会按多余的右括号处理。只管第一个键：逗号后面的 {
// 原库当成前一个对象少了 }（[{"i":1,{"i":2}]），不改。
func skipExtraOpeningBraces(text *[]rune, i *int, output *strings.Builder) {
	for runeIs(text, *i, codeOpeningBrace) {
		*i++
		parseWhitespaceAndSkipComments(text, i, output, true)
	}
}

// parseObjectComma 读两个键值对之间的逗号；没有逗号就补一个。多余的逗号丢掉。
func parseObjectComma(text *[]rune, i *int, output *strings.Builder) {
	iBefore := *i
	oBefore := output.Len()
	if !parseCharacter(text, i, output, codeComma) {
		// repair missing comma - restore output to oBefore and insert comma
		*i = iBefore
		resetOutput(output, insertBeforeLastWhitespace(output.String()[:oBefore], ","))
		return
	}
	moveLastCommaBeforeWhitespace(output)
	skipExtraCommas(text, i, output)
}

// moveLastCommaBeforeWhitespace 把刚写进去的逗号挪到它前面那段空白（比如换行加缩进）之前，
// 并去掉换行后面残留的缩进，后面会按原文再写一遍空白，不去掉缩进会翻倍。
func moveLastCommaBeforeWhitespace(output *strings.Builder) {
	temp, ok := strings.CutSuffix(output.String(), ",")
	if !ok {
		return
	}
	temp = insertBeforeLastWhitespace(temp, ",")
	if idx := strings.LastIndex(temp, "\n"); idx != -1 {
		// 换行后面全是空格和制表符才去掉
		j := idx + 1
		for j < len(temp) && (temp[j] == ' ' || temp[j] == '\t') {
			j++
		}
		if j == len(temp) {
			temp = temp[:idx+1]
		}
	}
	resetOutput(output, temp)
}

// isContainerEdge 判断这个位置是不是文本结尾或括号，对象里读不到键时用来区分「后面没东西了」和「后面是乱码」。
func isContainerEdge(text *[]rune, i int) bool {
	if i >= len(*text) {
		return true
	}
	c := structuralRune((*text)[i])
	return c == codeClosingBrace || c == codeOpeningBrace ||
		c == codeClosingBracket || c == codeOpeningBracket || c == 0
}

// parseObjectKey 读一个键，先按带引号的字符串读，不行再按不带引号的读。
func parseObjectKey(text *[]rune, i *int, output *strings.Builder, childDepth int) (bool, error) {
	processed, err := parseString(text, i, output, false, -1)
	if err != nil || processed {
		return processed, err
	}
	return parseUnquotedStringWithMode(text, i, output, true, childDepth)
}

// parseObjectMember 读一个「键 冒号 值」。
func parseObjectMember(text *[]rune, i *int, output *strings.Builder, childDepth int) (objectMemberStatus, error) {
	processedKey, err := parseObjectKey(text, i, output, childDepth)
	if err != nil {
		return memberAbort, err
	}
	if !processedKey {
		if !isContainerEdge(text, *i) {
			return memberAbort, newObjectKeyExpectedError(*i)
		}
		// repair trailing comma
		resetOutput(output, stripLastOccurrence(output.String(), ",", false))
		return memberStop, nil
	}

	parseWhitespaceAndSkipComments(text, i, output, true)
	processedColon := parseCharacter(text, i, output, codeColon)
	truncatedText := *i >= len(*text)
	if !processedColon {
		if !truncatedText && !isStartOfValue((*text)[*i]) {
			return memberAbort, newColonExpectedError(*i)
		}
		// repair missing colon
		resetOutput(output, insertBeforeLastWhitespace(output.String(), ":"))
	}

	processedValue, err := parseValue(text, i, output, childDepth)
	if err != nil {
		return memberAbort, err
	}
	if processedValue {
		return memberNext, nil
	}
	if !processedColon && !truncatedText {
		return memberAbort, nil
	}
	// repair missing object value
	output.WriteString("null")
	return memberNext, nil
}

// parseArray parses an array from the input text.
// Returns (success, error) where error is non-nil for non-repairable issues.
func parseArray(text *[]rune, i *int, output *strings.Builder, depth int) (bool, error) {
	if !runeIs(text, *i, codeOpeningBracket) {
		return false, nil
	}
	childDepth, err := nextNestingDepth(depth, *i)
	if err != nil {
		return false, err
	}

	output.WriteByte(codeOpeningBracket)
	*i++
	parseWhitespaceAndSkipComments(text, i, output, true)
	skipLeadingCommas(text, i, output)

	initial := true
	for *i < len(*text) && !runeIs(text, *i, codeClosingBracket) {
		if !initial {
			parseArrayComma(text, i, output)
		}
		initial = false

		skipEllipsis(text, i, output)

		processedValue, err := parseValue(text, i, output, childDepth)
		if err != nil {
			return false, err
		}
		if !processedValue {
			// repair trailing comma
			resetOutput(output, stripLastOccurrence(output.String(), ",", false))
			break
		}
		trimCommaInsideLastString(output)
	}

	closeContainer(text, i, output, codeClosingBracket)
	return true, nil
}

// parseArrayComma 读两个元素之间的逗号；没有逗号就补一个。多余的逗号丢掉。
func parseArrayComma(text *[]rune, i *int, output *strings.Builder) {
	iBefore := *i
	oBefore := output.Len()
	parseWhitespaceAndSkipComments(text, i, output, true)

	if !parseCharacter(text, i, output, codeComma) {
		// repair missing comma
		*i = iBefore
		resetOutput(output, insertBeforeLastWhitespace(output.String()[:oBefore], ","))
		return
	}
	skipExtraCommas(text, i, output)
}

// trimCommaInsideLastString 去掉刚读完的字符串里紧贴结尾引号的那个逗号。
// 输入像 "hello,world,"2 时，逗号其实是两个元素之间的，却落进了第一个字符串。
// 只有逗号本身的字符串（","）是合法值，不能动，所以要求引号里不止一个字符。
func trimCommaInsideLastString(output *strings.Builder) {
	withoutSuffix, ok := strings.CutSuffix(output.String(), ",\"")
	if !ok {
		return
	}
	lastQuote := strings.LastIndex(withoutSuffix, "\"")
	if lastQuote != -1 && len(withoutSuffix)-lastQuote > 2 {
		resetOutput(output, withoutSuffix+"\"")
	}
}

// parseNewlineDelimitedJSON parses Newline Delimited JSON (NDJSON) from the input text.
func parseNewlineDelimitedJSON(text *[]rune, i *int, output *strings.Builder) error {
	for {
		processedValue, err := parseValue(text, i, output, 0)
		if err != nil {
			return err
		}
		if !processedValue {
			resetOutput(output, stripLastOccurrence(output.String(), ",", false))
			break
		}

		if !parseCharacter(text, i, output, codeComma) {
			resetOutput(output, insertBeforeLastWhitespace(output.String(), ","))
		}
	}

	resetOutput(output, "[\n"+output.String()+"\n]")
	return nil
}
