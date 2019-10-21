package main

import (
	"testing"
	"gopkg.in/yaml.v2"

	"k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"github.com/prometheus/prometheus/pkg/rulefmt"

	. "github.com/smartystreets/goconvey/convey"
)

var c Controller

var testRules []byte
var testRuleGroup []byte
var testRuleGroups []byte

var configmapDataBlockRules v1.ConfigMap
var configmapDataBlockRulesGroup v1.ConfigMap
var configmapDataBlockRulesGroups v1.ConfigMap
var configmapDataBlockAllThree v1.ConfigMap

func TestMain(m *testing.M) {

	testRulesObj := []rulefmt.Rule {
		rulefmt.Rule{
			Record: "job:http_inprogress_requests:sum",
			Expr: "sum(http_inprogress_requests) by (job)",
		},
		rulefmt.Rule{
			Alert: "HighErrorRate",
			Expr: "job:request_latency_seconds:mean5m{job=\"myjob\"} > 0.5",
			Labels: map[string]string {
				"severity": "Page",
			},
			Annotations: map[string]string {
				"summary": "High request latency",
			},
		},
	}

	testRuleGroupObj := rulefmt.RuleGroup{
		Name: "TestGroup",
		Rules: testRulesObj,
	}

	testRuleGroupsObj := rulefmt.RuleGroups{
		Groups: []rulefmt.RuleGroup{
			testRuleGroupObj,
		},
	}

	var err error
	testRules, err = yaml.Marshal(testRulesObj)
	if err != nil {
		fmt.Println(err)
	}

	testRuleGroup, err = yaml.Marshal(testRuleGroupObj)
	if err != nil {
		fmt.Println(err)
	}

	testRuleGroups, err = yaml.Marshal(testRuleGroupsObj)
	if err != nil {
		fmt.Println(err)
	}

	configmapDataBlockRules.Data = map[string]string{
		"rules": string(testRules),
	}
	configmapDataBlockRulesGroup.Data = map[string]string{
		"rules": string(testRuleGroup),
	}
	configmapDataBlockRulesGroup.Data = map[string]string{
		"rules": string(testRuleGroup),
	}
	configmapDataBlockAllThree.Data = map[string]string{
		"aaa": string(testRules),
		"bbb": string(testRuleGroup),
		"ccc": string(testRuleGroups),
	}

	c = Controller{}
	os.Exit(m.Run())
}

func TestExtractRuleGroups(t *testing.T) {
	Convey("Test extractRuleGroups, a properly formatted rule groups", t, func() {

		Convey("Positive test case", func() {
			err, rulegroups := c.extractRuleGroups(string(testRuleGroups))

			So(err, ShouldBeNil)
			So(rulegroups.Groups[0].Name, ShouldEqual, "TestGroup")
			So(rulegroups.Groups[0].Rules[0].Record, ShouldEqual, "job:http_inprogress_requests:sum")

		})

		Convey( "Negative test case", func() {
			err, rulegroups := c.extractRuleGroups(string(testRuleGroup))

			So(err, ShouldNotBeNil)
			So(len(rulegroups.Groups), ShouldEqual, 0)

		})
	})
}

func TestExtractRuleGroupAsRuleGroups(t *testing.T) {
	Convey("Test extractRuleGroupAsRuleGroups, a properly formatted rule group", t, func() {

		Convey("Positive test case", func() {
			err, rulegroups := c.extractRuleGroupAsRuleGroups(string(testRuleGroup))

			So(err, ShouldBeNil)
			So(rulegroups.Groups[0].Name, ShouldEqual, "TestGroup")
			So(rulegroups.Groups[0].Rules[0].Record, ShouldEqual, "job:http_inprogress_requests:sum")
		})
		Convey("Negative test case", func() {
			err, rulegroups := c.extractRuleGroupAsRuleGroups(string(testRuleGroups))

			So(err, ShouldNotBeNil)
			So(len(rulegroups.Groups), ShouldEqual, 0)
		})

	})
}

func TestExtractRulesAsRuleGroups(t *testing.T) {
	Convey("Test extractRuleGroupAsRuleGroups, a properly formatted rule group", t, func() {

		Convey("Positive test case", func() {
			err, rulegroups := c.extractRulesAsRuleGroups("aaaa-bbbb", "cccc", string(testRules))

			So(err, ShouldBeNil)
			So(rulegroups.Groups[0].Name, ShouldEqual, "aaaa-bbbb-cccc")
			So(rulegroups.Groups[0].Rules[0].Record, ShouldEqual, "job:http_inprogress_requests:sum")
		})

		Convey("Negative test case", func() {
			err, rulegroups := c.extractRulesAsRuleGroups("aaaa-bbbb", "cccc", string(testRuleGroup))

			So(err, ShouldNotBeNil)
			So(len(rulegroups.Groups), ShouldEqual, 0)
		})


	})
}

