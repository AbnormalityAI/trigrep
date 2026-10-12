<p align="center"><img src="assets/banner.svg" alt="trigrep" width="100%"></p>

# trigrep

> A small trigram-indexed code search CLI with a published benchmark method.

[![ci](https://github.com/AbnormalityAI/trigrep/actions/workflows/ci.yml/badge.svg)](https://github.com/AbnormalityAI/trigrep/actions/workflows/ci.yml)
![go](https://img.shields.io/badge/go-1.22%2B-00ADD8?logo=go&logoColor=white)
[![license: MIT](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)
![status](https://img.shields.io/badge/status-recreational%20grade-orange)

`trigrep` builds a trigram inverted index once, then answers regex searches by intersecting posting lists and running the real regexp only on the few files that could match. On files unchanged since indexing, results equal a full scan (tested, and checked again by every benchmark run). The repo publishes how speed is measured and where the index does **not** help.

Inspired by [BurntSushi/ripgrep](https://github.com/BurntSushi/ripgrep) and the classic trigram index design behind code search engines. ripgrep is deliberately index-free; `trigrep` explores the other trade-off and is not a clone. It is written in Go rather than Rust as a deliberate toolchain choice.

## Features

- Trigram index with delta-varint posting lists, saved to `.trigrep/index`
- Regex queries: required literals are extracted from the parsed regex (`regexp/syntax`) and used to narrow candidates
- Safe fallback: patterns with no usable literal (case-insensitive, short, alternation) scan everything and still return correct results
- Detects indexed files that changed since indexing (size or mtime) and always scans them, so edits are not missed
- Validates the index on load (bounded sizes, strictly increasing in-range posting IDs, relative paths only) and is fuzzed
- Skips binary files, `.git`, `node_modules`, `vendor`
- `-stats` shows candidates versus files scanned
- Built-in benchmark with a deterministic generated corpus and an indexed-equals-scan correctness check

## Quickstart

```sh
# from a checkout
git clone https://github.com/AbnormalityAI/trigrep && cd trigrep
go build -o trigrep ./cmd/trigrep
# or, once published: go install github.com/AbnormalityAI/trigrep/cmd/trigrep@latest

./trigrep index -root .
./trigrep search -root . -stats 'func \w+Handler'
./trigrep search -root . -F 'TODO(alice)'
./trigrep bench -files 3000 -runs 7
```

`search` exits 0 on a match and 1 on none, like grep.

## Architecture

![architecture](assets/architecture.svg)

- **Index:** each file contributes the set of distinct trigrams it contains. `trigram -> sorted file IDs`.
- **Query planning:** the pattern is parsed and simplified. Literals of 3+ bytes found in concatenations, captures, `+` and `{n,}` are required. Case folding disables a literal, since the index is case-sensitive.
- **Execution:** intersect the shortest posting lists first, then read and regex-match only the surviving files line by line.
- **Staleness:** the index is a snapshot. At search time every indexed file is `stat`ed; files whose size or mtime changed are added to the candidates. Files created after indexing are not found until `trigrep index` is re-run, and an edit that keeps both size and mtime identical is not noticed. The `stat` pass costs a few milliseconds per few thousand files, which shows up in the benchmark.

## Benchmarks

Methodology lives in [docs/BENCHMARKS.md](docs/BENCHMARKS.md). Sample run: 3000 generated files (600k lines), median of 7, warm page cache, 2 vCPU Intel Xeon @ 2.60GHz, Go 1.26.6, linux/amd64. Timings on shared hardware vary run to run by tens of percent; only the shape matters.

| pattern | matches | candidates/files | indexed | full scan | speedup |
|---|---|---|---|---|---|
| `NEEDLE_rare_identifier_42` | 31 | 31/3000 | 5.5ms | 82.3ms | 15.1x |
| `ErrConnection\w+` | 231 | 231/3000 | 11.5ms | 126.0ms | 11.0x |
| `rollback replica leader` | 111 | 2943/3000 | 113.1ms | 104.7ms | 0.9x |
| `(?i)needle_rare` | 31 | 3000/3000 | 1132ms | 945ms | 0.8x |
| `socket buffer` | 3745 | 3000/3000 | 99.5ms | 108.7ms | 1.1x |

The honest reading: on this synthetic corpus the index wins on rare literals and does nothing, or slightly hurts, for common ones, because with a 33-word vocabulary nearly every file contains nearly every trigram. The numbers say nothing about real repositories, whose trigram distribution is different and has not been measured here.

## Known limitations

- Case-insensitive search is not indexed.
- Files created after indexing are not searched until you re-index; an edit that leaves size and mtime unchanged is not noticed.
- Whole index is loaded into memory on each search.
- Files over 8 MiB are skipped.
- Not a ripgrep replacement: no gitignore handling, no multiline mode, no colour.

## Roadmap

- [ ] Incremental re-index that also picks up new files (changed files are already detected at search time)
- [ ] Case-folded trigram index for `-i`
- [ ] mmap-backed postings instead of load-all
- [ ] Respect `.gitignore`
- [ ] Benchmark against a real public repository checkout, documented by commit hash

## License

MIT, see [LICENSE](LICENSE).
