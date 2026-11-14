# config

[`tokens.yaml`](tokens.yaml) is the fallback token list. Argus uses it only when both the `intents.near` enumeration and the 1Click supported-tokens API are unavailable. Entries also override symbol and decimals from the other sources.

The list is not exhaustive. A token missing from it is not untracked by Intents. The source for every row is named in the file header and in [`../docs/research.md`](../docs/research.md).
