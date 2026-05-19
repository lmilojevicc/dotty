package dotty

import (
	"fmt"
	"strings"
)

type Selector struct {
	Package string
	Source  string
}

type ResolveOptions struct {
	Selectors   []Selector
	Collections []string
	Targets     []string
	All         bool
}

func ParseSelector(arg string) (Selector, error) {
	if arg == "" {
		return Selector{}, fmt.Errorf("empty selector")
	}

	packageName, source, hasSource := strings.Cut(arg, "/")
	if err := validateName("package", packageName); err != nil {
		return Selector{}, err
	}
	if !hasSource {
		return Selector{Package: packageName}, nil
	}
	if source == "" {
		return Selector{}, fmt.Errorf("empty source selector")
	}
	if err := validateSourcePath(source); err != nil {
		return Selector{}, err
	}
	return Selector{Package: packageName, Source: source}, nil
}

func (s Selector) IsPackage() bool {
	return s.Source == ""
}

func (s Selector) IsPackageSource() bool {
	return s.Source != ""
}
