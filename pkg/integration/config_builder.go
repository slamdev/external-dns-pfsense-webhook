package integration

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"github.com/knadh/koanf/parsers/dotenv"
	kyaml "github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	kfile "github.com/knadh/koanf/providers/file"
	kfs "github.com/knadh/koanf/providers/fs"
	"github.com/knadh/koanf/v2"
)

var koanfMergeOpt = koanf.WithMergeFunc(mergeConfigs)

func BuildConfig(envPrefix string, fileName string, cfgFs fs.FS, out any) error {
	k := koanf.New(".")

	if err := LoadYamlConfigs(k, envPrefix, fileName, cfgFs); err != nil {
		return fmt.Errorf("failed to load yaml configs; %w", err)
	}

	if err := LoadEnvConfigs(k, envPrefix); err != nil {
		return fmt.Errorf("failed to load env configs; %w", err)
	}

	if err := k.Unmarshal("", out); err != nil {
		return fmt.Errorf("failed to unmarshal config; %w", err)
	}

	return nil
}

func LoadYamlConfigs(k *koanf.Koanf, envPrefix string, fileName string, cfgFs fs.FS) error {
	var yamlProviders []koanf.Provider

	cfgFiles := []string{fileName + ".yaml"}
	if activeProfilesStr, set := os.LookupEnv(envPrefix + "CONFIG_ACTIVE_PROFILES"); set {
		if activeProfilesStr != "" {
			for profile := range strings.SplitSeq(activeProfilesStr, ",") {
				file := fmt.Sprintf("%s-%s.yaml", fileName, strings.ToLower(strings.TrimSpace(profile)))
				cfgFiles = append(cfgFiles, file)
			}
		}
	}
	for _, file := range cfgFiles {
		yamlProviders = append(yamlProviders, kfs.Provider(cfgFs, file))
	}

	if additionalLocation, set := os.LookupEnv(envPrefix + "CONFIG_ADDITIONAL_LOCATION"); set {
		additionalSources, err := collectFSConfigFiles(additionalLocation)
		if err != nil {
			return fmt.Errorf("failed to collect configs from "+envPrefix+"CONFIG_ADDITIONAL_LOCATION env var; %w", err)
		}
		for _, path := range additionalSources {
			yamlProviders = append(yamlProviders, kfile.Provider(path))
		}
	}

	yamlParser := kyaml.Parser()
	for _, provider := range yamlProviders {
		if err := k.Load(provider, yamlParser, koanfMergeOpt); err != nil {
			return fmt.Errorf("failed to load config; %w", err)
		}
	}

	return nil
}

func LoadEnvConfigs(k *koanf.Koanf, envPrefix string) error {
	envParserFunc := func(s string) string { return strings.TrimPrefix(s, envPrefix) }
	dotenvParser := dotenv.ParserEnv(envPrefix, "_", envParserFunc)

	dotEnvPaths := []string{".env", "../.env"}

	for _, dotEnvPath := range dotEnvPaths {
		if _, err := os.Stat(dotEnvPath); err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("failed to check if %s file exists; %w", dotEnvPath, err)
			}
		} else {
			if err := k.Load(kfile.Provider(dotEnvPath), dotenvParser, koanfMergeOpt); err != nil {
				return fmt.Errorf("failed to load dotenv; %w", err)
			}
		}
	}

	if err := k.Load(env.Provider(envPrefix, "_", envParserFunc), nil, koanfMergeOpt); err != nil {
		return fmt.Errorf("failed to load env; %w", err)
	}
	return nil
}

// copy of https://github.com/knadh/koanf/blob/v0.1.1/maps/maps.go#L109
// that allows to merge case insensitive maps
// so if you have a config like:
//
//	apiKey: 123
//
// and you want to merge it with environment variable like:
//
//	APP_APIKEY: asdf
//
// then without this function the result would be:
//
//	apiKey: 123
//	apikey: asdf
//
// but with this function the result would be:
//
//	apiKey: asdf
func mergeConfigs(a, b map[string]any) error {
	for key, val := range a {
		// Does the key exist in the target map?
		// If no, add it and move on.
		destKey, bVal, ok := lookupKeyInConfigMap(key, b)
		if !ok {
			b[destKey] = val
			continue
		}

		// If the incoming val is not a map, do a direct merge.
		if _, ok := val.(map[string]any); !ok {
			b[destKey] = val
			continue
		}

		// The source key and target keys are both maps. Merge them.
		switch v := bVal.(type) {
		case map[string]any:
			if err := mergeConfigs(val.(map[string]any), v); err != nil {
				return fmt.Errorf("failed to merge configs; %w", err)
			}
		default:
			b[destKey] = val
		}
	}
	return nil
}

func lookupKeyInConfigMap(key string, m map[string]any) (string, any, bool) {
	for k := range maps.Keys(m) {
		if strings.EqualFold(k, key) {
			v := m[k]
			return k, v, v != nil
		}
	}
	return key, nil, false
}

func collectFSConfigFiles(dir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if d.Name() == "application.yaml" {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to traverse through %s; %w", dir, err)
	}
	return files, nil
}
