# Benchmark methodology

Goal: measure what the trigram index buys over scanning the same files, without flattering the result.

## Corpus

`trigrep bench` calls `GenerateCorpus(dir, nFiles, seed=1)`: `nFiles` files of 200 lines, 8 words each, drawn from a fixed 33-word vocabulary with a seeded PRNG, in 20 subdirectories. Two needles are planted: `NEEDLE_rare_identifier_42` in every 97th file and `ErrConnectionReset` in every 13th. The corpus is identical for every run and every machine.

Limits of the corpus: a tiny vocabulary means common-word trigrams appear in nearly every file. That makes the index look bad on common queries and good on planted ones. How a real source tree behaves is not measured here.

## Procedure

1. Build the index once (not timed; index time is reported by `trigrep index`).
2. For each pattern, run the indexed search and the full scan and **fail the benchmark if the full match lists (file, line, text) differ**.
3. Warm-up: the correctness run above doubles as the warm-up, so the page cache is hot for both paths.
4. Time N runs per path (default 7), report the median.
5. Both paths use the same regexp, file reader and line splitter. The only difference is the candidate list.

## Patterns and what each probes

| pattern | probes |
|---|---|
| `NEEDLE_rare_identifier_42` | best case: rare literal |
| `ErrConnection\w+` | rare literal prefix inside a regex |
| `rollback replica leader` | common words: index cannot narrow |
| `(?i)needle_rare` | case-insensitive: no usable literal, fallback path |
| `socket buffer` | common phrase |

## Reporting rules

- Report the machine, Go version, file count and run count next to any numbers (the README sample: 2 vCPU Intel Xeon @ 2.60GHz, Go 1.26.6, linux/amd64, 3000 files, 7 runs).
- The indexed path includes a `stat` of every indexed file to detect changes; that cost is part of the measurement.
- Shared cloud hardware is noisy. Re-run before trusting a difference smaller than about 20 percent.
- Report slowdowns (candidates equal to total files adds only overhead).
- Do not compare against ripgrep with this tool; ripgrep is a different design with different strengths. A fair comparison needs a real corpus, cold and warm cache, and is on the roadmap.

## Reproduce

```sh
go run ./cmd/trigrep bench -files 3000 -runs 7
```
