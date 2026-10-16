# llmjsonrepair

修复大模型吐出的坏 JSON 的 Go 库，只依赖标准库，Go 1.25 可用。

```bash
go get github.com/AmbroseX/llmjsonrepair
```

```go
fixed, err := llmjsonrepair.Repair(`{"title":"甲","ids":["n1","n2"]}}`)
// fixed == `{"title":"甲","ids":["n1","n2"]}`
```

## 能修什么

- 缺括号、多括号、对象开头多一个 `{`、被截断
- 单引号、弯引号、没加引号的键和值、结尾逗号、缺逗号、缺冒号
- 全角括号、全角冒号逗号（`｛"a"："甲"，"b"：［1，2］｝`），字符串里的全角标点不动
- 几个 JSON 连着写（`{"a":1}{"b":2}`），包成一个数组
- 连续逗号（`[1,,2]`、`{"a":1,,"b":2}`）
- 多余的冒号（`{"a"::1}`）、冒号后面夹的词（`{"a": value is "x"}`）、开头的 `- ` 列表符号
- 单词里的撇号（`{'text': 'it's ok'}`）
- ````json` 围栏、注释、JSONP、`None`/`True`/`False`/`TRUE`/`NULL`，`NaN`/`Infinity` 转成 `null`
- 修不好时回 error，不会 panic

## 致谢

- [josdejong/jsonrepair](https://github.com/josdejong/jsonrepair)：最早的 JS 版，修法都源自它
- [kaptinlin/jsonrepair](https://github.com/kaptinlin/jsonrepair)：它的 Go 移植，本库从 v0.4.9 拉出来改的
- [RealAlexandreAI/json-repair](https://github.com/RealAlexandreAI/json-repair)、[mangiucugna/json_repair](https://github.com/mangiucugna/json_repair)：参考了它们修大模型输出的思路



## 许可证

MIT