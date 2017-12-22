package main

import (
	"os"
	"testing"

	"github.com/prometheus/prometheus/pkg/rulefmt"
	. "github.com/smartystreets/goconvey/convey"
	"k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var c Controller

func TestMain(m *testing.M) {
	c = Controller{}
	os.Exit(m.Run())
}

func TestExtractValues(t *testing.T) {
	yaml := `- record: job:http_inprogress_requests:sum
  expr: sum(http_inprogress_requests) by (job)
- alert: HighErrorRate
  expr: job:request_latency_seconds:mean5m{job="myjob"} > 0.5
  for: 10m
  labels:
    severity: page
  annotations:
    summary: High request latency`
	groups := make([]rulefmt.RuleGroup, 0)

	Convey("Extract Recording and Alert rules", t, func() {
		name := "test"
		err := c.extractValues(name, yaml, &groups)
		if err != nil {
			t.Errorf("Error: %s", err)
		}
		t.Logf("test strucl: %v\n", groups)
		t.Logf("%s\n", groups[0].Rules[0].Record)
		So(len(groups), ShouldEqual, 1)
		So(groups[0].Name, ShouldEqual, name)
		So(len(groups[0].Rules), ShouldEqual, 2)
		So(groups[0].Rules[0].Record, ShouldContainSubstring, "job:http")
		So(groups[0].Rules[1].Alert, ShouldEqual, "HighErrorRate")
	})
}

func TestBadRule(t *testing.T) {
	cm1d := map[string]string{
		"test": `- record: job:http_inprogress_requests:sum
  expr: sum(http_inprogress_requests) by (job)`,
		"test2": `garbage`,
		"test3": `- record: fail`,
	}
	cm1m := meta_v1.ObjectMeta{
		Name:        "configmap_one",
		Namespace:   "one",
		Annotations: map[string]string{"nordstrom.net/prometheus2Alerts": "true"},
	}
	cm1 := v1.ConfigMap{
		Data:       cm1d,
		ObjectMeta: cm1m,
	}

	cmList := v1.ConfigMapList{Items: []v1.ConfigMap{cm1}}

	Convey("Processing list of configmaps", t, func() {
		groups := []rulefmt.RuleGroup{}
		c.processConfigMaps(&cmList, &groups)

		So(len(groups), ShouldEqual, 1)
		So(groups[0].Name, ShouldEqual, "test")
	})

}

func TestCreateFile(t *testing.T) {
	cm1d := map[string]string{"test": `- record: job:http_inprogress_requests:sum
  expr: sum(http_inprogress_requests) by (job)`}
	cm1m := meta_v1.ObjectMeta{
		Name:        "configmap_one",
		Namespace:   "one",
		Annotations: map[string]string{"nordstrom.net/prometheus2Alerts": "true"},
	}
	cm1 := v1.ConfigMap{
		Data:       cm1d,
		ObjectMeta: cm1m,
	}

	cm2d := map[string]string{"test2": `- alert: HighErrorRate
  expr: job:request_latency_seconds:mean5m{job="myjob"} > 0.5
  for: 10m
  labels:
    severity: page
  annotations:
    summary: High request latency`}
	cm2m := meta_v1.ObjectMeta{
		Name:        "configmap_two",
		Namespace:   "two",
		Annotations: map[string]string{"nordstrom.net/prometheus2Alerts": "true"},
	}
	cm2 := v1.ConfigMap{
		Data:       cm2d,
		ObjectMeta: cm2m,
	}

	cm3d := map[string]string{"test3": `Just a normal configmap`}
	cm3m := meta_v1.ObjectMeta{
		Name:        "configmap_three",
		Namespace:   "three",
		Annotations: map[string]string{"badannotation": "true"},
	}
	cm3 := v1.ConfigMap{
		Data:       cm3d,
		ObjectMeta: cm3m,
	}

	cmList := v1.ConfigMapList{Items: []v1.ConfigMap{cm1, cm2, cm3}}

	Convey("Processing list of configmaps", t, func() {
		groups := []rulefmt.RuleGroup{}
		c.processConfigMaps(&cmList, &groups)

		So(len(groups), ShouldEqual, 2)
		So(groups[0].Name, ShouldEqual, "test")
		So(groups[1].Name, ShouldEqual, "test2")
	})

}
