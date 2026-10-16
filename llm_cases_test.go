package llmjsonrepair

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// 大模型真实出过的错。修完要能被 encoding/json 解开，且内容对。

type llmFinding struct {
	Text      string   `json:"text"`
	ObjectIDs []string `json:"object_ids"`
}

type llmReport struct {
	Title    string       `json:"title"`
	Summary  string       `json:"summary"`
	Findings []llmFinding `json:"findings"`
}

func repairToReport(t *testing.T, in string) llmReport {
	t.Helper()
	out, err := Repair(in)
	require.NoError(t, err, "输入: %s", in)
	var r llmReport
	require.NoError(t, json.Unmarshal([]byte(out), &r), "修复输出: %s", out)
	return r
}

// 整段 JSON 写完后结尾多一个 }
func TestLLMExtraClosingBrace(t *testing.T) {
	r := repairToReport(t, `{"title":"似鹈鹕龙类群","summary":"社区围绕似鹈鹕龙展开。","findings":[{"text":"多锯似鹈鹕龙是模式种。","object_ids":["n1","n2"]}]}}`)
	require.Equal(t, "似鹈鹕龙类群", r.Title)
	require.Equal(t, "社区围绕似鹈鹕龙展开。", r.Summary)
	require.Equal(t, []llmFinding{{Text: "多锯似鹈鹕龙是模式种。", ObjectIDs: []string{"n1", "n2"}}}, r.Findings)
}

// 数组后面直接跟 }，少了 ]
func TestLLMMissingArrayClose(t *testing.T) {
	r := repairToReport(t, `{"title":"t","summary":"s","findings":[{"text":"a","object_ids":["x"]}}`)
	require.Equal(t, "t", r.Title)
	require.Equal(t, []llmFinding{{Text: "a", ObjectIDs: []string{"x"}}}, r.Findings)
}

// 对象里多了一个 {
func TestLLMDoubleOpenBrace(t *testing.T) {
	r := repairToReport(t, `{"title":"t","summary":"s","findings":[{"text":"a"},{{"text":"b"}]}`)
	require.Equal(t, []llmFinding{{Text: "a"}, {Text: "b"}}, r.Findings)
}

// JSON 前后夹着解释文字的，这个库不管（报错，或者把文字和 JSON 包成一个数组），
// 由调用方截出第一个 { 或 [ 到最后一个 } 或 ] 那一段再修。

// 中文弯引号包住键和值
func TestLLMCurlyQuotes(t *testing.T) {
	r := repairToReport(t, `{“title”:“弯引号”,“summary”:“s”,“findings”:[]}`)
	require.Equal(t, "弯引号", r.Title)
	require.Equal(t, "s", r.Summary)
}

// 结尾逗号
func TestLLMTrailingComma(t *testing.T) {
	r := repairToReport(t, `{"title":"t","summary":"s","findings":[{"text":"a","object_ids":["x","y",],},],}`)
	require.Equal(t, []llmFinding{{Text: "a", ObjectIDs: []string{"x", "y"}}}, r.Findings)
}

// 被截断：在字符串中间断掉
func TestLLMTruncatedInString(t *testing.T) {
	r := repairToReport(t, `{"title":"t","summary":"s","findings":[{"text":"第一条","object_ids":["x"]},{"text":"第二条被截`)
	require.Equal(t, "t", r.Title)
	require.GreaterOrEqual(t, len(r.Findings), 1)
	require.Equal(t, llmFinding{Text: "第一条", ObjectIDs: []string{"x"}}, r.Findings[0])
}

// 被截断：在键后面断掉
func TestLLMTruncatedAfterKey(t *testing.T) {
	r := repairToReport(t, `{"title":"t","summary":"s","findings":[{"text":"a","object_ids":`)
	require.Equal(t, "t", r.Title)
	require.Equal(t, "s", r.Summary)
}

// repairToAny 修复后用标准库解开成 any，用来比对整体结构和值。
func repairToAny(t *testing.T, in string) any {
	t.Helper()
	out, err := Repair(in)
	require.NoError(t, err, "输入: %s", in)
	var v any
	require.NoError(t, json.Unmarshal([]byte(out), &v), "修复输出: %s", out)
	return v
}

type obj = map[string]any

// 全角括号 ｛ ｝ ［ ］ 在结构位置当成 { } [ ]
func TestLLMFullWidthBrackets(t *testing.T) {
	v := repairToAny(t, `｛"a":［1,2］,"b":｛"c":"d"｝｝`)
	require.Equal(t, obj{"a": []any{1.0, 2.0}, "b": obj{"c": "d"}}, v)
}

// 全角括号包住的数组，元素是对象，结尾右括号后面还有全角右括号
func TestLLMFullWidthBracketsNested(t *testing.T) {
	v := repairToAny(t, `［｛"text":"a"｝,｛"text":"b"｝］`)
	require.Equal(t, []any{obj{"text": "a"}, obj{"text": "b"}}, v)
}

