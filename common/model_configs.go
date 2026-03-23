package common

// ConfigRegistry is the list of tested configurations. Each config contains a copy of the
// ModelInfo it's based on (value, not pointer, to prevent mutation of shared defaults).
//
// Tests loop over all configs, creating an embedder for each and running assertions.
// The CLI prints available configs after downloading a model.
//
// ModelOverheadMB values are measured via /proc/self/status VmRSS delta. Models marked with
// (measured) were directly tested; others are estimated at ~3.5× INT8 file size.
//
// OptimalChunkTokens values follow evidence from Vectara (NAACL 2025) and Fraunhofer IAIS
// (2025): 384d + small params → 200-256, 384d + large params → 384, 768d → 512.
var ConfigRegistry []ModelConfig

func init() {
	ConfigRegistry = []ModelConfig{
		// --- multilingual-e5-small ---
		// NOTE: e5-small uses BertModel architecture which expects token_type_ids (pass zeros).
		// e5-base uses XLMRobertaModel which does not accept token_type_ids. This is why two
		// models from the same e5 family have different InputNames.
		{
			Model: model("multilingual-e5-small"), ConfigName: "multilingual-e5-small",
			Description: "Default. Best quality/size for multilingual.",
			Dimensions: 384, Pooling: PoolingMean, OptimalChunkTokens: 384,
			QueryPrefix: "query: ", PassagePrefix: "passage: ",
			InputNames: []string{"input_ids", "attention_mask", "token_type_ids"},
			OutputName: "last_hidden_state",
			ModelOverheadMB: 378, BestFor: "All use cases", // measured
		},
		// --- multilingual-e5-base ---
		// NOTE: e5-base uses XLMRobertaModel — no token_type_ids. See e5-small note above.
		{
			Model: model("multilingual-e5-base"), ConfigName: "multilingual-e5-base",
			Description: "Higher quality multilingual. 2x memory vs e5-small.",
			Dimensions: 768, Pooling: PoolingMean, OptimalChunkTokens: 512,
			QueryPrefix: "query: ", PassagePrefix: "passage: ",
			InputNames: []string{"input_ids", "attention_mask"},
			OutputName: "last_hidden_state",
			ModelOverheadMB: 930, BestFor: "Quality-critical multilingual", // estimated
		},
		// --- e5-small-v2 ---
		{
			Model: model("e5-small-v2"), ConfigName: "e5-small-v2",
			Description: "Lightweight English with e5 quality.",
			Dimensions: 384, Pooling: PoolingMean, OptimalChunkTokens: 256,
			QueryPrefix: "query: ", PassagePrefix: "passage: ",
			InputNames: []string{"input_ids", "attention_mask", "token_type_ids"},
			OutputName: "last_hidden_state",
			ModelOverheadMB: 120, BestFor: "Lightweight English", // estimated
		},
		// --- all-MiniLM-L6-v2 ---
		{
			Model: model("all-MiniLM-L6-v2"), ConfigName: "all-MiniLM-L6-v2",
			Description: "Most widely used small model. No prefixes needed.",
			Dimensions: 384, Pooling: PoolingMean, OptimalChunkTokens: 200,
			QueryPrefix: "", PassagePrefix: "",
			InputNames: []string{"input_ids", "attention_mask", "token_type_ids"},
			OutputName: "last_hidden_state",
			ModelOverheadMB: 96, BestFor: "Smallest with broad validation", // measured
		},
		// --- bge-small-en-v1.5 ---
		{
			Model: model("bge-small-en-v1.5"), ConfigName: "bge-small-en-v1.5",
			Description: "Strong small English retrieval. CLS pooling.",
			Dimensions: 384, Pooling: PoolingCLS, OptimalChunkTokens: 256,
			QueryPrefix:   "Represent this sentence for searching relevant passages: ",
			PassagePrefix: "",
			InputNames: []string{"input_ids", "attention_mask"},
			OutputName: "token_embeddings",
			ModelOverheadMB: 120, BestFor: "English retrieval, CLS pooling", // estimated
		},
		// --- bge-base-en-v1.5 ---
		{
			Model: model("bge-base-en-v1.5"), ConfigName: "bge-base-en-v1.5",
			Description: "Strong base English retrieval. CLS pooling.",
			Dimensions: 768, Pooling: PoolingCLS, OptimalChunkTokens: 512,
			QueryPrefix:   "Represent this sentence for searching relevant passages: ",
			PassagePrefix: "",
			InputNames: []string{"input_ids", "attention_mask"},
			OutputName: "token_embeddings",
			ModelOverheadMB: 385, BestFor: "English retrieval, CLS pooling", // estimated
		},
		// --- nomic-embed-text-v1.5 (full 768d) ---
		{
			Model: model("nomic-embed-text-v1.5"), ConfigName: "nomic-embed-text-v1.5-768d",
			Description: "Full 768 dimensions. Best for long documents (8192 seq len).",
			Dimensions: 768, Pooling: PoolingMean, OptimalChunkTokens: 512,
			QueryPrefix: "search_query: ", PassagePrefix: "search_document: ",
			InputNames: []string{"input_ids", "token_type_ids", "attention_mask"},
			OutputName: "last_hidden_state",
			PostProcess:     func(v []float32) []float32 { return LayerNorm(v) },
			ModelOverheadMB: 480, BestFor: "Long documents, max quality", // estimated
		},
		// --- nomic-embed-text-v1.5 (Matryoshka 384d) ---
		{
			Model: model("nomic-embed-text-v1.5"), ConfigName: "nomic-embed-text-v1.5-384d",
			Description: "Matryoshka 384d truncation. Same model, half the vector storage.",
			Dimensions: 384, ModelDimensions: 768, Pooling: PoolingMean,
			OptimalChunkTokens: 384,
			QueryPrefix: "search_query: ", PassagePrefix: "search_document: ",
			InputNames: []string{"input_ids", "token_type_ids", "attention_mask"},
			OutputName: "last_hidden_state",
			PostProcess: func(v []float32) []float32 {
				v = LayerNorm(v)
				return v[:384]
			},
			ModelOverheadMB: 480, BestFor: "Long documents, compact vectors", // estimated
		},
		// --- snowflake-arctic-embed-s ---
		{
			Model: model("snowflake-arctic-embed-s"), ConfigName: "snowflake-arctic-embed-s",
			Description: "Top small retrieval score. CLS pooling.",
			Dimensions: 384, Pooling: PoolingCLS, OptimalChunkTokens: 256,
			QueryPrefix:   "Represent this sentence for searching relevant passages: ",
			PassagePrefix: "",
			InputNames: []string{"input_ids", "attention_mask", "token_type_ids"},
			OutputName: "last_hidden_state",
			ModelOverheadMB: 120, BestFor: "Top small retrieval score", // estimated
		},
		// --- snowflake-arctic-embed-m ---
		{
			Model: model("snowflake-arctic-embed-m"), ConfigName: "snowflake-arctic-embed-m",
			Description: "Top base retrieval score. CLS pooling.",
			Dimensions: 768, Pooling: PoolingCLS, OptimalChunkTokens: 512,
			QueryPrefix:   "Represent this sentence for searching relevant passages: ",
			PassagePrefix: "",
			InputNames: []string{"input_ids", "attention_mask", "token_type_ids"},
			OutputName: "last_hidden_state",
			ModelOverheadMB: 385, BestFor: "Top base retrieval score", // estimated
		},
		// --- bge-micro-v2 ---
		{
			Model: model("bge-micro-v2"), ConfigName: "bge-micro-v2",
			Description: "Absolute smallest model. No prefixes.",
			Dimensions: 384, Pooling: PoolingMean, OptimalChunkTokens: 200,
			QueryPrefix: "", PassagePrefix: "",
			InputNames: []string{"input_ids", "attention_mask", "token_type_ids"},
			OutputName: "last_hidden_state",
			ModelOverheadMB: 66, BestFor: "Absolute smallest", // measured
		},
	}
}

// model looks up a ModelInfo by name from the registry. Panics if not found — this is only
// called during init() where all names are known at compile time.
func model(name string) ModelInfo {
	m := FindModel(name)
	if m == nil {
		panic("common: unknown model " + name)
	}
	return *m
}

// FindConfig looks up a config by name. Returns nil if not found.
func FindConfig(name string) *ModelConfig {
	for i := range ConfigRegistry {
		if ConfigRegistry[i].ConfigName == name {
			return &ConfigRegistry[i]
		}
	}
	return nil
}

// ConfigsForModel returns all configs that reference the given model name.
func ConfigsForModel(modelName string) []ModelConfig {
	var configs []ModelConfig
	for _, c := range ConfigRegistry {
		if c.Model.Name == modelName {
			configs = append(configs, c)
		}
	}
	return configs
}
