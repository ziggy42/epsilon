package epsilon

// ExperimentalFeatures are features that have not yet been validated. In
// particular, these are WASM 3.0 features for which we don't have spec tests
// yet (see https://github.com/WebAssembly/wabt/issues/2648)
// See also https://webassembly.org/news/2025-09-17-wasm-3.0/.
type ExperimentalFeatures struct {
	MultipleMemories bool
}
