package values

import (
	"regexp"
	"sync"
)

var patternPrograms sync.Map

func PatternProgram(pattern string) (*regexp.Regexp, error) {
	if cached, ok := patternPrograms.Load(pattern); ok {
		return cached.(*regexp.Regexp), nil
	}
	program, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	cached, _ := patternPrograms.LoadOrStore(pattern, program)
	return cached.(*regexp.Regexp), nil
}
