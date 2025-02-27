package schema

import (
	"encoding/json"
	"errors"

	"github.com/calmisland/parquet-go/common"
	"github.com/calmisland/parquet-go/parquet"
)

type JSONSchemaItemType struct {
	Tag    string
	Fields []*JSONSchemaItemType
}

func NewJSONSchemaItem() *JSONSchemaItemType {
	return new(JSONSchemaItemType)
}

func NewSchemaHandlerFromJSON(str string) (sh *SchemaHandler, err error) {
	defer func() {
		if r := recover(); r != nil {
			switch x := r.(type) {
			case string:
				err = errors.New(x)
			case error:
				err = x
			default:
				err = errors.New("unknown error")
			}
		}
	}()

	schema := NewJSONSchemaItem()
	json.Unmarshal([]byte(str), schema)

	stack := make([]*JSONSchemaItemType, 0)
	stack = append(stack, schema)
	schemaElements := make([]*parquet.SchemaElement, 0)
	infos := make([]*common.Tag, 0)

	for len(stack) > 0 {
		ln := len(stack)
		item := stack[ln-1]
		stack = stack[:ln-1]
		info := common.StringToTag(item.Tag)
		var newInfo *common.Tag
		if info.Type == "" { //struct
			schema := parquet.NewSchemaElement()
			schema.Name = info.InName
			rt := info.RepetitionType
			schema.RepetitionType = &rt
			numField := int32(len(item.Fields))
			schema.NumChildren = &numField
			schemaElements = append(schemaElements, schema)

			newInfo = common.NewTag()
			common.DeepCopy(info, newInfo)
			infos = append(infos, newInfo)

			for i := int(numField - 1); i >= 0; i-- {
				newItem := item.Fields[i]
				stack = append(stack, newItem)
			}

		} else if info.Type == "LIST" { //list
			schema := parquet.NewSchemaElement()
			schema.Name = info.InName
			rt1 := info.RepetitionType
			schema.RepetitionType = &rt1
			var numField1 int32 = 1
			schema.NumChildren = &numField1
			ct1 := parquet.ConvertedType_LIST
			schema.ConvertedType = &ct1
			schemaElements = append(schemaElements, schema)

			newInfo = common.NewTag()
			common.DeepCopy(info, newInfo)
			infos = append(infos, newInfo)

			schema = parquet.NewSchemaElement()
			schema.Name = "list"
			rt2 := parquet.FieldRepetitionType_REPEATED
			schema.RepetitionType = &rt2
			var numField2 int32 = 1
			schema.NumChildren = &numField2
			schemaElements = append(schemaElements, schema)

			newInfo = common.NewTag()
			common.DeepCopy(info, newInfo)
			newInfo.InName, newInfo.ExName = "list", "list"
			infos = append(infos, newInfo)

			stack = append(stack, item.Fields[0])

		} else if info.Type == "MAP" { //map
			schema := parquet.NewSchemaElement()
			schema.Name = info.InName
			rt1 := info.RepetitionType
			schema.RepetitionType = &rt1
			var numField1 int32 = 1
			schema.NumChildren = &numField1
			ct1 := parquet.ConvertedType_MAP
			schema.ConvertedType = &ct1
			schemaElements = append(schemaElements, schema)

			newInfo = common.NewTag()
			common.DeepCopy(info, newInfo)
			infos = append(infos, newInfo)

			schema = parquet.NewSchemaElement()
			schema.Name = "key_value"
			rt2 := parquet.FieldRepetitionType_REPEATED
			schema.RepetitionType = &rt2
			ct2 := parquet.ConvertedType_MAP_KEY_VALUE
			schema.ConvertedType = &ct2
			var numField2 int32 = 2
			schema.NumChildren = &numField2
			schemaElements = append(schemaElements, schema)

			newInfo = common.NewTag()
			common.DeepCopy(info, newInfo)
			newInfo.InName, newInfo.ExName = "key_value", "key_value"
			infos = append(infos, newInfo)

			stack = append(stack, item.Fields[1]) //put value
			stack = append(stack, item.Fields[0]) //put key

		} else { //normal variable
			schema := common.NewSchemaElementFromTagMap(info)
			schemaElements = append(schemaElements, schema)

			newInfo = common.NewTag()
			common.DeepCopy(info, newInfo)
			infos = append(infos, newInfo)
		}
	}
	res := NewSchemaHandlerFromSchemaList(schemaElements)
	res.Infos = infos
	res.CreateInExMap()
	return res, nil
}
