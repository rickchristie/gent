package main

import "github.com/rickchristie/gent/common"

// Re-export from common for convenience within cmd/gent.
type ModelInfo = common.ModelInfo

var Registry = common.ModelRegistry

func FindModel(name string) *ModelInfo { return common.FindModel(name) }
