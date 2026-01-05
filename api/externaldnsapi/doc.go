package externaldnsapi

import (
	_ "github.com/getkin/kin-openapi/openapi3"
	_ "github.com/oapi-codegen/oapi-codegen/v2/pkg/codegen"
	_ "gopkg.in/yaml.v2"
)

//go:generate go tool oapi-codegen --config=gen-config.yaml openapi.yaml
