package model_fields_prefixer

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"
	"sync"
)

const prefixedColumnsPlaceholder = "{columns}"

type ModelFieldsPrefixer struct {
	//strBuilder      *strings.Builder
	bytesBuffer     *bytes.Buffer
	cache           *ModelsInfoCache
	excludeScanning map[string]struct{}

	debug bool
}

type M struct {
	N string // name of Model
	A string // DB alias for using in queries
}

func NewModelFieldsPrefixer() *ModelFieldsPrefixer {
	bytesBuffer := &bytes.Buffer{}
	bytesBuffer.Grow(256)

	return &ModelFieldsPrefixer{
		bytesBuffer: bytesBuffer,
		cache: &ModelsInfoCache{
			modelsCache: make(map[string]*ModelInfo),
			mu:          &sync.RWMutex{},
		},
		excludeScanning: make(map[string]struct{}),
		debug:           false,
	}
}

func (mp *ModelFieldsPrefixer) SetDebug(debug bool) *ModelFieldsPrefixer {
	mp.debug = debug

	return mp
}

func (mp *ModelFieldsPrefixer) handleBuilderErr(err error, str string) {
	if mp.debug && err != nil {
		fmt.Printf("failed to write string (%s) to builder: %+v\n", str, err)
	}
}

// AllocPrefixer creates new ModelFieldsPrefixer instance with the cache from the parent instance.
// Use this method if you access ModelFieldsPrefixer from multiple goroutines and you want concurrent safe behavior
func (mp *ModelFieldsPrefixer) AllocPrefixer() *ModelFieldsPrefixer {
	bytesBuffer := &bytes.Buffer{}
	bytesBuffer.Grow(256)

	return &ModelFieldsPrefixer{
		bytesBuffer:     bytesBuffer,
		cache:           mp.cache,
		excludeScanning: mp.excludeScanning,
	}
}

// CustomColumns allows to write columns in a custom way. E.g. if you need conditions, switch cases and so on
func (mp *ModelFieldsPrefixer) CustomColumns(custom string) *ModelFieldsPrefixer {
	if mp.bytesBuffer.Len() > 0 {
		mp.bytesBuffer.WriteString(", ")
	}

	mp.bytesBuffer.WriteString(custom)

	return mp
}

func (mp *ModelFieldsPrefixer) Columns(model any, dbTableAlias string, joinModels ...M) *ModelFieldsPrefixer {
	mp.bytesBuffer.Reset()

	t := reflect.TypeOf(model)

	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	tKind := t.Kind()
	tName := t.Name()

	if tKind == reflect.Ptr {
		t = t.Elem()
	}

	if tKind != reflect.Struct {
		return mp
	}

	modelInfo := mp.cache.getModelCacheValue(tName)

	if modelInfo == nil {
		modelInfo, _ = mp.collectCache(t, nil, dbTableAlias, "")

		if modelInfo != nil {
			mp.cache.setModelCacheValue(tName, modelInfo)
		}
	}

	// build string here
	var joinModelsMap map[string]M
	if len(joinModels) > 0 {
		joinModelsMap = mp.getJoinModelsMap(joinModels...)
	}

	mp.buildString(modelInfo, joinModelsMap)

	return mp
}

func (mp *ModelFieldsPrefixer) buildString(model *ModelInfo, joinModelsMap map[string]M) {
	isFullyRecursive := true

	if len(joinModelsMap) > 0 {
		isFullyRecursive = false
	}

	for _, field := range model.Fields {
		// if it is a struct and join model is exist then go recursive
		if field.IsStruct && field.ModelInfo != nil {
			joinModel, ok := joinModelsMap[field.ModelInfo.Name]

			if !isFullyRecursive && !ok {
				continue
			}

			if joinModel.A != "" {
				field.ModelInfo.DBAlias = joinModel.A
			}

			mp.buildString(field.ModelInfo, joinModelsMap)

			continue
		}

		// write first part with db alias - 'users.id'
		_, err := mp.bytesBuffer.WriteString(model.DBAlias)
		mp.handleBuilderErr(err, model.DBAlias)

		_, _ = mp.bytesBuffer.WriteString(".")

		_, err = mp.bytesBuffer.WriteString(field.DBTag)
		mp.handleBuilderErr(err, field.DBTag)

		// if this is the inner struct then write the second part - 'users_meta.user_id -->AS "um.user_id"<--'
		if model.ModelsPrefix != "" {
			_, _ = mp.bytesBuffer.WriteString(" AS \"")

			_, err = mp.bytesBuffer.WriteString(model.ModelsPrefix)
			mp.handleBuilderErr(err, model.ModelsPrefix)

			_, _ = mp.bytesBuffer.WriteString(".")

			_, err = mp.bytesBuffer.WriteString(field.DBTag)
			mp.handleBuilderErr(err, field.DBTag)

			_, _ = mp.bytesBuffer.WriteString("\"")
		}

		_, _ = mp.bytesBuffer.WriteString(", ")
	}
}

