package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"strings"
	"sync"
	"unicode"
)

type Languages map[string]map[string]string

func LoadLanguages(fsys fs.FS, dir string) Languages {
	out := Languages{}
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		panic("languages: " + err.Error())
	}
	for _, e := range entries {
		lang, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok {
			continue
		}
		raw, err := fs.ReadFile(fsys, path.Join(dir, e.Name()))
		if err != nil {
			panic("languages: " + err.Error())
		}
		dict := map[string]string{}
		if err := json.Unmarshal(raw, &dict); err != nil {
			panic("languages: " + e.Name() + ": " + err.Error())
		}
		out[lang] = dict
	}
	return out
}

func (a Application) CheckLanguages() error {
	if len(a.Languages) > 32 {
		return fmt.Errorf("application translations exceed 32 languages")
	}
	entries := 0
	for lang, words := range a.Languages {
		if lang == "" || len(lang) > 35 || strings.ContainsAny(lang, "/\\") || strings.ContainsFunc(lang, unicode.IsSpace) {
			return fmt.Errorf("application translations need a language tag")
		}
		entries += len(words)
		for source, translation := range words {
			if strings.TrimSpace(source) == "" || strings.TrimSpace(translation) == "" || len(source) > 4096 || len(translation) > 4096 {
				return fmt.Errorf("application translations need nonempty texts of at most 4096 bytes")
			}
		}
	}
	if entries > 10000 {
		return fmt.Errorf("application translations exceed 10000 entries")
	}
	return nil
}

type Resources struct {
	mu        sync.Mutex
	closing   bool
	cleanups  []resourceCleanup
	closeGate chan struct{}
}

type resourceCleanup struct {
	name string
	undo func(context.Context) error
}

func (s *Resources) Track(name string, undo func(context.Context) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing || undo == nil {
		return fmt.Errorf("cannot register resource %s on an inactive scope", name)
	}
	s.cleanups = append(s.cleanups, resourceCleanup{name, undo})
	return nil
}

func (s *Resources) Close(ctx context.Context) error {
	s.mu.Lock()
	s.closing = true
	if s.closeGate == nil {
		s.closeGate = make(chan struct{}, 1)
	}
	gate := s.closeGate
	s.mu.Unlock()
	select {
	case gate <- struct{}{}:
		defer func() { <-gate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		s.mu.Lock()
		if len(s.cleanups) == 0 {
			s.mu.Unlock()
			return nil
		}
		last := s.cleanups[len(s.cleanups)-1]
		s.mu.Unlock()
		if err := last.undo(ctx); err != nil {
			return errors.Join(fmt.Errorf("resource %s did not close", last.name), err)
		}
		s.mu.Lock()
		s.cleanups = s.cleanups[:len(s.cleanups)-1]
		s.mu.Unlock()
	}
}

func CallRead(app App, c Caller, name string, inputs ...json.RawMessage) (any, *kernel.Error) {
	if app == nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	parameterized, hasParameterized := app.(ParameterizedReader)
	_, hasPlain := app.(Reader)
	if !hasParameterized && !hasPlain {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	if fields, declared := app.Manifest().ReadInputs[name]; declared {
		if reader, ok := parameterized, hasParameterized; ok {
			raw := json.RawMessage(`{}`)
			if len(inputs) > 0 && len(inputs[0]) > 0 {
				raw = inputs[0]
			}
			var values map[string]any
			if json.Unmarshal(raw, &values) != nil || values == nil {
				return nil, Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Read inputs must be a JSON object")
			}
			if issues := InputIssues(fields, values); len(issues) > 0 {
				return nil, c.RefuseFields(issues)
			}
			return reader.ReadInput(c, name, raw)
		}
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	if reader, ok := app.(Reader); ok {
		return reader.Read(c, name)
	}
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
}