func TestExtractValues(t *testing.T) {
	Convey("Test extractValues no matter what of the possible rules formats it should return a good rulesgroups", t, func() {
		Convey("If we pass it just rules it should return a good rulesgroups", func() {
			mrg, err := c.extractValues("aaaa-bbbb", configmapDataBlockRules.Data )

			So(err, ShouldBeNil)
			So(len(mrg.Values), ShouldEqual, 1)
			So(mrg.Values[0].Groups[0].Name, ShouldEqual, "aaaa-bbbb-rules")
			So(mrg.Values[0].Groups[0].Rules[0].Record, ShouldEqual, "job:http_inprogress_requests:sum")
		})

		Convey("If we pass it a rulegroup it should return a good rulesgroups", func() {
			mrg, err := c.extractValues("aaaa-bbbb", configmapDataBlockRulesGroup.Data )

			So(err, ShouldBeNil)
			So(len(mrg.Values), ShouldEqual, 1)
			So(mrg.Values[0].Groups[0].Name, ShouldEqual, "TestGroup")
			So(mrg.Values[0].Groups[0].Rules[0].Record, ShouldEqual, "job:http_inprogress_requests:sum")
		})

		Convey("If we pass it a rulegroups it should return a good rulesgroups", func() {
			mrg, err := c.extractValues("aaaa-bbbb", configmapDataBlockRulesGroup.Data )

			So(err, ShouldBeNil)
			So(len(mrg.Values), ShouldEqual, 1)
			So(mrg.Values[0].Groups[0].Name, ShouldEqual, "TestGroup")
			So(mrg.Values[0].Groups[0].Rules[0].Record, ShouldEqual, "job:http_inprogress_requests:sum")
		})

		Convey("If we pass it all three we should return three rulesgroups", func() {
			mrg, err := c.extractValues("aaaa-bbbb", configmapDataBlockAllThree.Data )

			So(err, ShouldBeNil)
			So(len(mrg.Values), ShouldEqual, 3)
			So(mrg.Values[0].Groups[0].Name, ShouldEqual, "aaaa-bbbb-aaa")
			So(mrg.Values[0].Groups[0].Rules[0].Record, ShouldEqual, "job:http_inprogress_requests:sum")
			So(mrg.Values[1].Groups[0].Name, ShouldEqual, "TestGroup")
			So(mrg.Values[1].Groups[0].Rules[0].Record, ShouldEqual, "job:http_inprogress_requests:sum")
			So(mrg.Values[2].Groups[0].Name, ShouldEqual, "TestGroup")
			So(mrg.Values[2].Groups[0].Rules[0].Record, ShouldEqual, "job:http_inprogress_requests:sum")

		})

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
		Name:        "myconfigmapone",
		Namespace:   "mynamespace",
		Annotations: map[string]string{"nordstrom.net/prometheus2Alerts": "true"},
	}
	cm1 := v1.ConfigMap{
		Data:       cm1d,
		ObjectMeta: cm1m,
	}

	cmList := v1.ConfigMapList{Items: []v1.ConfigMap{cm1}}

	Convey("Processing list of configmaps", t, func() {
		mrg, err := c.processConfigMaps(&cmList)

		So(err, ShouldNotBeNil)
		So(len(mrg.Values), ShouldEqual, 2)
		So(mrg.Values[0].Groups[0].Name, ShouldStartWith, "mynamespace-myconfigmapone-test")
	})

}

func TestCreateFile(t *testing.T) {
	cm1d := map[string]string{"test": `- record: job:http_inprogress_requests:sum
  expr: sum(http_inprogress_requests) by (job)`}
	cm1m := meta_v1.ObjectMeta{
		Name:        "myconfigmapone",
		Namespace:   "mynamespace",
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
		Name:        "myconfigmaptwo",
		Namespace:   "myothernamespace",
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
		mrg, err := c.processConfigMaps(&cmList)

		So(err, ShouldBeNil)
		So(len(mrg.Values), ShouldEqual, 2)
		So(mrg.Values[0].Groups[0].Name, ShouldEqual, "mynamespace-myconfigmapone-test")
		So(mrg.Values[1].Groups[0].Name, ShouldEqual, "myothernamespace-myconfigmaptwo-test2")
	})

}