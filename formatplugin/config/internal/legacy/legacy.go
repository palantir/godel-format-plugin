// Copyright 2016 Palantir Technologies, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package legacy

import (
	"reflect"
	"sort"

	"github.com/palantir/pkg/matcher"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"

	"github.com/palantir/godel-format-plugin/formatplugin"
	"github.com/palantir/godel-format-plugin/formatplugin/config/internal/v0"
)

type Config struct {
	Legacy bool `yaml:"legacy-config"`

	// Formatters specifies the configuration used by the formatters. The key is the name of the formatter and the
	// value is the custom configuration for that formatter.
	Formatters map[string]FormatterConfig `yaml:"formatters"`

	// Exclude specifies the files that should be excluded from formatting.
	Exclude matcher.NamesPathsCfg `yaml:"exclude"`
}

type FormatterConfig struct {
	// Args specifies the command-line arguments that are provided to the formatter.
	Args []string `yaml:"args"`
}

type AssetConfig struct {
	Legacy bool     `yaml:"legacy-config"`
	Args   []string `yaml:"args"`
}

func IsLegacyConfig(cfgBytes []byte) bool {
	var cfg Config
	if err := yaml.Unmarshal(cfgBytes, &cfg); err != nil {
		return false
	}
	return cfg.Legacy
}

func UpgradeConfig(cfgBytes []byte, factory formatplugin.Factory) ([]byte, error) {
	var legacyCfg Config
	if err := yaml.UnmarshalStrict(cfgBytes, &legacyCfg); err != nil {
		return nil, errors.Wrapf(err, "failed to unmarshal legacy configuration")
	}

	upgradedCfg, err := upgradeLegacyConfig(legacyCfg, factory)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to upgrade config")
	}

	// indicates that this is the default config
	if upgradedCfg == nil {
		return nil, nil
	}

	outputBytes, err := yaml.Marshal(*upgradedCfg)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to marshal configuration as YAML")
	}
	return outputBytes, nil
}

func upgradeLegacyConfig(legacyCfg Config, factory formatplugin.Factory) (*v0.Config, error) {
	defaultCfg := Config{
		Formatters: map[string]FormatterConfig{
			"gofmt": {
				Args: []string{
					"-s",
				},
			},
		},
	}
	if reflect.DeepEqual(legacyCfg.Formatters, defaultCfg.Formatters) && legacyCfg.Exclude.Empty() {
		// special case: this was the default configuration that shipped with gödel. If this is all that existed, no
		// need to upgrade (default behavior of upgraded plugins/assets match this configuration).
		return nil, nil
	}

	if reflect.DeepEqual(legacyCfg.Formatters, defaultCfg.Formatters) {
		// special case: formatter configuration matches default, but exclude configuration does not. Upgrade just the
		// exclude configuration.
		return &v0.Config{
			Exclude: legacyCfg.Exclude,
		}, nil
	}

	// configuration does not match default: delegate to asset upgraders
	upgradedCfg := v0.Config{}

	var sortedKeys []string
	for k := range legacyCfg.Formatters {
		sortedKeys = append(sortedKeys, k)
	}
	sort.Strings(sortedKeys)

	if len(sortedKeys) > 0 {
		upgradedCfg.Formatters = make(map[string]v0.FormatterConfig)
	}

	for _, k := range sortedKeys {
		upgrader, err := factory.ConfigUpgrader(k)
		if err != nil {
			return nil, err
		}

		assetCfgBytes, err := yaml.Marshal(AssetConfig{
			Legacy: true,
			Args:   legacyCfg.Formatters[k].Args,
		})
		if err != nil {
			return nil, errors.Wrapf(err, "failed to marshal legacy asset configuration for formatter %q as YAML", k)
		}

		upgradedBytes, err := upgrader.UpgradeConfig(assetCfgBytes)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to upgrade configuration for formatter %s", k)
		}

		var yamlRep yaml.MapSlice
		if err := yaml.Unmarshal(upgradedBytes, &yamlRep); err != nil {
			return nil, errors.Wrapf(err, "failed to unmarshal YAML for upgraded configuration for formatter %q", k)
		}

		upgradedCfg.Formatters[k] = v0.FormatterConfig{
			Config: yamlRep,
		}
	}
	return &upgradedCfg, nil
}
