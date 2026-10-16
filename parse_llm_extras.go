package llmjsonrepair

import (
	"strings"
	"unicode"
)

// 本文件是本仓库加的几种修法：多余的冒号、键值之间夹的词、开头的 "- " 列表符号。

// maxFillerWords 键值之间夹的词最多认几个。
const maxFillerWords = 3

// fillerKeywords 这几个词有自己的意思，不能当成夹在中间的词丢掉。
var fillerKeywords = map[string]bool{
	"true": true, "false": true, "null": true, "none": true,
	"undefined": true, "nan": true, "infinity": true,
}

// skipExtraColons 丢掉冒号后面多余的冒号（{"a"::1}、{"a": : "x"}），冒号之间可以夹空白。
func skipExtraColons(text *[]rune, i *int, output *strings.Builder) {
	for {
		j := *i
		var whitespace strings.Builder
		parseWhitespaceAndSkipComments(text, &j, &whitespace, true)
		if !runeIs(text, j, codeColon) {
			return
		}
		*i = j + 1
	}
}

// skipWordsBeforeValue 丢掉冒号和带引号的值之间夹的英文单词：{"a": value is "x"} 里的 value is。
// 带引号的值后面必须紧跟 , } ] 或文本结尾，免得把「少了逗号的下一个键」当成夹的词。
func skipWordsBeforeValue(text *[]rune, i *int) {
	skipFillerWords(text, i, func(j int) bool { return isCleanQuotedValueAt(text, j) })
}

// skipFillerWords 从 i 开始最多跳过 maxFillerWords 个英文单词，
// 跳完之后 stop(j) 为真才真的跳；单词是 true、null 这类有自己意思的词就不跳。
func skipFillerWords(text *[]rune, i *int, stop func(j int) bool) {
	j := *i
	for words := 0; words < maxFillerWords; words++ {
		end := scanLetters(text, j)
		if end == j || fillerKeywords[strings.ToLower(string((*text)[j:end]))] {
			return
		}
		j = skipBlanks(text, end)
		if j < len(*text) && stop(j) {
			*i = j
			return
		}
	}
}

// isCleanQuotedValueAt 判断 text[j] 是不是一个带引号的值的开头，且结尾引号后面紧跟 , } ] 或文本结尾。
func isCleanQuotedValueAt(text *[]rune, j int) bool {
	if !isQuote((*text)[j]) {
		return false
	}
	for k := j + 1; k < len(*text); k++ {
		if (*text)[k] != (*text)[j] && !(isDoubleQuoteLike((*text)[j]) && isDoubleQuoteLike((*text)[k])) {
			continue
		}
		k = skipBlanks(text, k+1)
		return k >= len(*text) || isValueEnd((*text)[k])
	}
	return false
}

// isValueEnd 判断这个字符是不是一个值后面该有的收尾：逗号或右括号。
func isValueEnd(c rune) bool {
	c = structuralRune(c)
	return c == codeComma || c == codeClosingBrace || c == codeClosingBracket
}

// scanLetters 返回从 j 开始连续的英文字母到哪里结束。
func scanLetters(text *[]rune, j int) int {
	for j < len(*text) && ((*text)[j] < unicode.MaxASCII) && unicode.IsLetter((*text)[j]) {
		j++
	}
	return j
}

// skipBlanks 跳过空格和制表符（不跨行），返回新位置。
func skipBlanks(text *[]rune, j int) int {
	for j < len(*text) && ((*text)[j] == codeSpace || (*text)[j] == codeTab) {
		j++
	}
	return j
}

// skipListMarker 跳过行首的 "- " 列表符号（- {"a":1}），只在后面接着 { 或 [ 时才算，
// 免得把 "- 1" 这种负数当成列表符号。
func skipListMarker(text *[]rune, i *int) {
	if !runeIs(text, *i, codeMinus) {
		return
	}
	j := skipBlanks(text, *i+1)
	if j == *i+1 || j >= len(*text) || !isOpeningBracketLike((*text)[j]) {
		return
	}
	*i = j
}

// isWordApostrophe 判断 text[j] 是不是夹在两个字母中间的撇号（it's），这种撇号是单词的一部分。
func isWordApostrophe(text *[]rune, j int) bool {
	c := (*text)[j]
	if c != codeQuote && c != codeQuoteRight {
		return false
	}
	return j > 0 && j+1 < len(*text) && unicode.IsLetter((*text)[j-1]) && unicode.IsLetter((*text)[j+1])
}
