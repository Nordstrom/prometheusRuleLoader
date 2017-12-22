package main

import (
	"github.com/prometheus/prometheus/pkg/rulefmt"
)

func checkRules(groupPtr *rulefmt.RuleGroup) []error {
	rgs := rulefmt.RuleGroups{}
	rgs.Groups = append(rgs.Groups, *groupPtr)
	errs := rgs.Validate()
	return errs
}