func (mp *ModelFieldsPrefixer) getJoinModelsMap(joinModels ...M) map[string]M {
	joinModelsMap := make(map[string]M)

	for _, model := range joinModels {
		if model.N == "" {
			continue
		}

		joinModelsMap[model.N] = model
	}

	return joinModelsMap
}

func (mp *ModelFieldsPrefixer) collectCache(t reflect.Type, modelInfo *ModelInfo, dbTableAlias string, modelsPrefix string) (*ModelInfo, bool) {
	modelName := t.Name()

	isAnyDBTag := false

	numField := t.NumField()

	if modelInfo == nil {
		modelInfo = &ModelInfo{
			Name:         modelName,
			DBAlias:      dbTableAlias,
			ModelsPrefix: modelsPrefix,
			Fields:       make([]*FieldInfo, 0, numField),
		}
	}

	for i := 0; i < numField; i++ {
		field := t.Field(i)

		dbTag := field.Tag.Get("db")
		if dbTag == "" || dbTag == "-" {
			continue
		}

		isAnyDBTag = true

		fieldType := field.Type
		if fieldType.Kind() == reflect.Ptr {
			fieldType = fieldType.Elem()
		}

		fieldTypeName := fieldType.Name()
		pkgPath := fieldType.PkgPath()
		excludeKey := pkgPath + "." + fieldTypeName
		_, isExcluded := mp.excludeScanning[excludeKey]

		fieldInfo := &FieldInfo{
			DBTag: dbTag,
		}

		switch fieldType.Kind() {
		case reflect.Ptr:
			if fieldType.Elem().Kind() == reflect.Struct && !isExcluded {
				var innerModel *ModelInfo

				modelsPrefixToPass := dbTag
				if modelsPrefix != "" {
					modelsPrefixToPass = modelsPrefix + "." + dbTag
				}

				innerModel, isAnyDBTag = mp.collectCache(fieldType.Elem(), innerModel, dbTag, modelsPrefixToPass)

				if !isAnyDBTag {
					mp.excludeScanning[excludeKey] = struct{}{}

					break
				}

				fieldInfo.IsStruct = true
				fieldInfo.ModelInfo = innerModel
			}

		case reflect.Struct:
			if !isExcluded {
				var innerModel *ModelInfo

				modelsPrefixToPass := dbTag
				if modelsPrefix != "" {
					modelsPrefixToPass = modelsPrefix + "." + dbTag
				}

				innerModel, isAnyDBTag = mp.collectCache(fieldType, innerModel, dbTag, modelsPrefixToPass)

				if !isAnyDBTag {
					mp.excludeScanning[excludeKey] = struct{}{}

					break
				}

				fieldInfo.IsStruct = true
				fieldInfo.ModelInfo = innerModel
			}

		case reflect.Slice:
			elemType := fieldType.Elem()

			// []Struct
			if elemType.Kind() == reflect.Struct && !isExcluded {
				var innerModel *ModelInfo

				modelsPrefixToPass := dbTag
				if modelsPrefix != "" {
					modelsPrefixToPass = modelsPrefix + "." + dbTag
				}

				innerModel, isAnyDBTag = mp.collectCache(elemType, nil, dbTag, modelsPrefixToPass)

				if !isAnyDBTag {
					mp.excludeScanning[excludeKey] = struct{}{}

					break
				}

				fieldInfo.IsStruct = true
				fieldInfo.ModelInfo = innerModel
			}

			// []*Struct
			if elemType.Kind() == reflect.Ptr && elemType.Elem().Kind() == reflect.Struct && !isExcluded {
				var innerModel *ModelInfo

				modelsPrefixToPass := dbTag
				if modelsPrefix != "" {
					modelsPrefixToPass = modelsPrefix + "." + dbTag
				}

				innerModel, isAnyDBTag = mp.collectCache(elemType.Elem(), nil, dbTag, modelsPrefixToPass)

				if !isAnyDBTag {
					mp.excludeScanning[excludeKey] = struct{}{}

					break
				}

				fieldInfo.IsStruct = true
				fieldInfo.ModelInfo = innerModel
			}

		default:
		}

		modelInfo.Fields = append(modelInfo.Fields, fieldInfo)
	}

	return modelInfo, isAnyDBTag
}

func (mp *ModelFieldsPrefixer) WithinQuery(query string) string {
	if mp.bytesBuffer == nil {
		return ""
	}

	strings.ReplaceAll(query, prefixedColumnsPlaceholder, mp.bytesBuffer.String())

	mp.bytesBuffer.Reset()

	return mp.bytesBuffer.String()
}

func (mp *ModelFieldsPrefixer) String() string {
	if mp.bytesBuffer == nil || mp.bytesBuffer.Len() == 0 {
		return ""
	}

	mp.bytesBuffer.Truncate(mp.bytesBuffer.Len() - 2)

	return mp.bytesBuffer.String()
}
