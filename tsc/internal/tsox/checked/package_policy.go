package checked

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/microsoft/typescript-go/internal/core"
	"github.com/microsoft/typescript-go/internal/tsoptions"
	"github.com/microsoft/typescript-go/internal/tspath"
)

type DependencyTypes string

const (
	DependencyTypesConfig  DependencyTypes = "tsconfig"
	DependencyTypesInferJS DependencyTypes = "infer-js"
)

type ProjectOptions struct{ DependencyTypes DependencyTypes }
type dependencyPolicyRecord struct {
	ConfiguredOptions, EffectiveOptions  string
	Version                              int
	Policy                               DependencyTypes
	ConfiguredRoots, ImplementationRoots []string
	ConfiguredAllowJS, EffectiveAllowJS  core.Tristate
	ConfiguredDepth, EffectiveDepth      int
}

func dependencyConfig(config *tsoptions.ParsedCommandLine, policy ProjectOptions, depth int, implementations []string) (*tsoptions.ParsedCommandLine, dependencyPolicyRecord, error) {
	kind := policy.DependencyTypes
	if kind == "" {
		kind = DependencyTypesConfig
	}
	if kind != DependencyTypesConfig && kind != DependencyTypesInferJS {
		return nil, dependencyPolicyRecord{}, fmt.Errorf("DependencyTypingPolicy: unknown policy %q", kind)
	}
	original := config.CompilerOptions()
	effective := original.Clone()
	configuredDepth := 0
	if original.MaxNodeModuleJsDepth != nil {
		configuredDepth = *original.MaxNodeModuleJsDepth
	}
	if kind == DependencyTypesInferJS {
		effective.AllowJs = core.TSTrue
		depth = max(configuredDepth, depth)
		effective.MaxNodeModuleJsDepth = &depth
	} else {
		if len(implementations) != 0 {
			return nil, dependencyPolicyRecord{}, fmt.Errorf("DependencyTypingPolicy: implementation inference requires infer-js")
		}
		depth = configuredDepth
	}
	roots := slices.Clone(config.FileNames())
	for _, p := range implementations {
		if !slices.Contains(roots, p) {
			roots = append(roots, p)
		}
	}
	next := tsoptions.NewParsedCommandLine(effective, roots, tspath.ComparePathsOptions{UseCaseSensitiveFileNames: config.UseCaseSensitiveFileNames(), CurrentDirectory: config.GetCurrentDirectory()})
	parsed := *config.ParsedConfig
	parsed.CompilerOptions = effective
	parsed.FileNames = roots
	next.ParsedConfig = &parsed
	next.ConfigFile = config.ConfigFile
	next.Errors = slices.Clone(config.Errors)
	next.Raw = config.Raw
	next.CompileOnSave = config.CompileOnSave
	configuredBytes, err := json.Marshal(original)
	if err != nil {
		return nil, dependencyPolicyRecord{}, err
	}
	effectiveBytes, err := json.Marshal(effective)
	if err != nil {
		return nil, dependencyPolicyRecord{}, err
	}
	record := dependencyPolicyRecord{string(configuredBytes), string(effectiveBytes), 1, kind, slices.Clone(config.FileNames()), slices.Clone(implementations), original.AllowJs, effective.AllowJs, configuredDepth, depth}
	return next, record, nil
}