// 全角冒号、全角逗号在键值之间、元素之间当成 : ,
func TestLLMFullWidthColonComma(t *testing.T) {
	v := repairToAny(t, `{"a"："b"，"c"：1，"d"：[1，2]，"e"：true，"f"：null}`)
	require.Equal(t, obj{"a": "b", "c": 1.0, "d": []any{1.0, 2.0}, "e": true, "f": nil}, v)
}

// 全角括号冒号逗号混在一起写
func TestLLMFullWidthAll(t *testing.T) {
	v := repairToAny(t, `｛"title"："t"，"findings"：［｛"text"："a"，"object_ids"：［"x"，"y"］｝］｝`)
	require.Equal(t, obj{"title": "t", "findings": []any{obj{"text": "a", "object_ids": []any{"x", "y"}}}}, v)
}

// 字符串值里面的全角标点一个都不能动
func TestLLMFullWidthPunctuationInStringUntouched(t *testing.T) {
	v := repairToAny(t, `{"a"："说明：甲，乙"，"b"：["丙，丁"，"戊：己｛庚｝［辛］"]}`)
	require.Equal(t, obj{"a": "说明：甲，乙", "b": []any{"丙，丁", "戊：己｛庚｝［辛］"}}, v)

	// 半角结构里的全角标点同样不动
	v = repairToAny(t, `{"a":"说明：甲，乙","b":["丙，丁","戊：己"]}`)
	require.Equal(t, obj{"a": "说明：甲，乙", "b": []any{"丙，丁", "戊：己"}}, v)
}

// 字符串正文里有没转义的引号，引号后面紧跟全角逗号，不能把字符串在这里截断
func TestLLMFullWidthCommaAfterInnerQuote(t *testing.T) {
	v := repairToAny(t, `{"t":"他称之为"神作"，评价很高","u":"x"}`)
	require.Equal(t, obj{"t": `他称之为"神作"，评价很高`, "u": "x"}, v)
}

// 没加引号的键碰到全角冒号结束，没加引号的值碰到全角逗号结束，跟半角的 : , 一样
func TestLLMFullWidthUnquoted(t *testing.T) {
	v := repairToAny(t, `{名称：甲，年龄：3}`)
	require.Equal(t, obj{"名称": "甲", "年龄": 3.0}, v)

	v = repairToAny(t, `[甲，乙]`)
	require.Equal(t, []any{"甲", "乙"}, v)

	// 跟半角写法的结果一致
	require.Equal(t, repairToAny(t, `{名称:甲,年龄:3}`), repairToAny(t, `{名称：甲，年龄：3}`))
}

// 多个 JSON 紧挨着写，包成一个数组
func TestLLMConcatenatedValues(t *testing.T) {
	require.Equal(t, []any{obj{"a": 1.0}, obj{"b": 2.0}}, repairToAny(t, `{"a":1}{"b":2}`))
	require.Equal(t, []any{obj{"a": 1.0}, obj{"b": 2.0}}, repairToAny(t, "{\"a\":1}\n{\"b\":2}"))
	require.Equal(t, []any{obj{"a": 1.0}, obj{"b": 2.0}}, repairToAny(t, `{"a":1} {"b":2}`))
	require.Equal(t, []any{[]any{1.0}, []any{2.0}}, repairToAny(t, `[1][2]`))
	require.Equal(t, []any{obj{"a": 1.0}, []any{2.0}, obj{"c": 3.0}}, repairToAny(t, `{"a":1}[2]{"c":3}`))
	require.Equal(t, []any{obj{"a": 1.0}, obj{"b": 2.0}}, repairToAny(t, `｛"a":1｝｛"b":2｝`))
}

// 紧挨着的多个值后面跟解释文字，文字不会被当成数组元素，照旧报错
func TestLLMConcatenatedValuesTrailingText(t *testing.T) {
	_, err := Repair(`{"a":1}{"b":2} 以上是结果`)
	require.Error(t, err)
}

// 连续的逗号，多余的丢掉
func TestLLMConsecutiveCommas(t *testing.T) {
	require.Equal(t, []any{1.0, 2.0}, repairToAny(t, `[1,,2]`))
	require.Equal(t, []any{1.0, 2.0}, repairToAny(t, `[1, , 2]`))
	require.Equal(t, []any{1.0}, repairToAny(t, `[1,,,]`))
	require.Equal(t, []any{1.0}, repairToAny(t, `[,,1]`))
	require.Equal(t, []any{1.0, 2.0, 3.0}, repairToAny(t, `[1,,,2,,3]`))
	require.Equal(t, obj{"a": 1.0, "b": 2.0}, repairToAny(t, `{"a":1,,"b":2}`))
	require.Equal(t, obj{"a": 1.0, "b": 2.0}, repairToAny(t, "{\"a\":1,\n  ,\n  \"b\":2}"))
	require.Equal(t, obj{"a": 1.0}, repairToAny(t, `{"a":1,,,}`))
	require.Equal(t, obj{"a": 1.0}, repairToAny(t, `{,,"a":1}`))
	require.Equal(t, []any{1.0, 2.0}, repairToAny(t, `[1，，2]`))
	// 字符串里的逗号不动
	require.Equal(t, []any{"a,,b", ",,"}, repairToAny(t, `["a,,b",",,"]`))
}
