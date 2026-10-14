package schema

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/model"
)

// FRDDraft is the drafter's reply for the draft and polish purposes: an FRD
// of which the model must supply only the objective, the requirements
// (description, optional priority) and the criteria text. Ids, links, status
// and dates are filled by code (model.FillFRD), and an empty title falls back
// to the effort's.
type FRDDraft struct {
	model.FRD
}

// Validate checks the parts the model owns and, through model.ParseFRD, that
// the links it chose point at real requirements.
func (d *FRDDraft) Validate() error {
	return d.validate("")
}

func (d *FRDDraft) validate(prefix string) error {
	if err := nonEmpty(prefix+"objective", strings.TrimSpace(d.Objective)); err != nil {
		return err
	}
	if len(d.Requirements) == 0 {
		return bad(prefix+"requirements", "needs at least one entry")
	}
	for i, r := range d.Requirements {
		f := fmt.Sprintf("%srequirements[%d]", prefix, i)
		if err := nonEmpty(f+".description", strings.TrimSpace(r.Description)); err != nil {
			return err
		}
		if r.Priority != "" {
			if err := oneOf(f+".priority", r.Priority, "must", "should", "could", "wont"); err != nil {
				return err
			}
		}
	}
	for i, ac := range d.AcceptanceCriteria {
		if err := nonEmpty(fmt.Sprintf("%sacceptance_criteria[%d].criterion", prefix, i), strings.TrimSpace(ac.Criterion)); err != nil {
			return err
		}
	}
	probe := d.FRD
	probe.Requirements = append([]model.FRDRequirement{}, d.Requirements...)
	probe.AcceptanceCriteria = append([]model.FRDAC{}, d.AcceptanceCriteria...)
	if probe.Title == "" {
		probe.Title = "draft"
	}
	model.FillFRD(&probe, 0, time.Time{})
	raw, err := json.Marshal(probe)
	if err == nil {
		_, err = model.ParseFRD(raw)
	}
	if err != nil {
		return bad(prefix+"acceptance_criteria", "%s", strings.TrimPrefix(err.Error(), "frd: "))
	}
	return nil
}

// FRDSplit is the drafter's reply for the split purpose: the FRD cut into
// ordered, self-contained parts, each a full FRD with a title.
type FRDSplit struct {
	Parts []FRDDraft `json:"parts"`
}

// Validate requires at least one part and every part to be a valid draft with
// its own title.
func (s *FRDSplit) Validate() error {
	if len(s.Parts) == 0 {
		return bad("parts", "needs at least one entry")
	}
	for i := range s.Parts {
		p := fmt.Sprintf("parts[%d].", i)
		if err := nonEmpty(p+"title", strings.TrimSpace(s.Parts[i].Title)); err != nil {
			return err
		}
		if err := s.Parts[i].validate(p); err != nil {
			return err
		}
	}
	return nil
}
