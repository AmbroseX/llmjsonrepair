# llmjsonrepair

修复大模型吐出的坏 JSON：缺括号、多括号、单引号、围栏、结尾逗号、被截断，以及全角括号冒号逗号、几个 JSON 连着写、连续逗号。
只依赖标准库，Go 1.25 可用。

## 用法

```bash
go get github.com/AmbroseX/llmjsonrepair
```

```go
import "github.com/AmbroseX/llmjsonrepair"

fixed, err := llmjsonrepair.Repair(`{"title":"甲","ids":["n1","n2"]}}`)
// fixed == `{"title":"甲","ids":["n1","n2"]}`
```

`Repair` 修不好时回 error，不会 panic。修好的结果再用 `encoding/json` 解。

## 来源

- 从 [kaptinlin/jsonrepair](https://github.com/kaptinlin/jsonrepair) v0.4.9 拉出来改的，它是 JS 版 [jsonrepair](https://github.com/josdejong/jsonrepair) 的 Go 移植。
  原库的新版本要求 Go 1.26，而且要按大模型常出的错补修法，所以单独维护。
- 许可证：MIT，原作者的版权保留在 `LICENSE` 里。
- 原库的 `example/` 没有拿。`llm_cases_test.go` 是新加的，里面都是大模型真实出过的错。

## 跟原库比改了什么

1. 原库用 `github.com/go-json-experiment/json` 做字符串转义（`parse_scalars.go` 的 `parseRegex`），
   换成标准库 `encoding/json`，不引第三方依赖。标准库默认会把 `<` `>` `&` 转义，
   所以用 `Encoder.SetEscapeHTML(false)` 关掉，输出和原来一样。
2. `repair_test.go` 里 `errors.AsType`（Go 1.26 才有）换成 `errors.As`；测试里的 json 也换成标准库。
3. 原库 `go.mod` 要求 Go 1.26.4，这里要求 Go 1.25，除上面两处没有别的语法需要改。包名从 `jsonrepair` 改成 `llmjsonrepair`。
4. `parseObject`：对象一开头又多了一个 `{`（大模型写过 `[{"a":1},{{"b":2}]`）时跳过它，
   原库会补出一个 `{}`、后面再缺逗号，结果解不开。只管第一个键的位置：逗号后面的 `{` 原库当成前一个对象少了 `}`
   （`[{"i":1,{"i":2}]`，原库自带用例），不改。用例 `TestLLMDoubleOpenBrace`。
5. 全角括号：`｛ ｝ ［ ］` 在对象和数组的开头结尾位置当成 `{ } [ ]`，输出的永远是半角。
   用例 `TestLLMFullWidthBrackets`、`TestLLMFullWidthBracketsNested`。
6. 全角冒号、全角逗号：`：` `，` 在键值之间、元素之间当成 `:` `,`。
   - 字符串里面的全角标点一个都不动（`"说明：甲，乙"` 原样保留）。
   - 结束引号后面紧跟 `，` 或 `：` 时，要看后面接的是什么：是下一个值的开头（引号、括号、字母数字、`-`）、右括号、逗号或文本结尾，才算结构字符；
     否则当成字符串正文，因为中文正文里常有没转义的引号加全角逗号（`"他称之为"神作"，评价很高"`）。
     所以后面紧跟汉字的 `，` 不会被当成逗号，`{"a":"x"，乙:1}` 这种修不了。
   - 没加引号的键遇到 `：` 结束，没加引号的值遇到 `，` 或全角括号结束，跟半角的 `:` `,` 同样处理（`{名称：甲，年龄：3}`、`[甲，乙]` 能修）。
   - 数字、`true`/`false`/`null` 后面跟全角结构字符也算结束。
   用例 `TestLLMFullWidthColonComma`、`TestLLMFullWidthAll`、`TestLLMFullWidthPunctuationInStringUntouched`、
   `TestLLMFullWidthCommaAfterInnerQuote`、`TestLLMFullWidthUnquoted`。
7. 多个 JSON 连着写：按 Python 版 json_repair 的做法，多个顶层值包成一个数组返回。
   原库已经能处理换行或逗号隔开的（NDJSON），这里补上不隔开的：`{"a":1}{"b":2}`、`[1][2]`、`{"a":1} {"b":2}`、`{"a":1}[2]`。
   - 第一个值必须是对象或数组，后面接着的也必须以 `{` 或 `[` 开头；空的 `{}`、`[]` 不算（原库自带用例要求 `{"a":2}{}` 报错）。
   - 后面跟的解释文字不会当成数组元素，照旧报错（`{"a":1}{"b":2} 以上是结果`）。
   用例 `TestLLMConcatenatedValues`、`TestLLMConcatenatedValuesTrailingText`。
8. 连续逗号：`[1,,2]`、`{"a":1,,"b":2}`、`[1,,,]`、`{,,"a":1}`，多余的逗号丢掉；逗号之间夹空白、注释、全角逗号也一样。
   字符串里的逗号不动。用例 `TestLLMConsecutiveCommas`。
9. 为了过 golangci-lint 的复杂度和 unparam 检查，把大函数拆成名字说得清干什么的小函数，行为不变：
   - `parseString` 拆成 `stringParser` 的一组方法（`onQuote`、`onBackslash`、`onUnicodeEscape`、`onPlainChar` 等）。
   - `parseObject` 拆出 `parseObjectComma`、`parseObjectMember`、`parseObjectKey`、`closeContainer` 等；`parseArray` 拆出 `parseArrayComma`、`trimCommaInsideLastString`。
   - `parseNumber` 拆出 `scanMinusSign`、`scanFraction`、`scanExponent`、`writeNumber`。
   - `parseUnquotedStringWithMode` 拆出 `parseFunctionCall`、`skipUnquotedText`、`writeUnquotedSymbol`。
   - `isURLChar` 改成 `strings.ContainsRune` 查表；`hasExcessiveEscapeSequences` 拆出 `countUnicodeEscapes`、`countSimpleEscapes`。
   - 没人用的 bool 返回值去掉：`parseMarkdownCodeBlock`、`parseConcatenatedString`、`parseWhitespaceAndSkipComments`、`parseWhitespace`、`skipEllipsis`。
   拆完用随机拼出来的约 80 万条坏输入对照拆之前的版本：拆之前能修好的，输出一字不差。

原库自带的测试（`repair_test.go`、`utils_test.go`、`repair_depth_test.go`）没有改期望值，全部通过。

## 不在这个库里管的

- JSON 前后夹着解释文字的，这个库会报错，或者把文字和 JSON 包成一个数组。调用方先截出第一个 `{` 或 `[` 到最后一个 `}` 或 `]` 那一段再修。
- 几个 JSON 连着写会被包成数组，要解进单个对象的，调用方自己取元素。
- 还修不了：一个值带两个冒号、键值之间夹杂词、开头的 `- ` 列表符号、单词里的撇号（`'it's'`）、`TRUE`/`NaN` 这类常量。
  这几种是对照 Python 版 json_repair 的 Go 移植（github.com/RealAlexandreAI/json-repair，GPL-3，只参考做法、没有复制代码）列出来的。
