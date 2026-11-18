package model

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
)

// ParseQueue strictly decodes {"tasks":[...]} and validates the result. An
// unknown field, including "verify", is rejected (V1).
func ParseQueue(raw []byte) ([]TaskDef, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var q struct {
		Tasks []TaskDef `json:"tasks"`
	}
	if err := dec.Decode(&q); err != nil {
		return nil, fmt.Errorf("queue: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("queue: trailing data after JSON object")
	}
	if err := ValidateTasks(q.Tasks); err != nil {
		return nil, err
	}
	return q.Tasks, nil
}

// ValidateTasks checks required fields, unique keys, dangling dependencies and
// dependency cycles.
func ValidateTasks(tasks []TaskDef) error {
	if len(tasks) == 0 {
		return errors.New("queue: at least one task is required")
	}
	keys := map[string]bool{}
	for i, t := range tasks {
		switch {
		case t.Key == "":
			return fmt.Errorf("queue: tasks[%d].key is required", i)
		case t.Title == "":
			return fmt.Errorf("queue: task %s: title is required", t.Key)
		case t.Prompt == "":
			return fmt.Errorf("queue: task %s: prompt is required", t.Key)
		case len(t.Acceptance) == 0:
			return fmt.Errorf("queue: task %s: acceptance is required", t.Key)
		case keys[t.Key]:
			return fmt.Errorf("queue: duplicate task key %q", t.Key)
		}
		keys[t.Key] = true
	}
	deps := map[string][]string{}
	for _, t := range tasks {
		seen := map[string]bool{}
		for _, d := range t.DependsOn {
			if seen[d] {
				return fmt.Errorf("queue: task %s lists dependency %q twice", t.Key, d)
			}
			seen[d] = true
			if !keys[d] {
				return fmt.Errorf("queue: task %s depends on unknown task %q", t.Key, d)
			}
		}
		deps[t.Key] = t.DependsOn
	}
	state := map[string]int{} // 1 visiting, 2 done
	var visit func(k string) error
	visit = func(k string) error {
		switch state[k] {
		case 1:
			return fmt.Errorf("queue: dependency cycle through task %q", k)
		case 2:
			return nil
		}
		state[k] = 1
		for _, d := range deps[k] {
			if err := visit(d); err != nil {
				return err
			}
		}
		state[k] = 2
		return nil
	}
	for _, t := range tasks {
		if err := visit(t.Key); err != nil {
			return err
		}
	}
	return nil
}

// QueueHash is the sha256 over the canonical JSON of the task definitions,
// sorted by key with sorted dependency lists, so it is stable under reordering
// and changes on any field edit (QA2).
func QueueHash(tasks []TaskDef) string {
	cp := make([]TaskDef, len(tasks))
	for i, t := range tasks {
		t.Acceptance = nonNil(t.Acceptance)
		t.FilesTouched = nonNil(t.FilesTouched)
		t.DependsOn = append(nonNil(nil), t.DependsOn...)
		sort.Strings(t.DependsOn)
		cp[i] = t
	}
	sort.Slice(cp, func(i, j int) bool { return cp[i].Key < cp[j].Key })
	b, _ := json.Marshal(cp) // struct field order is fixed; no maps
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
