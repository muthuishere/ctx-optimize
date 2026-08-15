# Headless benchmark — gorilla-mux

Model: `openai/gpt-4o-mini` · via OpenRouter · same model, same questions, one freshly-cloned repo.
Arms: **a** shell-only · **b** ctx-optimize store first · **c** graphify store first.
Tokens and cost are OpenRouter's own accounting (`usage.include=true`), not estimates. Deltas are vs arm a.

| question | arm | wall s | tokens | cost | steps |
|---|---|--:|--:|--:|--:|
| q1 | a shell | 3.71 | 2851 | $0.0005 | 2 |
| q1 | b ctx-optimize | 2.12 | 2830 | $0.0005 | 1 |
| q1 | c graphify | 1.95 | 2454 | $0.0004 | 1 |
| q2 | a shell | 3.99 | 4009 | $0.0007 | 4 |
| q2 | b ctx-optimize | 3.22 | 2878 | $0.0005 | 1 |
| q2 | c graphify | 3.01 | 4707 | $0.0006 | 2 |
| q3 | a shell | 6.1 | 7520 | $0.0011 | 3 |
| q3 | b ctx-optimize | 4.34 | 2964 | $0.0005 | 1 |
| q3 | c graphify | 5.35 | 2564 | $0.0005 | 1 |
| q4 | a shell | 2.71 | 698 | $0.0001 | 2 |
| q4 | b ctx-optimize | 2.09 | 987 | $0.0002 | 1 |
| q4 | c graphify | 8.42 | 8627 | $0.0011 | 4 |

## Totals (4 questions)

| metric | a shell | b ctx-optimize | c graphify | b vs a | c vs a |
|---|--:|--:|--:|--:|--:|
| wall time | 16.5s | 11.8s | 18.7s | **−29%** | **+13%** |
| tokens | 15078 | 9659 | 18352 | **−36%** | **+22%** |
| cost | $0.0024 | $0.0016 | $0.0025 | **−31%** | **+7%** |
| steps | 11 | 4 | 8 | **−64%** | **−27%** |
