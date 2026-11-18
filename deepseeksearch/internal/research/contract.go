package research

import (
	"errors"
	"reflect"
	"strings"

	"deepseeksearch/internal/storage"
)

type Envelope struct {
	OK    bool        `json:"ok"`
	Data  interface{} `json:"data,omitempty"`
	Error *Error      `json:"error,omitempty"`
}

func Wrap(data interface{}, err error) Envelope {
	if data != nil {
		value := reflect.ValueOf(data)
		if (value.Kind() == reflect.Pointer || value.Kind() == reflect.Slice) && value.IsNil() {
			data = nil
		}
	}
	out := Envelope{OK: err == nil, Data: data}
	if err == nil {
		return out
	}
	var business *Error
	switch {
	case errors.As(err, &business):
		out.Error = business
	case errors.Is(err, storage.ErrResearchBusy):
		out.Error = &Error{Code: "conflict", Message: err.Error()}
	case errors.Is(err, storage.ErrResearchNotFound):
		out.Error = &Error{Code: "not_found", Message: err.Error()}
	default:
		out.Error = &Error{Code: "internal_error", Message: "操作未完成，请检查日志和数据目录后重试"}
	}
	return out
}

// Schema 从公开 DTO 反射，不访问配置、文件、数据库或网络。
func Schema() interface{} {
	session := schemaOf(reflect.TypeFor[Session]())
	draft := schemaOf(reflect.TypeFor[Draft]())
	draft["required"] = []string{"queries"}
	draftProps := draft["properties"].(map[string]interface{})
	querySchema := schemaOf(reflect.TypeFor[Query]())
	querySchema["required"] = []string{"query"}
	draftProps["queries"] = map[string]interface{}{"type": "array", "maxItems": MaxQueries, "items": querySchema}
	draftProps["count"] = map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 50, "default": DefaultCount}
	feedback := schemaOf(reflect.TypeFor[Feedback]())
	feedback["required"] = []string{"kind"}
	feedback["properties"].(map[string]interface{})["kind"] = map[string]interface{}{"type": "string", "enum": []string{"too_broad", "off_topic", "need_examples", "need_counterexamples", "custom"}}
	result := func(data interface{}) interface{} {
		return map[string]interface{}{"oneOf": []interface{}{
			map[string]interface{}{"type": "object", "required": []string{"ok", "data"}, "properties": map[string]interface{}{"ok": map[string]interface{}{"const": true}, "data": data}, "additionalProperties": false},
			map[string]interface{}{"type": "object", "required": []string{"ok", "error"}, "properties": map[string]interface{}{"ok": map[string]interface{}{"const": false}, "data": data, "error": schemaOf(reflect.TypeFor[Error]())}, "additionalProperties": false},
		}}
	}
	commands := []interface{}{}
	for _, name := range []string{"create", "list", "show", "suggest", "set-queries", "run", "feedback"} {
		props := map[string]interface{}{}
		required := []string{}
		output := interface{}(session)
		if name != "create" && name != "list" {
			props["id"] = map[string]interface{}{"type": "string", "minLength": 1}
			required = append(required, "id")
		}
		switch name {
		case "create":
			props["question"] = map[string]interface{}{"type": "string", "minLength": 1, "maxLength": 4000}
			required = append(required, "question")
		case "list":
			output = schemaOf(reflect.TypeFor[[]storage.ResearchDocument]())
		case "set-queries":
			props["draft"] = draft
			required = append(required, "draft")
		case "feedback":
			props["feedback"] = feedback
			required = append(required, "feedback")
		case "run":
			props["retryFailed"] = map[string]string{"type": "boolean"}
			props["refresh"] = map[string]string{"type": "boolean"}
		}
		commands = append(commands, map[string]interface{}{"name": "research " + name, "inputSchema": map[string]interface{}{"type": "object", "properties": props, "required": required, "additionalProperties": false}, "outputSchema": result(output)})
	}
	return map[string]interface{}{"$schema": "https://json-schema.org/draft/2020-12/schema", "interface": "cli", "name": "deepseeksearch", "scope": "research", "commands": commands}
}
func schemaOf(t reflect.Type) map[string]interface{} {
	if t.Kind() == reflect.Pointer {
		return schemaOf(t.Elem())
	}
	switch t.Kind() {
	case reflect.String:
		return map[string]interface{}{"type": "string"}
	case reflect.Bool:
		return map[string]interface{}{"type": "boolean"}
	case reflect.Int, reflect.Int64, reflect.Int32:
		return map[string]interface{}{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]interface{}{"type": "number"}
	case reflect.Slice:
		return map[string]interface{}{"type": "array", "items": schemaOf(t.Elem())}
	case reflect.Struct:
		props := map[string]interface{}{}
		required := []string{}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			tag := strings.Split(f.Tag.Get("json"), ",")
			if tag[0] == "-" {
				continue
			}
			if f.Anonymous {
				embedded := schemaOf(f.Type)
				for k, v := range embedded["properties"].(map[string]interface{}) {
					props[k] = v
				}
				required = append(required, embedded["required"].([]string)...)
				continue
			}
			name := tag[0]
			if name == "" {
				name = f.Name
			}
			props[name] = schemaOf(f.Type)
			if !strings.Contains(f.Tag.Get("json"), "omitempty") {
				required = append(required, name)
			}
		}
		return map[string]interface{}{"type": "object", "properties": props, "required": required, "additionalProperties": false}
	default:
		return map[string]interface{}{}
	}
}
