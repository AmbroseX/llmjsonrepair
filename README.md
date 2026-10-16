# llmjsonrepair

English | [简体中文](README.zh-CN.md)

A Go library that repairs broken JSON produced by LLMs. Standard library only.

Requires Go 1.22 or later. CI runs the tests on every release from 1.22 to 1.26.

```bash
go get github.com/AmbroseX/llmjsonrepair
```

```go
fixed, err := llmjsonrepair.Repair(`{"title":"甲","ids":["n1","n2"]}}`)
// fixed == `{"title":"甲","ids":["n1","n2"]}`
```

## What it fixes

- Missing or extra brackets, a duplicated leading `{`, truncated output
- Single quotes, curly quotes, unquoted keys and values, trailing commas, missing commas, missing colons
- Full-width brackets, colons and commas (`｛"a"："甲"，"b"：［1，2］｝`); full-width punctuation inside strings is left alone
- Concatenated JSON values (`{"a":1}{"b":2}`), wrapped into an array
- Repeated commas (`[1,,2]`, `{"a":1,,"b":2}`)
- Extra colons (`{"a"::1}`), stray words after a colon (`{"a": value is "x"}`), leading `- ` list markers
- Apostrophes inside words (`{'text': 'it's ok'}`)
- ````json` fences, comments, JSONP, `None`/`True`/`False`/`TRUE`/`NULL`; `NaN`/`Infinity` become `null`
- Returns an error instead of panicking when the input can't be repaired

## Acknowledgements

- [josdejong/jsonrepair](https://github.com/josdejong/jsonrepair): the original JS implementation that all the repair rules come from
- [kaptinlin/jsonrepair](https://github.com/kaptinlin/jsonrepair): its Go port; this library was forked from v0.4.9
- [RealAlexandreAI/json-repair](https://github.com/RealAlexandreAI/json-repair), [mangiucugna/json_repair](https://github.com/mangiucugna/json_repair): ideas for handling LLM output

## License

MIT
