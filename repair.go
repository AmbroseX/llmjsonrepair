// Package llmjsonrepair repairs malformed JSON strings returned by LLMs,
// JavaScript snippets, and other JSON-like sources.
//
// It is a fork of github.com/kaptinlin/jsonrepair v0.4.9 (MIT) with extra
// repairs for mistakes LLMs often make: full-width brackets, colons and commas,
// several JSON values written back to back, consecutive commas, and a doubled
// opening brace. It needs only the standard library and works with Go 1.25.
//
// Public APIs return errors instead of panicking, and the package does not expose
// Must* helpers. The only panic-capable paths are package initialization of
// internal regexp.MustCompile literals, which can fail only if the package source
// contains an invalid regular expression.
package llmjsonrepair

import (
	"fmt"
	"strings"
)

// maxNestingDepth bounds recursive structures before they can exhaust the stack.
const maxNestingDepth = 10000

// Repair repairs malformed JSON-like input into valid JSON.
//
// It returns a structured [*Error] when the input is empty or cannot be repaired.
func Repair(text string) (string, error) {
	if len(text) == 0 {
		return "", newUnexpectedEndError(0)
	}

	runes := []rune(text)
	i := 0
	var output strings.Builder

	parseMarkdownCodeBlock(&runes, &i, []string{"```", "[```", "{```"}, &output)

	parseWhitespaceAndSkipComments(&runes, &i, &output, true)
	skipListMarker(&runes, &i)

	success, err := parseValue(&runes, &i, &output, 0)
	if err != nil {
		return "", err
	}
	if !success {
		return "", newUnexpectedEndError(len(runes))
	}

	parseMarkdownCodeBlock(&runes, &i, []string{"```", "```]", "```}"}, &output)

	processedComma := parseCharacter(&runes, &i, &output, codeComma)
	if processedComma {
		parseWhitespaceAndSkipComments(&runes, &i, &output, true)
	}

	if err := parseFollowingValues(&runes, &i, &output, processedComma); err != nil {
		return "", err
	}

	for i < len(runes) && (runeIs(&runes, i, codeClosingBrace) || runeIs(&runes, i, codeClosingBracket)) {
		i++
		parseWhitespaceAndSkipComments(&runes, &i, &output, true)
	}

	parseWhitespaceAndSkipComments(&runes, &i, &output, true)

	if i >= len(runes) {
		return output.String(), nil
	}

	message := fmt.Sprintf("unexpected character %q", string(runes[i]))
	return "", newUnexpectedCharacterError(message, i)
}

// parseFollowingValues 处理第一个值后面还跟着内容的情况：
// 换行或逗号隔开的多个值（NDJSON），和不隔开直接连着写的多个对象、数组（{"a":1}{"b":2}、[1][2]），
// 都包成一个数组。processedComma 表示第一个值后面已经读掉了一个逗号。
func parseFollowingValues(text *[]rune, i *int, output *strings.Builder, processedComma bool) error {
	skipListMarker(text, i)
	more := *i < len(*text) && isStartOfValue((*text)[*i])
	switch {
	case more && endsWithCommaOrNewline(output.String()):
		if !processedComma {
			resetOutput(output, insertBeforeLastWhitespace(output.String(), ","))
		}
		return parseNewlineDelimitedJSON(text, i, output)
	case more && !processedComma && endsWithContainer(output.String()) && startsAdjacentValue(text, *i):
		return parseAdjacentValues(text, i, output)
	case processedComma:
		resetOutput(output, stripLastOccurrence(output.String(), ",", false))
	}
	return nil
}

// endsWithContainer 判断已经写出的内容是不是以 } 或 ] 结尾（不算结尾的空白），也就是第一个值是对象或数组。
func endsWithContainer(output string) bool {
	trimmed := strings.TrimRight(output, " \t\r\n")
	return strings.HasSuffix(trimmed, "}") || strings.HasSuffix(trimmed, "]")
}

// startsAdjacentValue 判断 text[i] 是不是一个紧挨着写的对象或数组的开头。
// 空的 {} 和 [] 不算：{"a":2}{} 这种尾巴当成多余内容报错（原库自带用例要求这样）。
func startsAdjacentValue(text *[]rune, i int) bool {
	if i >= len(*text) || !isOpeningBracketLike((*text)[i]) {
		return false
	}
	closing := codeClosingBrace
	if structuralRune((*text)[i]) == codeOpeningBracket {
		closing = codeClosingBracket
	}
	j := i + 1
	for j < len(*text) && (isWhitespace((*text)[j]) || isSpecialWhitespace((*text)[j])) {
		j++
	}
	return !runeIs(text, j, rune(closing))
}

// parseAdjacentValues 把紧挨着写的多个对象或数组包成一个数组。
// 只接着读以 { 或 [ 开头的非空值，后面跟的解释文字之类留给调用方报错，不会被当成数组元素。
func parseAdjacentValues(text *[]rune, i *int, output *strings.Builder) error {
	for startsAdjacentValue(text, *i) {
		resetOutput(output, insertBeforeLastWhitespace(output.String(), ","))
		if _, err := parseValue(text, i, output, 0); err != nil {
			return err
		}
	}
	resetOutput(output, "[\n"+output.String()+"\n]")
	return nil
}

// parseValue determines the type of the next value and parses it.
// Returns (success, error) where error is non-nil for non-repairable issues.
// depth is the number of currently open recursive structures.
func parseValue(text *[]rune, i *int, output *strings.Builder, depth int) (bool, error) {
	parseWhitespaceAndSkipComments(text, i, output, true)

	processedObj, err := parseObject(text, i, output, depth)
	if err != nil {
		return false, err
	}
	if processedObj {
		parseWhitespaceAndSkipComments(text, i, output, true)
		return true, nil
	}

	processed, err := parseArray(text, i, output, depth)
	if err != nil {
		return false, err
	}
	if processed {
		parseWhitespaceAndSkipComments(text, i, output, true)
		return true, nil
	}

	stringProcessed, err := parseString(text, i, output, false, -1)
	if err != nil {
		return false, err
	}
	processed = stringProcessed ||
		parseNumber(text, i, output) ||
		parseKeywords(text, i, output)
	if !processed {
		processed, err = parseUnquotedStringWithMode(text, i, output, false, depth)
		if err != nil {
			return false, err
		}
	}
	if !processed {
		processed = parseRegex(text, i, output)
	}

	parseWhitespaceAndSkipComments(text, i, output, true)
	return processed, nil
}

func nextNestingDepth(depth, position int) (int, error) {
	if depth >= maxNestingDepth {
		return 0, newMaxDepthExceededError(position)
	}
	return depth + 1, nil
}
