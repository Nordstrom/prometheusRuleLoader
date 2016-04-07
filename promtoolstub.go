package main

import (
	//	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/promql"
	//	"github.com/prometheus/prometheus/util/cli"
	//	"github.com/prometheus/prometheus/version"
)

func CheckRules(rules string) error {
	_, err := promql.ParseStmts(rules)
	if err != nil {
		return err
	}
	return nil
}
