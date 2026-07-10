package config

import (
	"fmt"
	"reflect"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
)

// Load unmarshals the full configuration from the given viper instance.
// Because command flags and GOBOTTLE_* env vars are bound into viper, this
// single call realizes the whole precedence chain:
// flag > env > config file > flag default.
func Load(v *viper.Viper) (*Config, error) {
	var cfg Config

	hook := viper.DecodeHook(mapstructure.ComposeDecodeHookFunc(
		mapstructure.StringToTimeDurationHookFunc(),
		mapstructure.StringToSliceHookFunc(","),
		stringToFormulaConfigHook,
		stringToBinaryConfigHook,
	))

	if err := v.Unmarshal(&cfg, hook); err != nil {
		return nil, fmt.Errorf("failed to parse configuration: %w", err)
	}

	return &cfg, nil
}

// stringToFormulaConfigHook accepts "formula: mytool" as shorthand for
// "formula: {name: mytool}".
func stringToFormulaConfigHook(from reflect.Type, to reflect.Type, data any) (any, error) {
	if from.Kind() == reflect.String && to == reflect.TypeOf(FormulaConfig{}) {
		return FormulaConfig{Name: data.(string)}, nil
	}
	return data, nil
}

// stringToBinaryConfigHook accepts "binaries: [a, b]" as shorthand for
// "binaries: [{name: a}, {name: b}]".
func stringToBinaryConfigHook(from reflect.Type, to reflect.Type, data any) (any, error) {
	if from.Kind() == reflect.String && to == reflect.TypeOf(BinaryConfig{}) {
		return BinaryConfig{Name: data.(string)}, nil
	}
	return data, nil
}
