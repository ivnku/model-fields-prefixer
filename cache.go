package model_fields_prefixer

import (
	"sync"
)

type ModelsInfoCache struct {
	modelsCache map[string]*ModelInfo
	mu          *sync.RWMutex
}

type ModelInfo struct {
	Name string
	// DBAlias is an alias for a table which this field (column) belongs to. Used as prefix in queries
	DBAlias string
	// ModelsPrefix is concatenated string of all parent db tags, e.g. 'users.users_meta.'
	ModelsPrefix string
	Fields       []*FieldInfo
}

type FieldInfo struct {
	// DBTag is actual db column name if this field is not struct, if it is a struct then DBTag can be any string name
	DBTag     string
	IsStruct  bool
	ModelInfo *ModelInfo
}

func (c *ModelsInfoCache) getModelCacheValue(modelName string) *ModelInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.modelsCache[modelName]
}

func (c *ModelsInfoCache) setModelCacheValue(modelName string, modelInfo *ModelInfo) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	c.modelsCache[modelName] = modelInfo
}
